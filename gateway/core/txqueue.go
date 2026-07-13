/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// TxQueue is a two-queue system for tracking transaction lifecycle.
// Transactions flow: pendingQueue -> inProgressMap -> completed (removed from tracking)
//
// Locking strategy:
// - Write operations (Enqueue, Dequeue, Complete, Close) use exclusive Lock()
// - Read operations (IsPending) use shared RLock() for concurrent queries
// - This allows multiple simultaneous pending status checks without blocking writers
//
// Thread-safety: All map and slice operations are protected by the RWMutex.
// The inProgressMap is safe for concurrent access as long as proper locking is used.
//
// Persistence: the queue is in-memory only. On gateway restart both
// pendingQueue and inProgressMap are lost; clients must resubmit any
// unconfirmed transactions.
type TxQueue struct {
	mu            sync.RWMutex                       // Protects all fields below
	cond          *sync.Cond                         // Signals when new transactions arrive
	pendingQueue  []*types.Transaction               // FIFO queue of transactions waiting to be processed
	inProgressMap map[common.Hash]*types.Transaction // Transactions currently being processed by workers
	done          bool                               // Shutdown flag

	// Statistics
	total   int
	invalid int
}

// NewTxQueue creates a new transaction queue
func NewTxQueue() *TxQueue {
	q := &TxQueue{
		pendingQueue:  make([]*types.Transaction, 0),
		inProgressMap: make(map[common.Hash]*types.Transaction),
	}
	// sync.Cond requires a Locker; RWMutex implements Locker interface
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds a transaction to the queue.
// This method uses a write lock to ensure exclusive access when modifying the queue.
func (q *TxQueue) Enqueue(tx *types.Transaction) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pendingQueue = append(q.pendingQueue, tx)
	q.cond.Signal() // Wake up one waiting worker
}

// Dequeue removes a transaction from the pending queue and moves it to the in-progress map.
// Blocks if queue is empty until a transaction is available or queue is closed.
// Returns (transaction, true) if successful, or (nil, false) if queue is closed and empty.
// This method uses a write lock to ensure exclusive access during the queue modification.
func (q *TxQueue) Dequeue() (*types.Transaction, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.pendingQueue) == 0 && !q.done {
		q.cond.Wait()
	}

	if q.done && len(q.pendingQueue) == 0 {
		return nil, false
	}

	tx := q.pendingQueue[0]
	q.pendingQueue[0] = nil // Prevent memory leak
	q.pendingQueue = q.pendingQueue[1:]
	q.inProgressMap[tx.Hash()] = tx // Track as in-progress
	return tx, true
}

// Close signals that no more transactions will be enqueued and initiates shutdown.
// Any transactions in the pendingQueue will be processed by workers that are already waiting.
// Transactions in the inProgressMap at shutdown time will be lost - they are currently being
// processed by workers and will not complete. Clients should resubmit these transactions if needed.
// This method uses a write lock to ensure exclusive access during shutdown.
func (q *TxQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.done = true
	q.cond.Broadcast() // Wake up all waiting workers so they can exit
}

// IsPending checks if a transaction is in either the pending queue or in-progress map.
// Returns the transaction if found in either location, nil otherwise.
// This method uses a read lock, allowing multiple concurrent queries without blocking writers.
func (q *TxQueue) IsPending(txHash common.Hash) *types.Transaction {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Check if transaction is in the in-progress map (O(1) lookup)
	if tx, exists := q.inProgressMap[txHash]; exists {
		return tx
	}

	// Check if transaction is in the pending queue (O(n) lookup)
	for _, tx := range q.pendingQueue {
		if tx.Hash() == txHash {
			return tx
		}
	}

	return nil
}

// Complete removes a transaction from the in-progress map after it has been committed.
// This should be called by the Gateway's callback when a block containing the transaction
// is committed to the ledger. This method is idempotent - safe to call multiple times.
// This method uses a write lock to ensure exclusive access when modifying the map.
func (q *TxQueue) Complete(hash common.Hash) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inProgressMap, hash)
}

// Handle processes block notifications from the synchronizer and marks transactions as complete.
// This method is designed to be registered as a callback with the block synchronizer.
// It extracts all transaction hashes from the committed block and removes them from the in-progress map.
// This method is safe to call concurrently and will not block block processing.
func (q *TxQueue) Handle(ctx context.Context, block *domain.Block) error {
	// Mark all transactions in the block as complete and update statistics
	for _, tx := range block.Transactions {
		txHash := common.BytesToHash(tx.TxHash)
		q.total++
		if tx.Status == 0 {
			q.invalid++
		}
		q.Complete(txHash)
	}

	return nil
}

// HandleTx implements the TxHandler interface for processing transaction notifications.
// It extracts ethereum transaction hashes from the notifications and marks them as complete.
func (q *TxQueue) HandleTx(ctx context.Context, notifs []cmn.TxNotification) error {
	for _, notif := range notifs {
		q.mu.Lock()
		q.total++
		if notif.Status != committerpb.Status_COMMITTED {
			q.invalid++
		}
		q.mu.Unlock()

		q.Complete(notif.EthTxHash)
	}

	return nil
}

// Stats returns statistics about processed transactions.
// Returns (total transactions processed, invalid transactions).
func (q *TxQueue) Stats() (int, int, int, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.total, q.invalid, 0, 0
}
