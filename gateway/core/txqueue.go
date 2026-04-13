/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

// TxQueue is a simple in-memory FIFO queue for transactions
type TxQueue struct {
	mu    sync.Mutex
	cond  *sync.Cond
	queue []*types.Transaction
	done  bool
}

// NewTxQueue creates a new transaction queue
func NewTxQueue() *TxQueue {
	q := &TxQueue{
		queue: make([]*types.Transaction, 0),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue adds a transaction to the queue
func (q *TxQueue) Enqueue(tx *types.Transaction) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.queue = append(q.queue, tx)
	q.cond.Signal() // Wake up one waiting worker
}

// Dequeue removes and returns a transaction from the queue
// Blocks if queue is empty until a transaction is available or queue is closed
func (q *TxQueue) Dequeue() (*types.Transaction, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.queue) == 0 && !q.done {
		q.cond.Wait()
	}

	if q.done && len(q.queue) == 0 {
		return nil, false
	}

	tx := q.queue[0]
	q.queue = q.queue[1:]
	return tx, true
}

// Close signals that no more transactions will be enqueued
func (q *TxQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.done = true
	q.cond.Broadcast() // Wake up all waiting workers
}
