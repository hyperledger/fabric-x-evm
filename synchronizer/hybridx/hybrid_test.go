/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-evm/common"
)

// ── fakes ────────────────────────────────────────────────────────────────────

// fakeDelivery is a controllable deliverySyncer for single-cycle tests. shim
// stands in for the deliveryShim the real synchronizer would be built around;
// tests use deliver to push blocks through it, which is what advances the claim
// counter.  Start may only be called once, so tests that restart use
// deliveryFactory instead.
//
// stopped mirrors the real synchronizer: once its context is cancelled it is
// permanently stopped and reports not-ready for good.
type fakeDelivery struct {
	mu      sync.Mutex
	ready   bool
	shim    *deliveryShim
	started chan struct{} // closed when Start is called
	stopped chan struct{} // closed when Start returns
}

func newFakeDelivery() *fakeDelivery {
	return &fakeDelivery{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// deliver pushes a block through the registered shim, as the real delivery
// synchronizer's block processor would.
func (f *fakeDelivery) deliver(t *testing.T, num uint64) {
	t.Helper()
	require.NoError(t, f.shim.Handle(context.Background(), blocks.Block{Number: num}))
}

func (f *fakeDelivery) Start(ctx context.Context) error {
	close(f.started)
	<-ctx.Done()
	close(f.stopped)
	return nil
}

func (f *fakeDelivery) Ready() error {
	select {
	case <-f.stopped:
		// What network.Synchronizer reports in stateStopped: a stopped synchronizer
		// never becomes ready again.
		return errors.New("stopped")
	default:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ready {
		return nil
	}
	return errors.New("not ready")
}

func (f *fakeDelivery) setReady() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = true
}

// seedHeight returns the first block number delivery would stream from the given
// store height — used by tests that drive the gate/shim directly (without start)
// to call seedClaimAt with the right value.
func (f *fakeDelivery) seedHeight(height uint64) uint64 {
	if height > 1 {
		return height // delivery resumes at height; peer delivers block height first
	}
	return 0 // delivery resumes at 0; peer delivers block 0 first
}

// seedClaimAt seeds claimed and dispatched to first-1, simulating the state after
// delivery has already processed first-1 blocks.  Tests that drive the gate or
// shim directly (without calling start) use this so the CAS invariants are
// satisfied from the start.  It also marks the shim as seeded so the MinInt64
// guard in Handle does not misfire.
func (f *fakeDelivery) seedClaimAt(h *HybridSynchronizer, first uint64) {
	seed := int64(first) - 1
	h.claimed.Store(seed)
	h.dispatched.Store(seed)
	f.shim.seeded = true
}

// cycleDelivery is one cycle's deliverySyncer, handed out by deliveryFactory.
// Unlike fakeDelivery it is built fresh per cycle, so a restart gets a new one —
// exactly as the real synchronizer does.
//
// inFlight, if set, models a block still inside the handler chain when the cycle's
// context is cancelled: Start does not return until that block has been pushed
// through the shim.  The real network.Synchronizer behaves the same way — its
// SubscribeBlocks call can be mid-ProcessBlock when cancellation arrives — so a
// cycle that did not join delivery would let this block land in the next one.
type cycleDelivery struct {
	shim     *deliveryShim
	started  chan struct{}
	stopped  chan struct{}
	inFlight func(*deliveryShim)
}

func (c *cycleDelivery) Start(ctx context.Context) error {
	close(c.started)
	<-ctx.Done()
	if c.inFlight != nil {
		c.inFlight(c.shim)
	}
	close(c.stopped)
	return nil
}

// Ready mirrors network.Synchronizer: caught up while running, and permanently
// not-ready (stateStopped) once its context has been cancelled.
func (c *cycleDelivery) Ready() error {
	select {
	case <-c.stopped:
		return errors.New("stopped")
	default:
		return nil
	}
}

// deliver pushes a block through this cycle's shim.
func (c *cycleDelivery) deliver(t *testing.T, num uint64) {
	t.Helper()
	require.NoError(t, c.shim.Handle(context.Background(), blocks.Block{Number: num}))
}

// deliveryFactory stands in for h.newDelivery across restarts: each cycle gets a
// fresh cycleDelivery wired to the shim that cycle built, and nextCycle hands the
// test the one belonging to the cycle now running.
type deliveryFactory struct {
	created chan *cycleDelivery
	// inFlight, if set, is installed on every cycle's delivery; see cycleDelivery.
	inFlight func(*deliveryShim)
}

func newDeliveryFactory() *deliveryFactory {
	return &deliveryFactory{created: make(chan *cycleDelivery, 8)}
}

func (f *deliveryFactory) build(shim *deliveryShim) (deliverySyncer, error) {
	c := &cycleDelivery{
		shim:     shim,
		started:  make(chan struct{}),
		stopped:  make(chan struct{}),
		inFlight: f.inFlight,
	}
	f.created <- c
	return c, nil
}

// nextCycle returns the next cycle's delivery once start has built and started it.
func (f *deliveryFactory) nextCycle(t *testing.T) *cycleDelivery {
	t.Helper()
	select {
	case c := <-f.created:
		<-c.started
		return c
	case <-time.After(time.Minute):
		t.Fatal("no delivery synchronizer was built for the next cycle")
		return nil
	}
}

// fakeNotifPeer is a controllable notification.AllTxPeer.
// Call push to send a batch to the streamer; the stream blocks until ctx is done.
// Call disconnect to make the current Stream call return errDisconnected.
type fakeNotifPeer struct {
	batches    chan notification.AllTxBatch
	disconnErr chan error // send once to make Stream return that error
}

var errDisconnected = errors.New("disconnected")

func newFakeNotifPeer() *fakeNotifPeer {
	return &fakeNotifPeer{
		batches:    make(chan notification.AllTxBatch, 16),
		disconnErr: make(chan error, 1),
	}
}

func (f *fakeNotifPeer) StreamAllTransactions(ctx context.Context, _ *notification.StreamAllRequest, p notification.AllTxProcessor) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-f.disconnErr:
			return err
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
			{
				ID: txID, Status: blocks.StatusCommitted,
				BlockNum: blockNum,
			},
		},
	}
}

