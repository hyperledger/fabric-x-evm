/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/gateway/metrics"
)

var loggerV2 = flogging.MustGetLogger("gateway.core.txqueue_v2")

// txEntry represents a transaction in the queue with its dependency information.
// Each entry maintains two lists of pointers to other transactions:
// - Blocks: transactions that are waiting for this one to complete
// - IsBlockedBy: transactions that must complete before this one can start
//
// Invariants:
// - Entries in the Ready list have empty IsBlockedBy (no dependencies)
// - Entries in the Waiting list have non-empty IsBlockedBy (have dependencies)
type txEntry struct {
	tx           *types.Transaction
	participants []common.Address // Cached participants (sender/recipient)
	enqueuedAt   time.Time        // wall time the tx was Enqueued (for queue-wait metric)

	// Linked list element in either readyList or waitingList
	listElement *list.Element

	// Dependency tracking
	blocks      []*txEntry // Transactions blocked by this one
	isBlockedBy []*txEntry // Transactions blocking this one
}

// TxQueueV2 is a dependency-aware transaction queue that ensures transactions
// touching the same participants (sender/recipient addresses) are processed sequentially.
//
// Architecture:
// - Ready list: Transactions with no dependencies, ready to be dequeued
// - Waiting list: Transactions with dependencies, waiting for blockers to complete
// - Participant map: Fast lookup of transactions by participant address
// - Hash map: Fast lookup of transaction entries by hash
// - Pending map: Tracks transactions currently being processed by workers
//
// Thread-safety: All operations are protected by a single RWMutex.
type TxQueueV2 struct {
	mu   sync.RWMutex
	cond *sync.Cond

	// Transaction lists
	readyList   *list.List // Transactions ready to be dequeued
	waitingList *list.List // Transactions waiting for dependencies

	// Fast lookup maps
	// participantMap holds the SINGLE most-recent in-flight tx per participant address.
	// A new tx touching the same participant only blocks on this one; transitive
	// dependency chains ensure MVCC ordering with prior in-flight txs is preserved.
	// This shrinks the dependency graph from O(N²) edges (every new tx → every prior
	// in-flight tx touching the same participant) to O(N) edges.
	participantMap map[common.Address]*txEntry
	hashMap        map[common.Hash]*txEntry           // Tx hash -> transaction entry
	pendingMap     map[common.Hash]*types.Transaction // Tx hash -> in-progress transactions

	// Shutdown flag
	done bool

	// Statistics
	total   int
	invalid int
}

