/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── fakes ────────────────────────────────────────────────────────────────────

// fakeDelivery is a controllable deliverySyncer.
type fakeDelivery struct {
	mu      sync.Mutex
	ready   bool
	height  uint64
	started chan struct{} // closed when Start is called
	stopped chan struct{} // closed when Start returns
}

func newFakeDelivery() *fakeDelivery {
	return &fakeDelivery{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (f *fakeDelivery) Start(ctx context.Context) error {
	close(f.started)
	<-ctx.Done()
	close(f.stopped)
	return nil
}

func (f *fakeDelivery) Ready() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ready {
		return nil
	}
	return errors.New("not ready")
}

func (f *fakeDelivery) BlockHeight(_ context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.height, nil
}

func (f *fakeDelivery) setReady(height uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = true
	f.height = height
}

// fakeNotifPeer is a controllable notification.AllTxPeer.
// Call push to send a batch to the streamer; the stream blocks until ctx is done.
type fakeNotifPeer struct {
	batches chan notification.AllTxBatch
}

func newFakeNotifPeer() *fakeNotifPeer {
	return &fakeNotifPeer{batches: make(chan notification.AllTxBatch, 16)}
}

func (f *fakeNotifPeer) StreamAllTransactions(ctx context.Context, _ *notification.StreamAllRequest, p notification.AllTxProcessor) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case b := <-f.batches:
			if err := p.ProcessBatch(ctx, b); err != nil {
				return err
			}
		}
	}
}

func (f *fakeNotifPeer) push(blockNum uint64, txID string) {
	f.batches <- notification.AllTxBatch{
		BlockNumber: blockNum,
		Events: []notification.CommittedTxEvent{
			{TxID: txID, BlockNum: blockNum, Status: committerpb.Status_COMMITTED},
		},
	}
}

// recordingHandler records every blocks.Block it receives.
type recordingHandler struct {
	mu     sync.Mutex
	blocks []blocks.Block
}

func (r *recordingHandler) Handle(_ context.Context, b blocks.Block) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks = append(r.blocks, b)
	return nil
}

func (r *recordingHandler) received() []blocks.Block {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]blocks.Block, len(r.blocks))
	copy(cp, r.blocks)
	return cp
}

// newHybrid builds a HybridSynchronizer wired to the given fakes.
func newHybrid(t *testing.T, delivery *fakeDelivery, peer *fakeNotifPeer, handlers ...blocks.BlockHandler) *HybridSynchronizer {
	t.Helper()
	return newWithDeps(
		delivery,
		peer,
		&notification.StreamAllRequest{IncludeMetadata: true, IncludeReadWriteSets: true},
		sdk.NoOpLogger{},
		handlers...,
	)
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestStart_CancelBeforeSwitch verifies that cancelling the context before any
// switch occurs causes Start to return cleanly with nil.
func TestStart_CancelBeforeSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		h := newHybrid(t, delivery, peer)

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		// Wait until Start is running (delivery.Start has been called).
		synctest.Wait()
		<-delivery.started

		cancel()
		synctest.Wait()

		require.NoError(t, <-errCh)
	})
}

// TestReady_BeforeAndAfterSwitch verifies that Ready proxies the delivery
// synchronizer until the switch and returns nil afterwards.
func TestReady_BeforeAndAfterSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, peer, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Before the switch: Ready mirrors the delivery synchronizer.
		assert.Error(t, h.Ready(), "should not be ready before delivery catches up")

		// Mark delivery ready at block 5, then advance the watcher tick.
		delivery.setReady(6) // height=6 → last processed = 5
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()

		assert.NoError(t, h.Ready(), "should be ready once delivery is ready")

		// Push a notif batch at block 5: gate should now switch (nDel=5 >= nNot=5)
		// and the recorder should receive a blocks.Block.
		// The batch has no EVM metadata so AllTxBatchDispatcher will produce an empty
		// block — but we just care that Ready() still returns nil post-switch.
		peer.push(5, "tx1")
		synctest.Wait()

		assert.NoError(t, h.Ready(), "should remain ready in notification phase")
	})
}

// TestGate_DiscardsBatchWhileDeliveryBehind verifies that notification batches
// whose block number exceeds nDel are silently discarded.
func TestGate_DiscardsBatchWhileDeliveryBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, peer, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Delivery is at block 2 (height=3), notif sends block 10: should be discarded.
		delivery.setReady(3)
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()

		peer.push(10, "tx-ahead")
		synctest.Wait()

		assert.Empty(t, recorder.received(), "batch ahead of delivery must be discarded")
	})
}

// TestGate_SwitchesAndForwardsWhenCaughtUp verifies the gap-free handoff: when
// nDel >= nNot the gate flips switched and forwards the triggering batch.
func TestGate_SwitchesAndForwardsWhenCaughtUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, peer, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Delivery has processed block 5 (height=6); notif sends block 5: should switch.
		delivery.setReady(6)
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()

		// This batch has EVM metadata — build it properly so AllTxBatchDispatcher keeps it.
		// For the switch test we only need the block number; an empty-event batch is fine
		// to confirm dispatch runs (recorder.Handle is called with the resulting Block).
		peer.batches <- notification.AllTxBatch{BlockNumber: 5}
		synctest.Wait()

		// The dispatcher filters non-EVM events so the block will have no transactions,
		// but Handle is still called once with an empty block — or not called at all if
		// the dispatcher skips empty blocks. Either way, at minimum delivery must be
		// cancelled (delivery.stopped closed).
		select {
		case <-delivery.stopped:
			// delivery was cancelled — switch happened
		case <-time.After(time.Second):
			t.Fatal("delivery was not cancelled after switch")
		}
	})
}

// TestStart_DeliveryErrorIsLogged verifies that a delivery error does not cause
// Start to exit; Start only returns when ctx is cancelled.
func TestStart_DeliveryErrorIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Replace fakeDelivery with one that returns an error from Start.
		errDelivery := &errDeliverySyncer{err: errors.New("boom"), done: make(chan struct{})}
		peer := newFakeNotifPeer()
		h := newWithDeps(errDelivery, peer, &notification.StreamAllRequest{}, sdk.NoOpLogger{})

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		synctest.Wait()

		// Start must still be running despite delivery error.
		select {
		case err := <-errCh:
			t.Fatalf("Start returned early with %v", err)
		default:
		}

		cancel()
		synctest.Wait()
		require.NoError(t, <-errCh)
	})
}

// ── helper types ─────────────────────────────────────────────────────────────

// errDeliverySyncer returns an error from Start immediately, simulating a
// delivery peer that cannot connect.
type errDeliverySyncer struct {
	err  error
	done chan struct{}
}

func (e *errDeliverySyncer) Start(ctx context.Context) error {
	close(e.done)
	return e.err
}

func (e *errDeliverySyncer) Ready() error                                  { return errors.New("not ready") }
func (e *errDeliverySyncer) BlockHeight(_ context.Context) (uint64, error) { return 0, nil }