// pushEVM sends an EVM batch (one that AllTxBatchDispatcher will forward) for
// the given block number.
func (f *fakeNotifPeer) pushEVM(t *testing.T, blockNum uint64) {
	t.Helper()
	f.batches <- evmBatch(t, blockNum)
}

// disconnect makes the current Stream call return errDisconnected.
func (f *fakeNotifPeer) disconnect() {
	f.disconnErr <- errDisconnected
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

// newHybrid builds a HybridSynchronizer wired to the given fakes, handing the
// fake delivery each cycle's shim the way New does with the real synchronizer.
// The fake only supports one Start, so this is for single-cycle tests; restart
// tests use newRestartHybrid.
func newHybrid(t *testing.T, delivery *fakeDelivery, peer notification.AllTxPeer, handlers ...blocks.BlockHandler) *HybridSynchronizer {
	t.Helper()
	h := &HybridSynchronizer{
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
		notifPeer: peer,
		notifReq:  &notification.StreamAllRequest{IncludeMetadata: true, IncludeReadWriteSets: true},
	}
	h.newDelivery = func(shim *deliveryShim) (deliverySyncer, error) {
		delivery.shim = shim
		return delivery, nil
	}
	// Tests that drive the shim directly, without start, still need one.
	delivery.shim = &deliveryShim{h: h}
	return h
}

// newRestartHybrid builds a HybridSynchronizer whose delivery synchronizer is
// rebuilt per cycle, as New does, so Start's restart loop can be exercised.
func newRestartHybrid(peer notification.AllTxPeer, f *deliveryFactory, handlers ...blocks.BlockHandler) *HybridSynchronizer {
	h := &HybridSynchronizer{
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
		notifPeer: peer,
		notifReq:  &notification.StreamAllRequest{IncludeMetadata: true, IncludeReadWriteSets: true},
	}
	h.newDelivery = f.build
	return h
}

// newGate builds a notifGate over h, as start does, recording whether the gate
// asked for delivery to be stopped.
func newGate(h *HybridSynchronizer) (*notifGate, *bool) {
	stopped := new(bool)
	return &notifGate{
		hybrid:       h,
		dispatcher:   NewAllTxBatchDispatcher(&hybridAdapter{h: h}),
		stopDelivery: func() { *stopped = true },
	}, stopped
}

// evmBatch builds a batch the AllTxBatchDispatcher will actually forward: it drops
// batches whose events carry no EVM proposal args. The SDK decodes the metadata
// at the network boundary, so events arrive with InputArgs already populated and there
// is no wire-format metadata to marshal here.
func evmBatch(t *testing.T, blockNum uint64) notification.AllTxBatch {
	t.Helper()
	return notification.AllTxBatch{
		BlockNumber: blockNum,
		Events: []notification.CommittedTxEvent{{
			ID:        "evm-tx",
			Status:    blocks.StatusCommitted,
			EventName: common.ProposalTypeEVMTx,
			InputArgs: [][]byte{{0xaa}},
			BlockNum:  blockNum,
		}},
	}
}

// blockingHandler signals when Handle is entered and stays there until release is
// closed, so a test can hold one path inside the handler chain.
type blockingHandler struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newBlockingHandler(release chan struct{}) *blockingHandler {
	return &blockingHandler{entered: make(chan struct{}), release: release}
}

func (b *blockingHandler) Handle(_ context.Context, _ blocks.Block) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

// blockNumbers extracts the block numbers a recordingHandler saw, in order.
func blockNumbers(bs []blocks.Block) []uint64 {
	out := make([]uint64, len(bs))
	for i, b := range bs {
		out[i] = b.Number
	}
	return out
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
		go func() { errCh <- h.start(ctx) }()

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
		go func() { _ = h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Before the switch: Ready mirrors the delivery synchronizer.
		assert.Error(t, h.Ready(), "should not be ready before delivery catches up")

		delivery.setReady()
		assert.NoError(t, h.Ready(), "should be ready once delivery is ready")

		// Delivery processes block 5, which installs the seed (claimed=5).
		delivery.deliver(t, 5)
		synctest.Wait()

		// Batch for block 6 is exactly one past the seed, so the gate takes it and
		// switches. The batch carries no EVM metadata so the recorder sees nothing,
		// but that is not what this test is about.
		peer.push(6, "tx1")
		synctest.Wait()

		// Delivery is cancelled by the switch, which nils h.delivery.
		<-delivery.stopped
		synctest.Wait()

		assert.NoError(t, h.Ready(), "should remain ready in notification phase")
	})
}

