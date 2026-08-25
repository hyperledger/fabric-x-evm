/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package hybridx implements the two-phase startup strategy for fabric-x nodes.
//
// On startup both services run concurrently.  Delivery replays history from the
// store's height; notification receives a batch per committed block in real time
// but stays inert until it can take over.  Ownership of a block is decided by a
// single compare-and-swap, so exactly one path ever dispatches it, and the two
// never run the handler chain at the same time.  Two integers carry the protocol:
//
//   - claimed is the highest block claimed by either path.  A path takes block B
//     by moving claimed from B-1 to B with a compare-and-swap, so exactly one of
//     them can win B.
//   - dispatched is the highest block delivery has finished dispatching.  Only
//     delivery writes it, and only once its whole handler chain has returned.
//
// Delivery, per block B: drop it if disabled; else compare-and-swap claimed from
// B-1 to B.  On failure notification has taken over, so disable delivery for good
// and drop the block.  On success dispatch B, then publish dispatched = B.
//
// Notification, per batch for block B: if already switched, dispatch.  Otherwise
// compare-and-swap claimed from B-1 to B; on failure drop the batch and retry on
// the next one.  On success wait for dispatched to reach B-1 — until delivery has
// finished the block before ours — then stop delivery, flip switched, and dispatch
// B and every batch after it.
//
// Delivery learns it lost only when it attempts its next block, which it does
// between dispatches, so it is never interrupted mid-block.  The wait on
// dispatched is what keeps the two paths from overlapping.
//
// Failures are fatal by design: a block that cannot be applied leaves a hole in
// the store that neither path will ever fill.
//
// HybridSynchronizer satisfies the app.Synchronizer interface (Start + Ready)
// and plugs directly into NewGatewaySynchronizer as the fabric-x implementation.
package hybridx

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"

	evmcommon "github.com/hyperledger/fabric-x-evm/common"
)

// deliveryWaitPoll is how often the notification path re-reads dispatched while
// waiting for delivery to finish the block before its own.  That wait happens
// once per process and spans a single block's dispatch, so it can be short.
const deliveryWaitPoll = time.Millisecond

// deliverySyncer is the subset of *network.Synchronizer used by HybridSynchronizer.
// Extracted as an interface so tests can supply a fake without a real gRPC connection.
type deliverySyncer interface {
	Start(ctx context.Context) error
	Ready() error
	BlockHeight(ctx context.Context) (uint64, error)
}

// HybridSynchronizer implements the two-phase startup strategy described in
// the package doc.  It satisfies the app.Synchronizer interface.
type HybridSynchronizer struct {
	namespace string
	logger    sdk.Logger
	handlers  []blocks.BlockHandler

	delivery  deliverySyncer
	notifPeer notification.AllTxPeer
	notifReq  *notification.StreamAllRequest
	seeder    claimSeeder

	// claimed is the highest block claimed by either path; both take a block by
	// compare-and-swapping it from B-1 to B.  dispatched is the highest block
	// delivery has finished dispatching, written by delivery only, after its whole
	// handler chain has returned.  Both are seeded before Start launches either
	// service, so no notification batch can win a CAS against the zero value.
	claimed    atomic.Int64
	dispatched atomic.Int64
}

// New constructs a HybridSynchronizer.
// handlers is the initial handler chain fed by both phases.
func New(
	db network.BlockHeightReader,
	channel, namespace string,
	conf network.PeerConf,
	signer sdk.Signer,
	logger sdk.Logger,
	handlers ...blocks.BlockHandler,
) (*HybridSynchronizer, error) {
	h := &HybridSynchronizer{
		namespace: namespace,
		logger:    logger,
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
	}

	delivery, err := nfabx.NewSynchronizer(db, channel, conf, signer, logger, &deliveryShim{h: h})
	if err != nil {
		return nil, fmt.Errorf("hybridx: create delivery synchronizer: %w", err)
	}

	notifPeer, err := nfabx.NewPeer(conf, channel, signer)
	if err != nil {
		return nil, fmt.Errorf("hybridx: create notification peer: %w", err)
	}

	h.delivery = delivery
	h.notifPeer = notifPeer
	h.notifReq = &notification.StreamAllRequest{
		FilterNamespaces:     []string{namespace},
		IncludeReadWriteSets: true,
		IncludeMetadata:      true,
	}
	h.seeder = &throwawaySeeder{db: db, channel: channel, conf: conf, signer: signer, h: h}
	return h, nil
}

