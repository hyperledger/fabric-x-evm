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
// Seeding: claimed and dispatched start at math.MinInt64, a sentinel that no real
// block number can produce (real values are ≥ -1).  On its very first Handle call
// deliveryShim atomically installs b.Number-1 into both, establishing the real
// base that all subsequent CAS operations build on.  Notification batches that
// arrive before that moment always lose their CAS (MinInt64 ≠ b.Number-1 for any
// real b) and are silently dropped — which is correct, since notification is lossy
// and must not win any block until delivery has proven it can receive from the peer.
//
// Notification reconnects: a stream that ends before the switch is safe to retry
// in place — delivery still covers the chain, so nothing has been lost.  After the
// switch, however, delivery is permanently stopped and notification owns the chain,
// so blocks missed during an outage can never be recovered from the stream and
// reconnecting would leave a permanent hole in the store.  The internal start
// therefore gives up with ErrNotificationLost (wrapping the cause) and Start
// restarts the whole cycle: a fresh delivery synchronizer catches up from the
// store's height, then notification takes over again.  A clean close by the peer is
// treated exactly like a stream error, since it leaves the same gap.
//
// Each cycle is self-contained: it builds its own deliveryShim and delivery
// synchronizer, reseeds the two counters, and joins both services before returning,
// so a block still in flight can never be dispatched into the cycle that follows.
//
// Processing failures are fatal by design: a block that cannot be applied leaves a
// hole in the store that neither path will ever fill.
//
// HybridSynchronizer satisfies the synchronizer.Synchronizer interface
// (Start + Ready) and plugs directly into synchronizer.New as the fabric-x
// implementation.
package hybridx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"
)

var hybridLogger = flogging.MustGetLogger("synchronizer.hybridx")

// ErrNotificationLost reports that the notification stream ended after the switch
// from delivery to notification.  At that point delivery is permanently stopped and
// any reconnect would leave a gap in the store, so the cycle gives up and Start
// restarts it from scratch, letting a fresh delivery synchronizer catch up from the
// last safe height before notification takes over again.
//
// It is internal to the restart loop: Start handles it and never returns it.  The
// error it carries wraps both this sentinel and the cause, so both are reachable
// via errors.Is.
var ErrNotificationLost = errors.New("hybridx: notification stream lost after switch")

// errStreamClosed is the cause recorded when the peer ends the stream without an
// error of its own — a clean server-side close, which the SDK reports as a nil
// error.  Post-switch it leaves the same gap as a failure, so it is treated the
// same way; pre-switch it is simply retried.
var errStreamClosed = errors.New("notification stream closed by peer")

const (
	// retryDelay is how long the notification path waits before reopening a stream
	// that ended before the switch, while delivery still covers the chain.
	retryDelay = time.Second

	// restartDelay is how long Start waits before re-entering a cycle after
	// ErrNotificationLost.  A cycle rebuilds the delivery synchronizer (a fresh gRPC
	// client) and replays from the store's height, so a flapping peer must not be
	// allowed to spin through cycles as fast as it can disconnect.
	restartDelay = time.Second
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
}

// HybridSynchronizer implements the two-phase startup strategy described in
// the package doc.  It satisfies the app.Synchronizer interface.
type HybridSynchronizer struct {
	namespace  string
	handlers   []blocks.BlockHandler
	queueDepth int

	// onSwitch is called when notification takes over; may be nil.  onSwitchOnce
	// keeps it to a single call for the life of the process even though a restart
	// switches again, so callers can treat it as a one-shot.
	onSwitch     func()
	onSwitchOnce sync.Once

	// newDelivery builds a fresh deliverySyncer around that cycle's shim, and is
	// what makes a restart possible: the SDK's network.Synchronizer cannot be
	// restarted after its context is cancelled (a second Start call returns
	// errAlreadyStarted), so each cycle needs an instance of its own.
	newDelivery func(*deliveryShim) (deliverySyncer, error)
	notifPeer   notification.AllTxPeer
	notifReq    *notification.StreamAllRequest

	// delivery holds the current cycle's synchronizer: written at the top of each
	// cycle, read by Ready from whichever goroutine probes readiness.  Nil until the
	// first cycle has built one, which Ready reports as not-ready.
	delivery atomic.Pointer[deliverySyncer]

	// switched is set once notification has taken over the chain, and is the
	// post-switch sentinel for both Ready and the stream retry loop.  It is reset at
	// the top of each cycle, since a restart puts delivery back in charge.
	switched atomic.Bool

	// claimed is the highest block claimed by either path; both take a block by
	// compare-and-swapping it from B-1 to B.  dispatched is the highest block
	// delivery has finished dispatching, written by delivery only, after its whole
	// handler chain has returned.  Both are seeded before a cycle launches either
	// service, so no notification batch can win a CAS against the zero value.
	claimed    atomic.Int64
	dispatched atomic.Int64
}