// TestGate_DiscardsBatchWhileDeliveryBehind verifies that a batch for a block
// delivery has not yet reached loses the claim check and is silently discarded,
// leaving delivery running.
func TestGate_DiscardsBatchWhileDeliveryBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, peer, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Delivery processes block 2, installing the seed (claimed=2).
		delivery.deliver(t, 2)
		synctest.Wait()

		// Notif sends block 10: claimed(2) != 9, so the batch is discarded.
		peer.batches <- evmBatch(t, 10)
		synctest.Wait()

		// Only block 2 (dispatched by delivery) must be in the recorder — block 10 must not.
		assert.Equal(t, []uint64{2}, blockNumbers(recorder.received()), "batch ahead of delivery must be discarded")
		assert.Equal(t, int64(2), h.claimed.Load(), "a discarded batch must not move the claim")
		select {
		case <-delivery.stopped:
			t.Fatal("delivery must keep running while notification is still gated")
		default:
		}
	})
}

// TestGate_SwitchesAndForwardsWhenCaughtUp verifies the handoff end to end: a
// batch for the block right after delivery's position wins the claim, delivery is
// cancelled, and the triggering batch is forwarded to the handler chain.
func TestGate_SwitchesAndForwardsWhenCaughtUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, peer, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Delivery processes block 5, installing the seed (claimed=5).
		delivery.deliver(t, 5)
		synctest.Wait()

		// Notification takes block 6 — exactly one past delivery's position.
		peer.batches <- evmBatch(t, 6)
		synctest.Wait()

		select {
		case <-delivery.stopped:
			// delivery was cancelled — switch happened
		case <-time.After(time.Second):
			t.Fatal("delivery was not cancelled after switch")
		}
		assert.Equal(t, []uint64{5, 6}, blockNumbers(recorder.received()),
			"delivery's seed block and the switching batch must both be dispatched")

		// Subsequent batches keep flowing without consulting the claim counter, so a
		// gap in batch numbers (blocks with no EVM txs) does not stall the stream.
		peer.batches <- evmBatch(t, 9)
		synctest.Wait()
		assert.Equal(t, []uint64{5, 6, 9}, blockNumbers(recorder.received()))
	})
}

// TestGate_SwitchesWhenDeliveryIsOneBlockBehind is the regression test for the
// bug this protocol replaces: under continuous traffic a batch for block N always
// arrives while delivery has finished N-1 but not N, because N was only just
// committed. The old gate required delivery to have reached N itself, so the
// switch never fired — silently, with no error and no log.
func TestGate_SwitchesWhenDeliveryIsOneBlockBehind(t *testing.T) {
	delivery := newFakeDelivery()
	recorder := &recordingHandler{}
	h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
	delivery.seedClaimAt(h, delivery.seedHeight(6)) // store holds up to block 5

	// Delivery processes 6 and 7; it has not seen 8 yet.
	delivery.deliver(t, 6)
	delivery.deliver(t, 7)

	gate, stopped := newGate(h)
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 8)))

	assert.True(t, *stopped, "winning the claim must stop delivery")
	assert.Equal(t, []uint64{6, 7, 8}, blockNumbers(recorder.received()),
		"delivery covers 6-7, notification takes over at 8, nothing duplicated or skipped")
}

