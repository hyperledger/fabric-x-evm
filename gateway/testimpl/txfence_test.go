/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// fakePool stands in for the gateway's transaction queue.
type fakePool struct{ inFlight atomic.Int64 }

func (p *fakePool) InFlight() int { return int(p.inFlight.Load()) }
func (p *fakePool) enqueue()      { p.inFlight.Add(1) }
func (p *fakePool) commit()       { p.inFlight.Add(-1) }
func newFencedPool() (*txFence, *fakePool) {
	pool := &fakePool{}
	return &txFence{pool: pool}, pool
}

// enqueueOK is a submission that reaches the queue.
func enqueueOK(pool *fakePool) func() (common.Hash, error) {
	return func() (common.Hash, error) {
		pool.enqueue()
		return common.Hash{0x1}, nil
	}
}

// A rewind must not rewind the ledger while a transaction it admitted is still
// waiting for a block - the invariant the fence exists for.
func TestTxFence_RewindWaitsForQueueToDrain(t *testing.T) {
	fence, pool := newFencedPool()

	if _, err := fence.submit(enqueueOK(pool)); err != nil {
		t.Fatalf("submit on an idle fence: %v", err)
	}

	rewinding := make(chan error, 1)
	go func() { rewinding <- fence.beginRewind(context.Background()) }()

	select {
	case <-rewinding:
		t.Fatal("rewind proceeded with a transaction still queued")
	case <-time.After(50 * time.Millisecond):
	}

	pool.commit()
	select {
	case err := <-rewinding:
		if err != nil {
			t.Fatalf("beginRewind: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rewind did not proceed after the queue drained")
	}
}

// The reject window: once a rewind is under way, a submission built against the
// pre-rewind state must be turned away, never queued to run after it.
func TestTxFence_RejectsWhileRewinding(t *testing.T) {
	fence, pool := newFencedPool()

	if err := fence.beginRewind(context.Background()); err != nil {
		t.Fatalf("beginRewind on an idle fence: %v", err)
	}
	if _, err := fence.submit(enqueueOK(pool)); !errors.Is(err, errFenced) {
		t.Fatalf("submit during a rewind = %v, want errFenced", err)
	}
	if got := pool.InFlight(); got != 0 {
		t.Fatalf("rejected submission still reached the queue (InFlight=%d)", got)
	}

	fence.endRewind()
	if _, err := fence.submit(enqueueOK(pool)); err != nil {
		t.Fatalf("submit after the rewind finished: %v", err)
	}
}

// A queue that never drains must not pin the fence; the rewind returns on
// whichever bound fires first and reopens the fence on its way out.
func TestTxFence_BeginRewindTimesOutOnStuckQueue(t *testing.T) {
	fence, pool := newFencedPool()
	if _, err := fence.submit(enqueueOK(pool)); err != nil {
		t.Fatalf("submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// drainTimeout is 30s, so the context bound is the one that fires here.
	if err := fence.beginRewind(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("beginRewind = %v, want the context bound to fire", err)
	}

	// A failed rewind must not leave the fence closed for everyone else.
	if _, err := fence.submit(enqueueOK(pool)); err != nil {
		t.Fatalf("submit after a failed rewind: %v", err)
	}
}

// The fence covers the enqueue, so a rewind either sees a transaction in the
// queue or rejects it; it can never slip past into the post-rewind state.
func TestTxFence_RewindNeverRacesAnEnqueue(t *testing.T) {
	fence, pool := newFencedPool()

	// Block inside the enqueue, so the rewind arrives mid-submission.
	enqueueStarted := make(chan struct{})
	releaseEnqueue := make(chan struct{})
	submitted := make(chan error, 1)
	go func() {
		_, err := fence.submit(func() (common.Hash, error) {
			close(enqueueStarted)
			<-releaseEnqueue
			pool.enqueue()
			return common.Hash{0x1}, nil
		})
		submitted <- err
	}()
	<-enqueueStarted

	rewinding := make(chan error, 1)
	go func() { rewinding <- fence.beginRewind(context.Background()) }()

	// The rewind cannot even start while the enqueue holds the fence.
	select {
	case <-rewinding:
		t.Fatal("rewind started while an enqueue was in progress")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseEnqueue)
	if err := <-submitted; err != nil {
		t.Fatalf("submit: %v", err)
	}

	// It is now in the queue, so the rewind must wait for it rather than miss it.
	select {
	case <-rewinding:
		t.Fatal("rewind proceeded without draining the transaction it raced")
	case <-time.After(50 * time.Millisecond):
	}

	pool.commit()
	select {
	case err := <-rewinding:
		if err != nil {
			t.Fatalf("beginRewind: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rewind did not proceed after the queue drained")
	}
}