// OnSwitch registers fn to be called on the goroutine that performs the handoff,
// immediately after the switch from delivery to notification.  It is called at most
// once per process even if the synchronizer restarts and switches again, so fn may
// close a channel.  Must be called before Start.
func (h *HybridSynchronizer) OnSwitch(fn func()) {
	h.onSwitch = fn
}

// New constructs a HybridSynchronizer.
// queueDepth controls the AllTxStreamer's internal channel depth; pass 0 for the default.
// handlers is the initial handler chain fed by both phases.
func New(
	db network.BlockHeightReader,
	channel, namespace string,
	conf network.PeerConf,
	logger sdk.Logger,
	queueDepth int,
	handlers ...blocks.BlockHandler,
) (*HybridSynchronizer, error) {
	h := &HybridSynchronizer{
		namespace:  namespace,
		handlers:   append([]blocks.BlockHandler(nil), handlers...),
		queueDepth: queueDepth,
	}

	// Each cycle gets its own synchronizer and its own shim: the SDK's
	// network.Synchronizer cannot be restarted once stopped, and a shared shim would
	// let a cycle's last in-flight block reseed the cycle that replaced it.
	h.newDelivery = func(shim *deliveryShim) (deliverySyncer, error) {
		s, err := nfabx.NewSynchronizer(db, channel, conf, logger, shim)
		if err != nil {
			return nil, fmt.Errorf("hybridx: create delivery synchronizer: %w", err)
		}
		return s, nil
	}

	notifPeer, err := nfabx.NewPeer(conf, channel)
	if err != nil {
		return nil, fmt.Errorf("hybridx: create notification peer: %w", err)
	}

	h.notifPeer = notifPeer
	h.notifReq = &notification.StreamAllRequest{
		FilterNamespaces:     []string{namespace},
		IncludeReadWriteSets: true,
		IncludeMetadata:      true,
	}
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

// Start runs the two-phase startup in a loop, restarting automatically whenever
// the notification stream ends after the switch from delivery to notification.
// At that point delivery is permanently stopped and reconnecting would leave a gap
// in the store, so the cycle gives up and the next one lets a fresh delivery
// synchronizer catch up from the last safe height before notification takes over
// again.
//
// Start returns only on clean ctx cancellation or an error it cannot recover from.
// Processing panics inside a cycle are not recovered here — they remain fatal.
func (h *HybridSynchronizer) Start(ctx context.Context) error {
	for {
		err := h.start(ctx)
		if !errors.Is(err, ErrNotificationLost) {
			return err
		}
		hybridLogger.Warnf("%v — restarting via delivery in %s", err, restartDelay)
		// Back off before rebuilding: a cycle costs a new gRPC client and a replay
		// from the store's height, so a flapping peer must not spin through them.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(restartDelay):
		}
	}
}