// TestDelivery_DisablesItselfAfterLosingClaim verifies that once notification has
// taken a block, delivery drops that block and every block after it — not just the
// one it lost. Without the sticky flag delivery could win the claim for the next
// block and dispatch behind notification's back.
func TestDelivery_DisablesItselfAfterLosingClaim(t *testing.T) {
	delivery := newFakeDelivery()
	recorder := &recordingHandler{}
	h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
	delivery.seedClaimAt(h, delivery.seedHeight(6))

	gate, _ := newGate(h)
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 6)))
	require.Equal(t, []uint64{6}, blockNumbers(recorder.received()))

	// Delivery now offers 6 (which it lost) and then 7 (which it must not take).
	delivery.deliver(t, 6)
	delivery.deliver(t, 7)

	assert.Equal(t, []uint64{6}, blockNumbers(recorder.received()),
		"delivery must dispatch nothing once it has lost a claim")
	assert.Equal(t, int64(6), h.claimed.Load(), "a disabled delivery must not move the claim")
}

// TestGate_DropsBatchWhenDeliveryClaimsFirst verifies the other side of the race:
// when delivery has already claimed the batch's block, the gate drops the batch
// rather than dispatching it a second time, and takes over at the next one.
func TestGate_DropsBatchWhenDeliveryClaimsFirst(t *testing.T) {
	delivery := newFakeDelivery()
	recorder := &recordingHandler{}
	h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
	delivery.seedClaimAt(h, delivery.seedHeight(6))

	delivery.deliver(t, 6) // delivery claimed and dispatched 6

	gate, stopped := newGate(h)
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 6)))
	assert.False(t, *stopped, "losing the claim must not stop delivery")
	assert.Equal(t, []uint64{6}, blockNumbers(recorder.received()),
		"block 6 was delivery's; notification must not dispatch it again")

	// The next batch is the gate's to take.
	require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 7)))
	assert.True(t, *stopped)
	assert.Equal(t, []uint64{6, 7}, blockNumbers(recorder.received()))
}

// TestSeedClaim_RestartWithPopulatedStore guards the seeding rule. The delivery
// synchronizer resumes at store height, so the claim counter must already sit one
// below that block or delivery loses its very first compare-and-swap and disables
// itself for the whole run — which looks exactly like the bug being fixed. A fresh
// chain hides this, so both cases are covered here.
func TestSeedClaim_RestartWithPopulatedStore(t *testing.T) {
	t.Run("populated store", func(t *testing.T) {
		delivery := newFakeDelivery()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
		delivery.seedClaimAt(h, delivery.seedHeight(501)) // store holds up to block 500; delivery resumes at 501

		assert.Equal(t, int64(500), h.claimed.Load())
		assert.Equal(t, int64(500), h.dispatched.Load(),
			"blocks already in the store must count as dispatched, or the gate waits forever")

		delivery.deliver(t, 501)
		assert.Equal(t, []uint64{501}, blockNumbers(recorder.received()),
			"delivery's first block after a restart must win its claim")
	})

	t.Run("empty store", func(t *testing.T) {
		delivery := newFakeDelivery()
		recorder := &recordingHandler{}
		h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
		delivery.seedClaimAt(h, delivery.seedHeight(1)) // height 1 means empty: delivery resumes at block 0

		assert.Equal(t, int64(-1), h.claimed.Load())

		delivery.deliver(t, 0)
		assert.Equal(t, []uint64{0}, blockNumbers(recorder.received()),
			"delivery's first block on a fresh chain must win its claim")
	})
}

// TestDelivery_HandlesUnexpectedFirstBlock verifies the TestLocalX/in-process-
// test-network quirk: the peer may deliver block 1 as its first block even though
// the store is empty (expected block 0).  Because the shim seeds from whatever
// block the peer actually delivers, this is handled automatically — the shim
// installs claimed=0, then CAS 0→1 succeeds and the block is dispatched.
func TestDelivery_HandlesUnexpectedFirstBlock(t *testing.T) {
	delivery := newFakeDelivery()
	recorder := &recordingHandler{}
	h := newHybrid(t, delivery, newFakeNotifPeer(), recorder)
	// Start sets claimed=MinInt64; shim will seed from whatever block arrives first.
	h.claimed.Store(math.MinInt64)
	h.dispatched.Store(math.MinInt64)

	// Peer delivers block 1 instead of 0.
	delivery.deliver(t, 1)

	assert.Equal(t, []uint64{1}, blockNumbers(recorder.received()),
		"unexpected first block must be dispatched after shim installs seed")
	assert.Equal(t, int64(1), h.claimed.Load())
	assert.Equal(t, int64(1), h.dispatched.Load())

	// Subsequent blocks must work normally.
	delivery.deliver(t, 2)
	assert.Equal(t, []uint64{1, 2}, blockNumbers(recorder.received()))
}