// NewTxQueueV2 creates a new dependency-aware transaction queue.
func NewTxQueueV2() *TxQueueV2 {
	q := &TxQueueV2{
		readyList:      list.New(),
		waitingList:    list.New(),
		participantMap: make(map[common.Address]*txEntry),
		hashMap:        make(map[common.Hash]*txEntry),
		pendingMap:     make(map[common.Hash]*types.Transaction),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds a transaction to the queue.
// The transaction is placed in the Ready list if it has no conflicts with
// in-progress or waiting transactions, otherwise it's placed in the Waiting list
// with appropriate dependency links.
//
// Lock-hold optimisation: tx.Hash() and participantsForTx() (which does an ECDSA
// signature recovery, ~50-200µs) are both computed BEFORE acquiring the lock.
// A fast read-locked dedup check short-circuits when the tx is already tracked,
// so duplicate-heavy replay workloads pay only an RLock + map lookup, not the
// crypto cost. Insertion takes the write lock and double-checks the dedup map
// to handle the race between RUnlock and Lock.
func (q *TxQueueV2) Enqueue(tx *types.Transaction) {
	start := time.Now()
	defer func() { metrics.GatewayTxQueueEnqueueDuration.Observe(time.Since(start).Seconds()) }()

	// Compute tx hash outside any lock (~1µs, cached on first call).
	txHash := tx.Hash()

	// Fast read-locked dedup check. Replay workloads short-circuit here.
	q.mu.RLock()
	_, exists := q.hashMap[txHash]
	q.mu.RUnlock()
	if exists {
		return
	}

	// Compute participants outside the write lock (~100µs ECDSA recovery).
	participants := participantsForTx(tx)

	q.mu.Lock()
	defer q.mu.Unlock()

	// Double-check: another goroutine may have inserted the same tx
	// between our RUnlock and Lock.
	if _, exists := q.hashMap[txHash]; exists {
		return
	}

	// Create new entry (participants already computed above)
	entry := &txEntry{
		tx:           tx,
		participants: participants,
		enqueuedAt:   time.Now(),
		blocks:       make([]*txEntry, 0),
		isBlockedBy:  make([]*txEntry, 0),
	}

	// Find conflicting in-flight txs: only the most-recent prior tx per participant.
	// Transitive ordering via existing dependency edges still serializes against
	// older in-flight txs on the same participant.
	conflictingTxs := make(map[*txEntry]bool)
	for _, participant := range entry.participants {
		if mostRecent, exists := q.participantMap[participant]; exists {
			conflictingTxs[mostRecent] = true
		}
	}

	// If there are conflicts, add dependency links and place in waiting list
	if len(conflictingTxs) > 0 {
		for conflictTx := range conflictingTxs {
			// Add bidirectional dependency links
			entry.isBlockedBy = append(entry.isBlockedBy, conflictTx)
			conflictTx.blocks = append(conflictTx.blocks, entry)
		}

		// Add to waiting list
		entry.listElement = q.waitingList.PushBack(entry)
		loggerV2.Debugf("[QUEUE] Enqueue: tx %s WAITING (blocked by %d), ready=%d waiting=%d pending=%d",
			txHash.Hex()[:10], len(conflictingTxs), q.readyList.Len(), q.waitingList.Len(), len(q.pendingMap))
	} else {
		// No conflicts - add to ready list
		entry.listElement = q.readyList.PushBack(entry)
		q.cond.Signal() // Wake up one waiting worker
		loggerV2.Debugf("[QUEUE] Enqueue: tx %s READY, ready=%d waiting=%d pending=%d",
			txHash.Hex()[:10], q.readyList.Len(), q.waitingList.Len(), len(q.pendingMap))
	}

	// Update lookup maps. Overwriting participantMap[p] makes this tx the new
	// most-recent in-flight tx for that participant; the previous most-recent is
	// already recorded as a dependency in our isBlockedBy list (and a transitive
	// chain back to older in-flight txs sharing p).
	q.hashMap[txHash] = entry
	for _, participant := range entry.participants {
		q.participantMap[participant] = entry
	}

	metrics.GatewayTxQueueSize.Set(float64(q.readyList.Len() + q.waitingList.Len()))
}

// Dequeue removes a transaction from the ready list and moves it to the pending map.
// Blocks if the ready list is empty until a transaction becomes available or the queue is closed.
// Returns (transaction, true) if successful, or (nil, false) if queue is closed and empty.
func (q *TxQueueV2) Dequeue() (*types.Transaction, bool) {
	start := time.Now()
	defer func() { metrics.GatewayTxQueueDequeueDuration.Observe(time.Since(start).Seconds()) }()
	lockStart := time.Now()
	q.mu.Lock()
	metrics.GatewayTxQueueDequeueLockWait.Observe(time.Since(lockStart).Seconds())
	defer q.mu.Unlock()

	for {
		// Wait while ready list is empty and queue is not closed
		for q.readyList.Len() == 0 && !q.done {
			waitStart := time.Now()
			q.cond.Wait()
			metrics.GatewayTxQueueDequeueCondWait.Observe(time.Since(waitStart).Seconds())
		}

		// Check if queue is closed and empty
		if q.done && q.readyList.Len() == 0 {
			return nil, false
		}

		// Get transaction from front of ready list
		if elem := q.readyList.Front(); elem != nil {
			entry := elem.Value.(*txEntry)
			tx := entry.tx
			txHash := tx.Hash()

			// Remove from ready list
			q.readyList.Remove(elem)
			entry.listElement = nil

			// Keep in participant map so new transactions can find and block on it
			// Will be removed when Complete is called

			// Move to pending map
			q.pendingMap[txHash] = tx

			if !entry.enqueuedAt.IsZero() {
				metrics.GatewayTxQueueWait.Observe(time.Since(entry.enqueuedAt).Seconds())
			}
			metrics.GatewayTxQueueSize.Set(float64(q.readyList.Len() + q.waitingList.Len()))

			loggerV2.Debugf("Dequeue: tx %s, waiting=%d ready=%d pending=%d",
				txHash.Hex()[:10], q.waitingList.Len(), q.readyList.Len(), len(q.pendingMap))

			return tx, true
		}

		// Ready list became empty while we were waiting, loop again
		if q.done {
			return nil, false
		}
	}
}

// completeUnlocked is the internal implementation of Complete that assumes the lock is held.
// Returns the number of transactions promoted to the ready list.
func (q *TxQueueV2) completeUnlocked(hash common.Hash) int {
	// Remove from pending map
	delete(q.pendingMap, hash)

	// Find the transaction entry
	entry, exists := q.hashMap[hash]
	if !exists {
		return 0
	}

	numPromoted := 0
	numBlocked := len(entry.blocks)

	// Process all transactions blocked by this one
	for _, blockedTx := range entry.blocks {
		// Remove this entry from the blocked transaction's isBlockedBy list
		blockedTx.isBlockedBy = removeFromSlice(blockedTx.isBlockedBy, entry)

		// If the blocked transaction has no more dependencies, promote it to ready list
		if len(blockedTx.isBlockedBy) == 0 {
			// Remove from waiting list
			if blockedTx.listElement != nil {
				q.waitingList.Remove(blockedTx.listElement)
			}

			// Add to ready list
			blockedTx.listElement = q.readyList.PushBack(blockedTx)
			numPromoted++
		}
	}

	if numBlocked > 0 {
		loggerV2.Debugf("Complete: tx %s unblocked %d txs, promoted=%d, waiting=%d ready=%d",
			hash.Hex()[:10], numBlocked, numPromoted, q.waitingList.Len(), q.readyList.Len())
	}

	// Clean up the completed transaction
	delete(q.hashMap, hash)

	// Remove from participant map only if this tx is still the recorded most-recent
	// for that participant. A newer tx touching the same participant will already
	// have overwritten the entry — leave that alone.
	for _, participant := range entry.participants {
		if current, exists := q.participantMap[participant]; exists && current == entry {
			delete(q.participantMap, participant)
		}
	}

	return numPromoted
}

// IsPending checks if a transaction is in the queue (ready, waiting, or being processed).
// Returns the transaction if found in any location, nil otherwise.
func (q *TxQueueV2) IsPending(txHash common.Hash) *types.Transaction {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Check if transaction is in the pending map (being processed by workers)
	if tx, exists := q.pendingMap[txHash]; exists {
		return tx
	}

	// Check if transaction is in the hash map (ready or waiting list)
	if entry, exists := q.hashMap[txHash]; exists {
		return entry.tx
	}

	return nil
}

// Close signals that no more transactions will be enqueued and initiates shutdown.
// All waiting workers will be woken up and will exit once the ready list is empty.
func (q *TxQueueV2) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.done = true
	q.cond.Broadcast() // Wake up all waiting workers
}

// Handle processes block notifications from the synchronizer and marks transactions as complete.
// This method is designed to be registered as a callback with the block synchronizer.
func (q *TxQueueV2) Handle(ctx context.Context, block *domain.Block) error {
	start := time.Now()
	defer func() { metrics.GatewayTxQueueCompleteDuration.Observe(time.Since(start).Seconds()) }()
	q.mu.Lock()
	defer q.mu.Unlock()

	totalPromoted := 0

	// Mark all transactions in the block as complete
	for _, tx := range block.Transactions {
		txHash := common.BytesToHash(tx.TxHash)
		q.total++
		if tx.Status == 0 {
			q.invalid++
		}

		totalPromoted += q.completeUnlocked(txHash)
	}

	// Signal workers based on how many transactions were promoted
	if totalPromoted == 1 {
		// Only one transaction promoted - wake up one worker
		q.cond.Signal()
	} else if totalPromoted > 1 {
		// Multiple transactions promoted - wake up all workers
		q.cond.Broadcast()
	}

	return nil
}

// Stats returns statistics about processed transactions.
// Returns (total transactions processed, invalid transactions).
// HandleTx processes transaction notifications and marks transactions as complete.
// This method implements the TxHandler interface for use with the notification system.
func (q *TxQueueV2) HandleTx(ctx context.Context, notifs []TxNotification) error {
	start := time.Now()
	defer func() { metrics.GatewayTxQueueCompleteDuration.Observe(time.Since(start).Seconds()) }()
	q.mu.Lock()
	defer q.mu.Unlock()

	totalPromoted := 0

	// Mark all transactions in the batch as complete
	for _, notif := range notifs {
		q.total++
		if notif.Status != committerpb.Status_COMMITTED {
			q.invalid++
		}
		totalPromoted += q.completeUnlocked(notif.EthTxHash)
	}

	loggerV2.Debugf("[QUEUE] HandleTx: notifs=%d promoted=%d ready=%d waiting=%d pending=%d",
		len(notifs), totalPromoted, q.readyList.Len(), q.waitingList.Len(), len(q.pendingMap))

	// Signal workers based on how many transactions were promoted
	if totalPromoted == 1 {
		// Only one transaction promoted - wake up one worker
		q.cond.Signal()
	} else if totalPromoted > 1 {
		// Multiple transactions promoted - wake up all workers
		q.cond.Broadcast()
	}

	return nil
}

func (q *TxQueueV2) Stats() (int, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.total, q.invalid
}

// Helper functions

// removeFromSlice removes the first occurrence of target from the slice.
// Returns a new slice without the target element.
func removeFromSlice[T comparable](slice []T, target T) []T {
	for i, item := range slice {
		if item == target {
			// Remove by swapping with last element and truncating
			lastIdx := len(slice) - 1
			slice[i] = slice[lastIdx]
			return slice[:lastIdx]
		}
	}
	return slice
}