// start runs one cycle of the two-phase startup and blocks until ctx is cancelled
// or the notification stream is lost after the switch (ErrNotificationLost).  Both
// services are joined before it returns, so nothing from this cycle can still be
// dispatching when the next one begins.  Start calls this in a loop.
func (h *HybridSynchronizer) start(ctx context.Context) error {
	// switched means "notification owns the chain and is live", so it belongs to the
	// cycle: it is false while delivery is in charge, and false again once the cycle
	// ends (see the deferred clear below), since by then notification is gone and the
	// delivery synchronizer is stopped.
	h.switched.Store(false)
	defer h.switched.Store(false)

	// A shim per cycle, and a delivery synchronizer around it: the SDK's
	// network.Synchronizer cannot be restarted once stopped, and a shim carried over
	// from the previous cycle could still be holding its off/seeded state.
	delivery, err := h.newDelivery(&deliveryShim{h: h})
	if err != nil {
		return err
	}
	h.delivery.Store(&delivery)

	// claimed and dispatched go back to math.MinInt64 so this cycle's shim installs
	// the real seed on its first Handle call — delivery cannot win a CAS until it has
	// proven it can receive from the peer, and the seed it installs comes from the
	// store's height rather than from the cycle before.
	h.claimed.Store(math.MinInt64)
	h.dispatched.Store(math.MinInt64)

	deliveryCtx, deliveryCancel := context.WithCancel(ctx)
	defer deliveryCancel()

	// ── Notification service ────────────────────────────────────────────────
	gate := &notifGate{
		hybrid:     h,
		dispatcher: NewAllTxBatchDispatcher(&hybridAdapter{h: h}),
		// Safe to call only once the gate has seen dispatched reach the block
		// before its own: this ctx reaches the store's BeginTx, so cancelling
		// mid-dispatch would abort that block.
		stopDelivery: deliveryCancel,
	}
	streamer := notification.NewAllTxStreamer(h.notifPeer, []notification.AllTxHandler{gate}, hybridLogger, h.queueDepth)

	notifErrCh := make(chan error, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				notifErrCh <- nil
				return
			}

			// Stream returns a handler error, a stream error, or nil when the peer
			// closed the stream cleanly.  Handler errors panic inside
			// AllTxBatchDispatcher before they can reach here, so in practice this is
			// either a stream error or a clean close — and post-switch both leave the
			// same gap, so the nil case gets a cause of its own rather than being
			// mistaken for "keep going".
			streamErr := streamer.Stream(ctx, h.notifReq)
			if ctx.Err() != nil {
				notifErrCh <- nil
				return
			}
			if streamErr == nil {
				streamErr = errStreamClosed
			}

			// Post-switch the chain is notification's alone: blocks missed while the
			// stream is down can never be recovered from it, so give up and let Start
			// rebuild the cycle with delivery back in place.
			if h.switched.Load() {
				notifErrCh <- fmt.Errorf("%w: %w", ErrNotificationLost, streamErr)
				return
			}

			// Pre-switch delivery still covers the chain, so a gap here costs nothing
			// and the stream can simply be reopened.
			hybridLogger.Warnf("notification stream ended pre-switch: %v — reopening in %s", streamErr, retryDelay)
			select {
			case <-ctx.Done():
				notifErrCh <- nil
				return
			case <-time.After(retryDelay):
			}
		}
	}()

	// ── Delivery service ────────────────────────────────────────────────────
	deliveryDone := make(chan struct{})
	go func() {
		defer close(deliveryDone)
		if err := delivery.Start(deliveryCtx); err != nil && deliveryCtx.Err() == nil {
			hybridLogger.Warnf("delivery error: %v", err)
		}
	}()

	// Wait for either graceful shutdown or the loss of notification post-switch.
	var cycleErr error
	select {
	case <-ctx.Done():
		cycleErr = <-notifErrCh
	case cycleErr = <-notifErrCh:
	}

	// Join delivery before returning.  Its context is cancelled either by the switch
	// or by the deferred cancel above, but a block can still be in flight inside the
	// handler chain; letting the next cycle start underneath it would dispatch that
	// block against a freshly reseeded claim counter.
	deliveryCancel()
	<-deliveryDone
	return cycleErr
}

// Ready proxies to the delivery synchronizer until the switch; afterwards it
// returns nil, since the notification stream is always considered ready and the
// delivery synchronizer it would otherwise consult has been stopped for good.
//
// The gate sets switched as part of the handoff, so the answer flips the moment
// notification takes over rather than waiting for delivery to notice.  A probe that
// arrives before the first cycle has built a delivery synchronizer — Start runs on
// its own goroutine, so callers can get here first — is simply not ready yet.
func (h *HybridSynchronizer) Ready() error {
	if h.switched.Load() {
		return nil
	}
	d := h.delivery.Load()
	if d == nil {
		return errors.New("hybridx: delivery synchronizer not started")
	}
	return (*d).Ready()
}

