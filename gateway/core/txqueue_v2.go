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
)

var loggerV2 = flogging.MustGetLogger("gateway.core.txqueue_v2")

// ProcessingStartTimestamps is an optional map for tracking when transactions start processing.
// If non-nil, timestamps are recorded when transactions are dequeued from the queue.
// Key: Ethereum transaction hash, Value: T2 timestamp (when dequeued for processing)
var ProcessingStartTimestamps map[common.Hash]time.Time

// ProcessingStartTimestampsMu protects access to ProcessingStartTimestamps
var ProcessingStartTimestampsMu sync.Mutex

// SetTxQueueReadyListSizeMetric is an optional callback for reporting the ready list size.
// If non-nil, it will be called to report the current ready list size.
var SetTxQueueReadyListSizeMetric func(size int)

// SetTxQueueWaitingListSizeMetric is an optional callback for reporting the waiting list size.
// If non-nil, it will be called to report the current waiting list size.
var SetTxQueueWaitingListSizeMetric func(size int)

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
	txHash       common.Hash      // Cached transaction hash
	participants []common.Address // Cached participants (sender/recipient)

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
	participantMap map[common.Address][]*txEntry      // Address -> transactions involving that address
	hashMap        map[common.Hash]*txEntry           // Tx hash -> transaction entry
	pendingMap     map[common.Hash]*types.Transaction // Tx hash -> in-progress transactions

	// Shutdown flag
	done bool

	// Statistics
	total   int
	invalid int

	totalEnq    int
	conflictEnq int
}

// NewTxQueueV2 creates a new dependency-aware transaction queue.
func NewTxQueueV2() *TxQueueV2 {
	q := &TxQueueV2{
		readyList:      list.New(),
		waitingList:    list.New(),
		participantMap: make(map[common.Address][]*txEntry),
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
func (q *TxQueueV2) Enqueue(tx *types.Transaction) {
	// Compute hash and participants outside the lock
	txHash := tx.Hash()
	participants := participantsForTx(tx)

	// Pre-allocate slices outside the lock
	blocks := make([]*txEntry, 0)
	isBlockedBy := make([]*txEntry, 0)

	// Create new entry with pre-computed values
	entry := &txEntry{
		tx:           tx,
		txHash:       txHash,
		participants: participants,
		blocks:       blocks,
		isBlockedBy:  isBlockedBy,
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if already tracked
	if _, exists := q.hashMap[txHash]; exists {
		return
	}

	// Find all transactions that share at least one participant
	conflictingTxs := make(map[*txEntry]bool)
	for _, participant := range entry.participants {
		if txList, exists := q.participantMap[participant]; exists {
			for _, conflictTx := range txList {
				conflictingTxs[conflictTx] = true
			}
		}
	}

	q.totalEnq++

	// If there are conflicts, add dependency links and place in waiting list
	if len(conflictingTxs) > 0 {
		q.conflictEnq++
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

	// Update lookup maps
	q.hashMap[txHash] = entry
	for _, participant := range entry.participants {
		q.participantMap[participant] = append(q.participantMap[participant], entry)
	}
}

// Dequeue removes a transaction from the ready list and moves it to the pending map.
// Blocks if the ready list is empty until a transaction becomes available or the queue is closed.
// Returns (transaction, true) if successful, or (nil, false) if queue is closed and empty.
func (q *TxQueueV2) Dequeue() (*types.Transaction, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for {
		// Report queue size metrics if callbacks are set (inside mutex to get accurate counts)
		if SetTxQueueReadyListSizeMetric != nil {
			SetTxQueueReadyListSizeMetric(q.readyList.Len())
		}
		if SetTxQueueWaitingListSizeMetric != nil {
			SetTxQueueWaitingListSizeMetric(q.waitingList.Len())
		}

		// Wait while ready list is empty and queue is not closed
		for q.readyList.Len() == 0 && !q.done {
			q.cond.Wait()
		}

		// Check if queue is closed and empty
		if q.done && q.readyList.Len() == 0 {
			return nil, false
		}

		// Get transaction from front of ready list
		if elem := q.readyList.Front(); elem != nil {
			entry := elem.Value.(*txEntry)
			tx := entry.tx

			// Remove from ready list
			q.readyList.Remove(elem)
			entry.listElement = nil

			// Keep in participant map so new transactions can find and block on it
			// Will be removed when Complete is called

			// Move to pending map - use cached hash from entry
			q.pendingMap[entry.txHash] = tx

			// Record T2 timestamp if tracking is enabled
			if ProcessingStartTimestamps != nil {
				ProcessingStartTimestampsMu.Lock()
				ProcessingStartTimestamps[entry.txHash] = time.Now() // T2: dequeued for processing
				ProcessingStartTimestampsMu.Unlock()
			}

			loggerV2.Debugf("Dequeue: tx %s, waiting=%d ready=%d pending=%d",
				entry.txHash.Hex()[:10], q.waitingList.Len(), q.readyList.Len(), len(q.pendingMap))

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

	// Remove from participant map (now safe for new transactions with same participants)
	for _, participant := range entry.participants {
		if txList, exists := q.participantMap[participant]; exists {
			q.participantMap[participant] = removeFromSlice(txList, entry)
			if len(q.participantMap[participant]) == 0 {
				delete(q.participantMap, participant)
			}
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
	// Pre-compute all transaction hashes outside the lock
	txHashes := make([]common.Hash, len(block.Transactions))
	statuses := make([]uint8, len(block.Transactions))
	for i, tx := range block.Transactions {
		txHashes[i] = common.BytesToHash(tx.TxHash)
		statuses[i] = tx.Status
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	totalPromoted := 0

	// Mark all transactions in the block as complete
	for i, txHash := range txHashes {
		q.total++
		if statuses[i] == 0 {
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
	// Pre-extract transaction hashes and statuses outside the lock
	numNotifs := len(notifs)
	txHashes := make([]common.Hash, numNotifs)
	statuses := make([]committerpb.Status, numNotifs)
	for i, notif := range notifs {
		txHashes[i] = notif.EthTxHash
		statuses[i] = notif.Status
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	totalPromoted := 0

	// Mark all transactions in the batch as complete
	for i := 0; i < numNotifs; i++ {
		q.total++
		if statuses[i] != committerpb.Status_COMMITTED {
			q.invalid++
		}
		totalPromoted += q.completeUnlocked(txHashes[i])
	}

	loggerV2.Debugf("[QUEUE] HandleTx: notifs=%d promoted=%d ready=%d waiting=%d pending=%d",
		numNotifs, totalPromoted, q.readyList.Len(), q.waitingList.Len(), len(q.pendingMap))

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

func (q *TxQueueV2) Stats() (int, int, int, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.total, q.invalid, q.totalEnq, q.conflictEnq
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