// TestGate_WaitsForDeliveryToFinishInFlightBlock verifies the ordering guarantee:
// after winning the claim for N the gate must not touch the handler chain until
// delivery has finished N-1, so the two paths never run it concurrently.
func TestGate_WaitsForDeliveryToFinishInFlightBlock(t *testing.T) {
	delivery := newFakeDelivery()
	release := make(chan struct{})
	blocker := newBlockingHandler(release)
	recorder := &recordingHandler{}
	h := newHybrid(t, delivery, newFakeNotifPeer(), blocker, recorder)
	delivery.seedClaimAt(h, delivery.seedHeight(6))

	// Delivery starts block 6 and stalls inside the handler chain.
	deliveryReturned := make(chan struct{})
	go func() {
		defer close(deliveryReturned)
		delivery.deliver(t, 6)
	}()
	<-blocker.entered

	// The gate takes block 7 and must block until delivery publishes 6.
	gate, _ := newGate(h)
	gateReturned := make(chan struct{})
	go func() {
		defer close(gateReturned)
		require.NoError(t, gate.HandleBatch(context.Background(), evmBatch(t, 7)))
	}()

	select {
	case <-gateReturned:
		t.Fatal("gate dispatched while delivery was still inside the handler chain")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-deliveryReturned
	<-gateReturned

	assert.Equal(t, []uint64{6, 7}, blockNumbers(recorder.received()),
		"delivery's block must complete before notification's first dispatch")
}

// TestWaitForDelivery_PanicsIfDeliveryOvershoots verifies the guard on the claim
// protocol's core invariant: delivery can never finish a block the notification
// path owns, so dispatched running past the block the gate is waiting on means the
// protocol is broken. That must be loud, not waited out or silently accepted.
func TestWaitForDelivery_PanicsIfDeliveryOvershoots(t *testing.T) {
	delivery := newFakeDelivery()
	h := newHybrid(t, delivery, newFakeNotifPeer(), &recordingHandler{})
	delivery.seedClaimAt(h, delivery.seedHeight(6))

	// The gate owns block 7 and waits on 6, but delivery claims to have finished 7.
	h.dispatched.Store(7)
	gate, _ := newGate(h)

	assert.PanicsWithError(t,
		"hybridx: programming error: delivery dispatched block 7 while notification owns block 7",
		func() { _ = gate.waitForDelivery(context.Background(), 6) })
}

// TestStart_DeliveryErrorIsLogged verifies that a delivery error does not cause
// Start to exit; Start only returns when ctx is cancelled.
func TestStart_DeliveryErrorIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Replace fakeDelivery with one that returns an error from Start.
		errDelivery := &errDeliverySyncer{err: errors.New("boom"), done: make(chan struct{})}
		peer := newFakeNotifPeer()
		h := &HybridSynchronizer{
			notifPeer: peer,
			notifReq:  &notification.StreamAllRequest{},
		}
		h.newDelivery = func(*deliveryShim) (deliverySyncer, error) { return errDelivery, nil }

		ctx, cancel := context.WithCancel(t.Context())
		errCh := make(chan error, 1)
		go func() { errCh <- h.start(ctx) }()

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

func (e *errDeliverySyncer) Ready() error { return errors.New("not ready") }

// TestStart_NotificationErrorPreSwitch verifies that a notification stream error
// before the switch does not cause Start to exit — delivery still covers the
// chain, so the stream is silently retried.
func TestStart_NotificationErrorPreSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		streamErr := errors.New("transient network error")
		h := newHybrid(t, delivery, &errNotifPeer{err: streamErr})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Start must still be running: the stream error was pre-switch so it is
		// retried, not propagated.
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

// TestStart_NotificationErrorPostSwitch verifies that a notification stream error
// after the switch causes Start to return ErrNotificationLost (wrapping the
// underlying stream error).  At that point delivery is permanently stopped, so a
// reconnect would leave a gap; the caller must restart the whole synchronizer.
func TestStart_NotificationErrorPostSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		streamErr := errors.New("transient network error")
		recorder := &recordingHandler{}
		// errAfterSwitchNotifPeer: delivers one batch that triggers the switch,
		// then returns an error from that same Stream call.
		h := newHybrid(t, delivery, &errAfterSwitchNotifPeer{firstBatch: evmBatch(t, 6), err: streamErr}, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Seed delivery at block 5 so the gate's CAS for block 6 succeeds.
		delivery.deliver(t, 5)
		synctest.Wait()

		// Wait for the switch to complete (delivery is stopped).
		<-delivery.stopped
		synctest.Wait()

		// Now the stream error fires on the next Stream call (post-switch).
		// Start must return ErrNotificationLost wrapping the stream error.
		err := <-errCh
		require.ErrorIs(t, err, ErrNotificationLost)
		require.ErrorIs(t, err, streamErr)
	})
}

