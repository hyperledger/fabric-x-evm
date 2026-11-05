/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ──────────────────────────────────────────────────────────────────

type stubHeight struct{}

func (stubHeight) BlockNumber(context.Context) (uint64, error) { return 0, nil }

// failingHandler fails every block with err.
type failingHandler struct{ err error }

func (f failingHandler) Handle(context.Context, blocks.Block) error { return f.err }

// panicValue runs fn and returns what it panicked with, or nil.
func panicValue(fn func()) (v any) {
	defer func() { v = recover() }()
	fn()
	return nil
}

// flakyPeer fails its first stream and then holds the stream open until ctx ends.
type flakyPeer struct {
	mu    sync.Mutex
	calls int
}

func (p *flakyPeer) StreamAllTransactions(ctx context.Context, _ *notification.StreamAllRequest, _ notification.AllTxProcessor) error {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	if first {
		return errors.New("stream broke")
	}
	<-ctx.Done()
	return nil
}

func (p *flakyPeer) streams() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// ── New ──────────────────────────────────────────────────────────────────────

// New subscribes to the node's namespace only, with read-write sets and metadata.
func TestNew_BuildsFromPeerConf(t *testing.T) {
	first, second := &recordingHandler{}, &recordingHandler{}
	handlers := []blocks.BlockHandler{first, second}

	h, err := New(stubHeight{}, "ch", "evm", network.PeerConf{Address: "127.0.0.1:1"}, nil, 0, handlers...)
	require.NoError(t, err)

	assert.Equal(t, "evm", h.namespace)
	assert.NotNil(t, h.delivery)
	assert.NotNil(t, h.notifPeer)
	assert.Equal(t, []string{"evm"}, h.notifReq.FilterNamespaces)
	assert.True(t, h.notifReq.IncludeReadWriteSets)
	assert.True(t, h.notifReq.IncludeMetadata)
	assert.Equal(t, []blocks.BlockHandler{first, second}, h.handlers)

	handlers[0] = nil
	assert.Same(t, first, h.handlers[0], "the handler chain is copied, not shared with the caller")
}

func TestNew_RejectsBadPeerConf(t *testing.T) {
	for name, conf := range map[string]network.PeerConf{
		"address without a port": {Address: "no-port"},
		"invalid tls":            {Address: "127.0.0.1:1", TLS: network.TLSConfig{Mode: network.TLSModeTLS}},
	} {
		t.Run(name, func(t *testing.T) {
			h, err := New(stubHeight{}, "ch", "evm", conf, nil, 0)
			require.ErrorContains(t, err, "hybridx: create delivery synchronizer")
			assert.Nil(t, h)
		})
	}
}

// ── OnSwitch ─────────────────────────────────────────────────────────────────

// OnSwitch runs once, when notification takes over, after delivery is stopped.
func TestOnSwitch_CalledOnceAfterDeliveryStops(t *testing.T) {
	delivery := newFakeDelivery()
	h := newHybrid(t, delivery, newFakeNotifPeer(), &recordingHandler{})
	h.seedClaimAt(5)
	gate, stopped := newGate(h)

	var calls int
	var stoppedAtCall bool
	h.OnSwitch(func() {
		calls++
		stoppedAtCall = *stopped
	})

	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 5)))
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 6)))
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 7)))

	assert.Equal(t, 1, calls, "once, at the handoff, not on every batch")
	assert.True(t, stoppedAtCall, "delivery is already stopped when the callback runs")
}

func TestOnSwitch_NotCalledWhileDeliveryIsBehind(t *testing.T) {
	h := newHybrid(t, newFakeDelivery(), newFakeNotifPeer(), &recordingHandler{})
	h.seedClaimAt(5)
	gate, _ := newGate(h)

	var calls int
	h.OnSwitch(func() { calls++ })

	// Block 9 cannot be claimed while delivery is only at block 4: the batch is dropped.
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 9)))

	assert.Zero(t, calls)
}

// ── dispatch ─────────────────────────────────────────────────────────────────

// A failing handler stops the chain, so no later store gets the block.
func TestDispatch_StopsAtFirstFailingHandler(t *testing.T) {
	before, after := &recordingHandler{}, &recordingHandler{}
	boom := errors.New("boom")
	h := &HybridSynchronizer{handlers: []blocks.BlockHandler{before, failingHandler{boom}, after}}

	err := h.dispatch(context.Background(), blocks.Block{Number: 7})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, []uint64{7}, blockNumbers(before.received()))
	assert.Empty(t, after.received())
}

// ── Start: notification stream errors ────────────────────────────────────────