// deliveryShim is a blocks.BlockHandler registered with the delivery synchronizer.
// It claims each block before dispatching and publishes completion after, per the
// delivery half of the protocol in the package doc.  It delegates to h.dispatch so
// that the live handler chain (including any handlers added or removed via
// AddHandler/RemoveHandler) is always used.
//
// One shim serves one cycle, alongside the delivery synchronizer it was built for.
// off and seeded need no synchronisation: that synchronizer calls Handle
// sequentially from a single goroutine, and start joins it before the next cycle
// builds its own shim.
type deliveryShim struct {
	h      *HybridSynchronizer
	off    bool
	seeded bool // true once the first Handle call has installed the real seed
}

func (s *deliveryShim) Handle(ctx context.Context, b blocks.Block) error {
	if s.off {
		return nil // notification has taken over; this block is not ours
	}

	n := int64(b.Number)

	// First call: atomically replace the MinInt64 sentinel with the real seed
	// (b.Number - 1).  Notification batches that arrived before this point all
	// lost their CAS against MinInt64 and were dropped, which is correct —
	// notification is lossy and must never win before delivery is established.
	// If the CAS fails here, notification somehow won against MinInt64, which
	// is a bug; the panic surfaces it rather than silently misbehaving.
	if !s.seeded {
		s.seeded = true
		if !s.h.claimed.CompareAndSwap(math.MinInt64, n-1) {
			panic(fmt.Errorf(
				"hybridx: claimed was not MinInt64 on delivery's first block %d — "+
					"notification must not win before delivery is seeded", b.Number))
		}
		s.h.dispatched.Store(n - 1)
		hybridLogger.Infof("delivery seeded at block %d (claimed=%d)", b.Number, n-1)
	}

	if !s.h.claimed.CompareAndSwap(n-1, n) {
		// Notification claimed this block first.  Delivery is done for good: it can
		// only ever offer blocks notification already owns.
		s.off = true
		hybridLogger.Infof("notification claimed block %d first; delivery disabled", b.Number)
		return nil
	}

	if err := s.h.dispatch(ctx, b); err != nil {
		// graceful shutdown
		if ctx.Err() != nil {
			return err
		}

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
// performing the notification half of the protocol in the package doc.  One gate
// serves one cycle: on a restart the next cycle builds its own.
//
// The handoff is recorded on the hybrid rather than the gate, as hybrid.switched:
// Ready and the stream retry loop both consult it from other goroutines, and it has
// to survive the gate it was set on.  AllTxStreamer runs HandleBatch sequentially on
// one handler goroutine per Stream call — not on the stream's receive loop, so the
// waits and dispatches below cost the network path nothing — and Stream does not
// return until that goroutine has exited, so each Stream call's first HandleBatch
// is ordered after the previous call's last one.
type notifGate struct {
	hybrid       *HybridSynchronizer
	dispatcher   *AllTxBatchDispatcher
	stopDelivery func()
}

// hybridAdapter adapts HybridSynchronizer.dispatch to BlockHandler.
type hybridAdapter struct{ h *HybridSynchronizer }

func (a *hybridAdapter) Handle(ctx context.Context, b blocks.Block) error {
	return a.h.dispatch(ctx, b)
}

func (g *notifGate) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	if g.hybrid.switched.Load() {
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
	g.hybrid.switched.Store(true)
	hybridLogger.Infof("switching to notification at block %d", batch.BlockNumber)
	// Once per process, not once per cycle: a restart switches again, and callers
	// treat this as a one-shot (the integration harness closes a channel in it).
	if fn := g.hybrid.onSwitch; fn != nil {
		g.hybrid.onSwitchOnce.Do(fn)
	}
	return g.dispatchBatch(ctx, batch)
}

// waitForDelivery blocks until delivery has finished dispatching block upTo, or
// until ctx is done.  Returns immediately in the common case: either delivery has
// just published upTo, or upTo predates this process and the seed covered it.
func (g *notifGate) waitForDelivery(ctx context.Context, upTo int64) error {
	if g.deliveryReached(upTo) {
		return nil
	}
	hybridLogger.Infof("waiting for delivery to finish block %d before taking over", upTo)

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
		// graceful shutdown
		if ctx.Err() != nil {
			return err
		}

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
