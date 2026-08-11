/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// TxPool is the gateway's view of what it still owes a block, so a rewind can
// drain the real queue instead of a count kept alongside it.
type TxPool interface {
	// InFlight returns how many transactions are queued or being processed.
	InFlight() int
}

// txFence keeps evm_snapshot/evm_revert from moving the ledger out from under
// transactions the test RPC has accepted but not yet seen committed.
//
// It guards the submit path only up to the enqueue, not all the way to the
// commit. Once a rewind sets rewinding, nothing further can reach the queue, so
// draining the queue drains everything that could still land - and a transaction
// that never commits delays nobody, because the fence is long since released.
//
// The zero value needs a TxPool. Test-only: the production gateway has no revert
// and must never wait for the queue to drain on the submit path.
type txFence struct {
	mu        sync.Mutex
	rewinding bool
	pool      TxPool
}

// errFenced rejects a submission arriving mid-rewind. Queueing it would admit it
// the instant the rewind finished, applying a transaction built against the old
// state to the new one.
var errFenced = errors.New("transaction rejected: a snapshot or revert is in progress")

// errDrainTimeout fails a rewind whose queued transactions never commit.
var errDrainTimeout = errors.New("timed out waiting for in-flight transactions to commit")

const (
	// drainTimeout bounds the wait, under Hardhat's 60s request timeout and Mocha's
	// 40s test timeout so the error surfaces instead of a hook timeout.
	drainTimeout = 30 * time.Second
	// drainPollInterval paces it; InFlight is a couple of map reads under a lock.
	drainPollInterval = time.Millisecond
)

// submit runs enqueue under the fence, so a rewind either sees the transaction in
// the queue afterwards or turns it away. enqueue must return once the gateway has
// taken the transaction, and must not wait for it to commit.
func (f *txFence) submit(enqueue func() (common.Hash, error)) (common.Hash, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rewinding {
		return common.Hash{}, errFenced
	}
	return enqueue()
}

// beginRewind locks out new submissions, then waits for the queue to drain. Pair a
// nil return with endRewind; callers must serialise their own rewinds.
func (f *txFence) beginRewind(ctx context.Context) error {
	f.mu.Lock()
	f.rewinding = true
	f.mu.Unlock()

	deadline := time.NewTimer(drainTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(drainPollInterval)
	defer poll.Stop()

	for f.pool.InFlight() > 0 {
		select {
		case <-ctx.Done():
			f.endRewind()
			return ctx.Err()
		case <-deadline.C:
			f.endRewind()
			return errDrainTimeout
		case <-poll.C:
		}
	}
	return nil
}

// endRewind reopens the fence to submissions.
func (f *txFence) endRewind() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rewinding = false
}