// dispatch calls Handle on every handler in the chain.
// Called by both the deliveryShim and the notifGate.
func (h *HybridSynchronizer) dispatch(ctx context.Context, b blocks.Block) error {
	for _, bh := range h.handlers {
		if err := bh.Handle(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

// Start runs the two-phase startup and then blocks until ctx is cancelled.
//
// Both services start concurrently.  A notifGate sits between the streamer and
// the handler chain and performs the handoff described in the package doc.
func (h *HybridSynchronizer) Start(ctx context.Context) error {
	// Seed claimed and dispatched before notification starts.
	if err := h.seeder.Seed(ctx); err != nil {
		return err
	}

	deliveryCtx, deliveryCancel := context.WithCancel(ctx)
	defer deliveryCancel()

	// ── Notification service ────────────────────────────────────────────────
	gate := &notifGate{
		hybrid:     h,
		dispatcher: evmcommon.NewAllTxBatchDispatcher(&hybridAdapter{h: h}),
		logger:     h.logger,
		// Safe to call only once the gate has seen dispatched reach the block
		// before its own: this ctx reaches the store's BeginTx, so cancelling
		// mid-dispatch would abort that block.
		stopDelivery: deliveryCancel,
	}
	streamer := notification.NewAllTxStreamer(h.notifPeer, []notification.AllTxHandler{gate}, h.logger)

	notifErrCh := make(chan error, 1)
	go func() {
		const retryDelay = time.Second
		for {
			if ctx.Err() != nil {
				notifErrCh <- nil
				return
			}
			if err := streamer.Stream(ctx, h.notifReq); err != nil && ctx.Err() == nil {
				h.logger.Warnf("hybridx: notification stream error: %v — restarting in %s", err, retryDelay)
				select {
				case <-ctx.Done():
					notifErrCh <- nil
					return
				case <-time.After(retryDelay):
				}
			}
		}
	}()

	// ── Delivery service ────────────────────────────────────────────────────
	go func() {
		defer func() {
			h.delivery = nil // allow GC of the peer and all delivery resources
		}()
		if err := h.delivery.Start(deliveryCtx); err != nil && deliveryCtx.Err() == nil {
			h.logger.Warnf("hybridx: delivery error: %v", err)
		}
	}()

	<-ctx.Done()
	return <-notifErrCh
}

// Ready proxies to the delivery synchronizer until the switch; afterwards
// returns nil since the notification stream is always considered ready.
//
// Note: h.delivery is set to nil from the delivery goroutine in Start; since
// there is no mutex on this field the read here may race in theory, but the
// only consequence is an extra call to delivery.Ready() which is benign.
func (h *HybridSynchronizer) Ready() error {
	if h.delivery == nil {
		return nil
	}
	return h.delivery.Ready()
}

// deliveryShim is a blocks.BlockHandler registered with the delivery synchronizer.
// It claims each block before dispatching and publishes completion after, per the
// delivery half of the protocol in the package doc.  It delegates to h.dispatch so
// that the live handler chain (including any handlers added or removed via
// AddHandler/RemoveHandler) is always used.
//
// off needs no synchronisation: the delivery synchronizer calls Handle
// sequentially from a single goroutine.
type deliveryShim struct {
	h   *HybridSynchronizer
	off bool
}

func (s *deliveryShim) Handle(ctx context.Context, b blocks.Block) error {
	if s.off {
		return nil // notification has taken over; this block is not ours
	}

	n := int64(b.Number)
	if !s.h.claimed.CompareAndSwap(n-1, n) {
		// Notification claimed this block first.  Delivery is done for good: it can
		// only ever offer blocks notification already owns.
		s.off = true
		s.h.logger.Infof("hybridx: notification claimed block %d first; delivery disabled", b.Number)
		return nil
	}

	if err := s.h.dispatch(ctx, b); err != nil {
		// Fatal by design: the claim is not released, so nothing else will ever
		// apply this block and carrying on would leave a permanent hole in the
		// store.  Re-requesting it would almost certainly fail the same way unless
		// the cause was transient I/O.
		// TODO: classify transient store failures so they can be retried in place
		// instead of taking the process down.
		panic(fmt.Errorf("hybridx: delivery handler chain failed on block %d: %w", b.Number, err))
	}

	// Published only after every handler returned, so a reader seeing n knows
	// block n is fully applied.
	s.h.dispatched.Store(n)
	return nil
}

// notifGate is a notification.AllTxHandler that gates the handler chain,
// performing the notification half of the protocol in the package doc.
//
// switched needs no synchronisation: AllTxStreamer calls HandleBatch
// sequentially from the single notification goroutine started in Start.
type notifGate struct {
	hybrid       *HybridSynchronizer
	dispatcher   *evmcommon.AllTxBatchDispatcher
	logger       sdk.Logger
	stopDelivery func()
	switched     bool
}

// hybridAdapter adapts HybridSynchronizer.dispatch to evmcommon.BlockHandler.
type hybridAdapter struct{ h *HybridSynchronizer }

func (a *hybridAdapter) Handle(ctx context.Context, b blocks.Block) error {
	return a.h.dispatch(ctx, b)
}

func (g *notifGate) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	if g.switched {
		return g.dispatchBatch(ctx, batch)
	}

	// Take this block only if delivery is sitting exactly one behind it, and only
	// if we win the race for it: on failure delivery got there first, so drop the
	// batch and try again on the next one.
	n := int64(batch.BlockNumber)
	if !g.hybrid.claimed.CompareAndSwap(n-1, n) {
		return nil
	}

	// We own block n, but delivery may still be finishing n-1 and must not be
	// interrupted.  Wait for it to publish completion before touching the handler
	// chain — this is what keeps the two paths from ever overlapping.
	if err := g.waitForDelivery(ctx, n-1); err != nil {
		return err // shutting down
	}

	// Delivery is now idle between blocks and cannot claim n, so cancelling it
	// here cannot abort a dispatch mid-flight.
	g.stopDelivery()
	g.switched = true
	g.logger.Infof("hybridx: switching to notification at block %d", batch.BlockNumber)
	return g.dispatchBatch(ctx, batch)
}

// waitForDelivery blocks until delivery has finished dispatching block upTo, or
// until ctx is done.  Returns immediately in the common case: either delivery has
// just published upTo, or upTo predates this process and the seed covered it.
func (g *notifGate) waitForDelivery(ctx context.Context, upTo int64) error {
	if g.deliveryReached(upTo) {
		return nil
	}
	g.logger.Infof("hybridx: waiting for delivery to finish block %d before taking over", upTo)

	ticker := time.NewTicker(deliveryWaitPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if g.deliveryReached(upTo) {
				return nil
			}
		}
	}
}

// deliveryReached reports whether delivery has finished exactly block upTo.
//
// dispatched can only ever reach upTo, never pass it: the caller won the claim on
// upTo+1, so delivery cannot have won that block, and its next attempt is the
// compare-and-swap that disables it.  Overshooting therefore means the claim
// protocol itself is broken — surfaced here rather than papered over with a >=.
func (g *notifGate) deliveryReached(upTo int64) bool {
	d := g.hybrid.dispatched.Load()
	if d > upTo {
		panic(fmt.Errorf(
			"hybridx: programming error: delivery dispatched block %d while notification owns block %d",
			d, upTo+1))
	}
	return d == upTo
}

// dispatchBatch converts an AllTxBatch to a blocks.Block via AllTxBatchDispatcher
// and dispatches it through the hybrid's handler chain.
// The dispatcher is allocated once per gate in Start and stored here.
func (g *notifGate) dispatchBatch(ctx context.Context, batch notification.AllTxBatch) error {
	if err := g.dispatcher.HandleBatch(ctx, batch); err != nil {
		// Fatal by design, and unrecoverable in place: the notification stream has
		// no historical replay, so a block dropped here can never be re-fetched —
		// only a restart, which brings delivery back, can repair the store.
		// AllTxBatchDispatcher already panics on a handler error; this covers
		// anything else it can return.
		// TODO: revisit once the stream can be resumed from a past block.
		panic(fmt.Errorf("hybridx: notification dispatch failed on block %d: %w", batch.BlockNumber, err))
	}
	return nil
}