// A failed notification stream restarts after one second, not before.
func TestStart_RestartsNotificationStreamAfterOneSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := &flakyPeer{}
		h := &HybridSynchronizer{delivery: delivery, notifPeer: peer, notifReq: &notification.StreamAllRequest{}}
		delivery.handler = &deliveryShim{h: h}

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		synctest.Wait()
		require.Equal(t, 1, peer.streams(), "first stream fails straight away")

		time.Sleep(time.Second - time.Millisecond)
		synctest.Wait()
		require.Equal(t, 1, peer.streams(), "not restarted before the delay")

		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.Equal(t, 2, peer.streams(), "restarted after the delay")

		cancel()
		synctest.Wait()
		require.NoError(t, <-errCh)
		assert.Equal(t, 2, peer.streams())
	})
}

// Cancelling during the restart delay ends Start without another attempt.
func TestStart_CancelDuringStreamRestartDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := &flakyPeer{}
		h := &HybridSynchronizer{delivery: delivery, notifPeer: peer, notifReq: &notification.StreamAllRequest{}}
		delivery.handler = &deliveryShim{h: h}

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		synctest.Wait()
		require.Equal(t, 1, peer.streams())

		time.Sleep(500 * time.Millisecond)
		cancel()
		synctest.Wait()

		require.NoError(t, <-errCh)
		assert.Equal(t, 1, peer.streams(), "no restart after cancellation")
	})
}

// ── delivery shim ────────────────────────────────────────────────────────────

// If notification claimed a block before delivery seeded the counters, delivery panics.
func TestDeliveryShim_PanicsIfNotificationClaimedBeforeSeed(t *testing.T) {
	delivery := newFakeDelivery()
	h := newHybrid(t, delivery, newFakeNotifPeer(), &recordingHandler{})
	h.claimed.Store(3) // anything but the MinInt64 sentinel

	v := panicValue(func() { _ = delivery.handler.Handle(context.Background(), blocks.Block{Number: 7}) })

	require.NotNil(t, v)
	assert.Contains(t, fmt.Sprint(v), "claimed was not MinInt64")
}

// A handler failure is fatal for delivery: a hole in the store would be worse.
func TestDeliveryShim_PanicsWhenHandlerChainFails(t *testing.T) {
	delivery := newFakeDelivery()
	h := newHybrid(t, delivery, newFakeNotifPeer(), failingHandler{errors.New("disk full")})
	h.seedClaimAt(5)

	v := panicValue(func() { _ = delivery.handler.Handle(context.Background(), blocks.Block{Number: 5}) })

	require.NotNil(t, v)
	assert.Contains(t, fmt.Sprint(v), "delivery handler chain failed on block 5")
	assert.Contains(t, fmt.Sprint(v), "disk full")
}

// During shutdown the failure is returned, not a panic, and the block is not published.
func TestDeliveryShim_ReturnsHandlerErrorDuringShutdown(t *testing.T) {
	delivery := newFakeDelivery()
	boom := errors.New("context canceled mid-write")
	h := newHybrid(t, delivery, newFakeNotifPeer(), failingHandler{boom})
	h.seedClaimAt(5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var err error
	v := panicValue(func() { err = delivery.handler.Handle(ctx, blocks.Block{Number: 5}) })

	assert.Nil(t, v, "no panic on shutdown")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, int64(4), h.dispatched.Load(), "an unfinished block is not published")
}

// ── notification gate ────────────────────────────────────────────────────────

// Shutting down while waiting for delivery ends the wait without switching.
func TestGate_ShutdownWhileWaitingForDelivery(t *testing.T) {
	handler := &recordingHandler{}
	h := newHybrid(t, newFakeDelivery(), newFakeNotifPeer(), handler)
	h.seedClaimAt(5)
	h.dispatched.Store(3) // delivery has not yet finished block 4
	gate, stopped := newGate(h)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- gate.HandleBatch(ctx, evmBatch(t, 5)) }()

	cancel()

	require.ErrorIs(t, <-errCh, context.Canceled)
	assert.False(t, *stopped, "delivery is not stopped")
	assert.False(t, gate.switched)
	assert.Empty(t, handler.received())
}

// After the switch, a handler error during shutdown is returned, not a panic.
func TestGate_DispatchErrorDuringShutdownIsReturned(t *testing.T) {
	boom := errors.New("context canceled mid-write")
	h := newHybrid(t, newFakeDelivery(), newFakeNotifPeer(), failingHandler{boom})
	h.seedClaimAt(5)
	gate, _ := newGate(h)
	gate.switched = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var err error
	v := panicValue(func() { err = gate.HandleBatch(ctx, evmBatch(t, 5)) })

	assert.Nil(t, v, "no panic on shutdown")
	require.ErrorIs(t, err, boom)
}