// errNotifPeer is a notification.AllTxPeer that returns an error from
// StreamAllTransactions immediately, simulating a stream that cannot connect.
// Used to test pre-switch reconnect behaviour.
type errNotifPeer struct {
	err error
}

func (e *errNotifPeer) StreamAllTransactions(_ context.Context, _ *notification.StreamAllRequest, _ notification.AllTxProcessor) error {
	return e.err
}

// errAfterSwitchNotifPeer delivers a single batch on its first (and only) Stream
// call, triggering the switch, then ends that same call with err.  This simulates
// a post-switch disconnection: by the time Stream returns, the batch has been
// fully processed and switched is set, so start takes the post-switch branch and
// returns ErrNotificationLost.  A nil err models a clean close by the peer.
type errAfterSwitchNotifPeer struct {
	firstBatch notification.AllTxBatch
	err        error
}

func (e *errAfterSwitchNotifPeer) StreamAllTransactions(ctx context.Context, _ *notification.StreamAllRequest, p notification.AllTxProcessor) error {
	// Deliver the batch that triggers the switch.  ProcessBatch blocks until the
	// gate's handler goroutine has finished — including setting switched — before
	// returning.
	if err := p.ProcessBatch(ctx, e.firstBatch); err != nil {
		return err
	}
	return e.err
}

// TestStart_NotificationCleanClosePostSwitch covers the case the error check alone
// would miss: the SDK maps a clean server-side close to a nil error, but
// post-switch that leaves exactly the same gap as a failure, so it must surface as
// ErrNotificationLost rather than quietly reopening the stream.
func TestStart_NotificationCleanClosePostSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		// err: nil — the peer closes the stream without an error of its own.
		h := newHybrid(t, delivery, &errAfterSwitchNotifPeer{firstBatch: evmBatch(t, 6), err: nil}, &recordingHandler{})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		// Seed delivery at block 5 so the gate's CAS for block 6 succeeds.
		delivery.deliver(t, 5)
		synctest.Wait()

		err := <-errCh
		require.ErrorIs(t, err, ErrNotificationLost)
		require.ErrorIs(t, err, errStreamClosed)
	})
}

// TestStart_NotificationCleanClosePreSwitch is the counterpart: before the switch
// delivery still covers the chain, so a clean close is just reopened and the cycle
// keeps running.
func TestStart_NotificationCleanClosePreSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		// errNotifPeer with a nil error: the stream returns immediately, cleanly.
		h := newHybrid(t, delivery, &errNotifPeer{err: nil})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.start(ctx) }()

		synctest.Wait()
		<-delivery.started

		select {
		case err := <-errCh:
			t.Fatalf("cycle ended early with %v", err)
		default:
		}

		cancel()
		synctest.Wait()
		require.NoError(t, <-errCh)
	})
}

// TestReady_BeforeFirstCycle verifies that a readiness probe arriving before the
// first cycle has built its delivery synchronizer reports not-ready rather than
// crashing.  Start runs on its own goroutine, so callers (app.Run →
// WaitUntilSynced) routinely get here first.
func TestReady_BeforeFirstCycle(t *testing.T) {
	h := newHybrid(t, newFakeDelivery(), nil)
	require.Error(t, h.Ready(), "not ready until a cycle has built delivery")
}

// TestReady_AfterSwitchWithStoppedDelivery guards the readiness answer in the
// notification phase.  The switch stops the delivery synchronizer for good, and a
// stopped synchronizer reports not-ready forever, so Ready must stop consulting it
// the moment the gate takes over — not wait for delivery to notice, which it never
// does because it gets no further blocks.
func TestReady_AfterSwitchWithStoppedDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delivery := newFakeDelivery()
		peer := newFakeNotifPeer()
		h := newHybrid(t, delivery, peer, &recordingHandler{})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.start(ctx) }()

		synctest.Wait()
		<-delivery.started
		delivery.setReady()
		require.NoError(t, h.Ready(), "ready once delivery has caught up")

		// Delivery dispatches block 5, installing the seed.
		delivery.deliver(t, 5)
		synctest.Wait()

		// Notification takes block 6 and switches; delivery is cancelled and never
		// sees another block.
		peer.pushEVM(t, 6)
		synctest.Wait()
		<-delivery.stopped
		synctest.Wait()

		require.Error(t, delivery.Ready(), "a stopped delivery synchronizer is never ready")
		require.NoError(t, h.Ready(), "must stay ready in the notification phase")
	})
}

// TestOnSwitch_CalledOnceAcrossRestarts verifies that OnSwitch stays a one-shot
// even though each restart cycle switches again.  Callers register a callback that
// closes a channel, so a second call would panic.
func TestOnSwitch_CalledOnceAcrossRestarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var switches atomic.Int32
		factory := newDeliveryFactory()
		cycle1Peer, cycle2Peer := newFakeNotifPeer(), newFakeNotifPeer()
		h := newRestartHybrid(
			&sequentialNotifPeer{peers: []notification.AllTxPeer{cycle1Peer, cycle2Peer}},
			factory, &recordingHandler{})
		h.OnSwitch(func() { switches.Add(1) })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		// Cycle 1: switch at block 6, then disconnect.
		synctest.Wait()
		c1 := factory.nextCycle(t)
		c1.deliver(t, 5)
		synctest.Wait()
		cycle1Peer.pushEVM(t, 6)
		synctest.Wait()
		<-c1.stopped
		cycle1Peer.disconnect()
		synctest.Wait()

		// Cycle 2: switch again at block 8.
		c2 := factory.nextCycle(t)
		c2.deliver(t, 7)
		synctest.Wait()
		cycle2Peer.pushEVM(t, 8)
		synctest.Wait()
		<-c2.stopped
		synctest.Wait()

		require.Equal(t, int32(1), switches.Load(),
			"OnSwitch must fire once per process, not once per cycle")
	})
}

// TestReady_DuringRestart verifies that a readiness probe landing between cycles
// reports not-ready.  The previous cycle switched, so a leftover post-switch answer
// would claim readiness at the one moment nothing is applying blocks: notification
// is gone and the delivery synchronizer it would name is stopped.
func TestReady_DuringRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		factory := newDeliveryFactory()
		cycle1Peer, cycle2Peer := newFakeNotifPeer(), newFakeNotifPeer()
		h := newRestartHybrid(
			&sequentialNotifPeer{peers: []notification.AllTxPeer{cycle1Peer, cycle2Peer}},
			factory, &recordingHandler{})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		// Cycle 1 switches at block 6, so Ready reports ready.
		synctest.Wait()
		c1 := factory.nextCycle(t)
		c1.deliver(t, 5)
		synctest.Wait()
		cycle1Peer.pushEVM(t, 6)
		synctest.Wait()
		<-c1.stopped
		require.NoError(t, h.Ready(), "ready while notification owns the chain")

		// The stream drops.  Start is now between cycles, waiting out the backoff.
		cycle1Peer.disconnect()
		synctest.Wait()
		require.Error(t, h.Ready(), "must not report ready between cycles")
	})
}

// TestStart_RestartBacksOff verifies that a peer which disconnects immediately
// after every switch cannot spin Start through cycles: each restart waits
// restartDelay first.
func TestStart_RestartBacksOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		factory := newDeliveryFactory()
		peer := newFakeNotifPeer()
		h := newRestartHybrid(peer, factory, &recordingHandler{})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = h.Start(ctx) }()

		// Cycle 1 switches at block 6, then the stream drops.
		synctest.Wait()
		c1 := factory.nextCycle(t)
		c1.deliver(t, 5)
		synctest.Wait()
		peer.pushEVM(t, 6)
		synctest.Wait()
		<-c1.stopped
		peer.disconnect()
		synctest.Wait()

		// The cycle is over; the next one must not have been built yet.
		select {
		case <-factory.created:
			t.Fatal("restart rebuilt delivery immediately, with no backoff")
		default:
		}

		// Once restartDelay has elapsed, the next cycle starts.
		time.Sleep(restartDelay)
		synctest.Wait()
		require.NotNil(t, factory.nextCycle(t), "cycle must restart after the backoff")
	})
}

// TestStart_RestartOnNotificationLost verifies that no blocks are missed across
// an ErrNotificationLost restart.  The sequence is:
//
//  1. Cycle 1: delivery processes blocks 5–7, notification takes over at block 8,
//     then the stream disconnects → start returns ErrNotificationLost.
//  2. Cycle 2: Start restarts; delivery resumes from block 9 (the store height
//     after block 8 was fully applied), notification takes over at block 10 and
//     stays there for 10–11.
//
// Every block from 5 to 11 must appear exactly once, in order, with no gaps.
func TestStart_RestartOnNotificationLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &recordingHandler{}
		factory := newDeliveryFactory()

		// cycle1Peer: first Stream call; push block 8 to trigger the switch,
		// then disconnect so the cycle ends in ErrNotificationLost.
		// cycle2Peer: second Stream call (after restart); blocks 10–11.
		cycle1Peer := newFakeNotifPeer()
		cycle2Peer := newFakeNotifPeer()
		h := newRestartHybrid(
			// sequentialNotifPeer routes the first Stream call to cycle1Peer and all
			// subsequent calls to cycle2Peer.
			&sequentialNotifPeer{peers: []notification.AllTxPeer{cycle1Peer, cycle2Peer}},
			factory, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		// ── Cycle 1 ──────────────────────────────────────────────────────────
		synctest.Wait()
		c1 := factory.nextCycle(t)

		// Delivery processes blocks 5, 6, 7.
		c1.deliver(t, 5)
		c1.deliver(t, 6)
		c1.deliver(t, 7)
		synctest.Wait()

		// Push block 8 via cycle1Peer; wins the CAS and triggers the switch.
		// Stream returns only after HandleBatch completes, so block 8 is fully
		// applied and the store height is 9 before the cycle gives up.
		cycle1Peer.pushEVM(t, 8)
		synctest.Wait()
		<-c1.stopped // delivery cancelled by the switch
		synctest.Wait()

		// Disconnect cycle1Peer; the cycle ends and Start restarts after the backoff.
		cycle1Peer.disconnect()
		synctest.Wait()
		time.Sleep(restartDelay)
		synctest.Wait()

		// ── Cycle 2 ──────────────────────────────────────────────────────────
		c2 := factory.nextCycle(t)

		// Real delivery would resume from store height = 9 (block 8 was fully
		// applied before the disconnect).  Simulate that here.
		c2.deliver(t, 9)
		synctest.Wait()

		// Push block 10 via cycle2Peer; triggers the switch for cycle 2.
		cycle2Peer.pushEVM(t, 10)
		synctest.Wait()
		<-c2.stopped // delivery cancelled by the switch
		synctest.Wait()

		// Notification continues with block 11.
		cycle2Peer.pushEVM(t, 11)
		synctest.Wait()

		cancel()
		synctest.Wait()
		require.NoError(t, <-errCh)

		// Every block from 5 to 11 must appear exactly once, in order, no gaps.
		got := blockNumbers(recorder.received())
		require.Equal(t, []uint64{5, 6, 7, 8, 9, 10, 11}, got)
	})
}

// TestStart_JoinsDeliveryBeforeRestart verifies that a cycle does not return while
// its delivery synchronizer is still inside the handler chain.  Delivery is
// cancelled by the switch, but its last ProcessBlock can still be running — the SDK
// does not join it either.  A cycle that returned anyway would let the next one
// reseed the claim counter underneath that block, and the straggler could then win
// a CAS and dispatch a block the new cycle is about to replay.
//
// The test holds a block in flight and asserts the next cycle cannot start, even
// after the restart backoff has elapsed.
func TestStart_JoinsDeliveryBeforeRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &recordingHandler{}
		factory := newDeliveryFactory()

		// Cycle 1's delivery is still finishing a block when it is cancelled, and
		// stays there until the test releases it.
		release := make(chan struct{})
		var once sync.Once
		factory.inFlight = func(*deliveryShim) {
			once.Do(func() { <-release })
		}

		cycle1Peer, cycle2Peer := newFakeNotifPeer(), newFakeNotifPeer()
		h := newRestartHybrid(
			&sequentialNotifPeer{peers: []notification.AllTxPeer{cycle1Peer, cycle2Peer}},
			factory, recorder)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errCh := make(chan error, 1)
		go func() { errCh <- h.Start(ctx) }()

		// Cycle 1: delivery does 7–8, notification takes 9, then the stream drops.
		synctest.Wait()
		c1 := factory.nextCycle(t)
		c1.deliver(t, 7)
		c1.deliver(t, 8)
		synctest.Wait()
		cycle1Peer.pushEVM(t, 9)
		synctest.Wait()
		cycle1Peer.disconnect()

		// Let the backoff elapse while cycle 1's block is still in flight.  The
		// restart must still be waiting on it.
		time.Sleep(2 * restartDelay)
		synctest.Wait()
		select {
		case <-factory.created:
			t.Fatal("next cycle started while the previous one still had a block in flight")
		default:
		}

		// Releasing the block lets cycle 1 finish and cycle 2 begin.
		close(release)
		<-c1.stopped
		synctest.Wait()
		c2 := factory.nextCycle(t)
		c2.deliver(t, 10) // delivery resumes at 10; block 9 was fully applied
		synctest.Wait()

		cancel()
		synctest.Wait()
		require.NoError(t, <-errCh)

		require.Equal(t, []uint64{7, 8, 9, 10}, blockNumbers(recorder.received()),
			"a block in flight at cancellation must not leak into the next cycle")
	})
}

// sequentialNotifPeer routes each successive Stream call to the next peer in
// the list; the last peer handles all remaining calls.
type sequentialNotifPeer struct {
	mu    sync.Mutex
	peers []notification.AllTxPeer
	idx   int
}

func (s *sequentialNotifPeer) StreamAllTransactions(ctx context.Context, req *notification.StreamAllRequest, p notification.AllTxProcessor) error {
	s.mu.Lock()
	peer := s.peers[s.idx]
	if s.idx < len(s.peers)-1 {
		s.idx++
	}
	s.mu.Unlock()
	return peer.StreamAllTransactions(ctx, req, p)
}
