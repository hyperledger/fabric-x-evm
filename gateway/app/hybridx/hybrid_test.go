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
	"testing"
	"testing/synctest"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	evmcommon "github.com/hyperledger/fabric-x-evm/common"
)

// ── fakes ────────────────────────────────────────────────────────────────────

// fakeDelivery is a controllable deliverySyncer. handler stands in for the
// deliveryShim the real synchronizer would call; tests use deliver to push blocks
// through it, which is what advances the claim counter.
type fakeDelivery struct {
	mu      sync.Mutex
	ready   bool
	handler blocks.BlockHandler
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
	require.NoError(t, f.handler.Handle(context.Background(), blocks.Block{Number: num}))
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

func (f *fakeDelivery) setReady() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = true
}

// seedHeight returns the first block number delivery would stream from the given
// store height — used by tests that drive the gate/shim directly (without Start)
// to call seedClaimAt with the right value.
func (f *fakeDelivery) seedHeight(height uint64) uint64 {
	if height > 1 {
		return height // delivery resumes at height; peer delivers block height first
	}
	return 0 // delivery resumes at 0; peer delivers block 0 first
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

// newHybrid builds a HybridSynchronizer wired to the given fakes, registering a
// deliveryShim with the fake delivery the way New does with the real one.
func newHybrid(t *testing.T, delivery *fakeDelivery, peer *fakeNotifPeer, handlers ...blocks.BlockHandler) *HybridSynchronizer {
	t.Helper()
	h := &HybridSynchronizer{
		logger:    sdk.NoOpLogger{},
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
		delivery:  delivery,
		notifPeer: peer,
		notifReq:  &notification.StreamAllRequest{IncludeMetadata: true, IncludeReadWriteSets: true},
	}
	delivery.handler = &deliveryShim{h: h}
	return h
}

// seedClaimAt seeds claimed and dispatched to first-1, simulating the state after
// delivery has already processed first-1 blocks.  Tests that drive the gate or
// shim directly (without calling Start) use this so the CAS invariants are
// satisfied from the start.  It also marks the deliveryShim as seeded so the
// MinInt64 guard in Handle does not misfire.
func (h *HybridSynchronizer) seedClaimAt(first uint64) {
	seed := int64(first) - 1
	h.claimed.Store(seed)
	h.dispatched.Store(seed)
	// Mark the shim as already seeded so its first-call MinInt64 guard is skipped.
	if shim, ok := h.delivery.(*fakeDelivery); ok {
		if ds, ok := shim.handler.(*deliveryShim); ok {
			ds.seeded = true
		}
	}
}

// newGate builds a notifGate over h, as Start does, recording whether the gate
// asked for delivery to be stopped.
func newGate(h *HybridSynchronizer) (*notifGate, *bool) {
	stopped := new(bool)
	return &notifGate{
		hybrid:       h,
		dispatcher:   NewAllTxBatchDispatcher(&hybridAdapter{h: h}),
		logger:       sdk.NoOpLogger{},
		stopDelivery: func() { *stopped = true },
	}, stopped
}

// evmBatch builds a batch the AllTxBatchDispatcher will actually forward: it
// drops batches whose events carry no EVM proposal metadata.
func evmBatch(t *testing.T, blockNum uint64) notification.AllTxBatch {
	t.Helper()
	input := &peer.ChaincodeInput{Args: [][]byte{{byte(evmcommon.ProposalTypeEVMTx)}, {0xaa}}}
	raw, err := proto.Marshal(input)
	require.NoError(t, err)
	return notification.AllTxBatch{
		BlockNumber: blockNum,
		Events: []notification.CommittedTxEvent{{
			TxID:     "evm-tx",
			BlockNum: blockNum,
			Status:   committerpb.Status_COMMITTED,
			Metadata: [][]byte{raw},
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
		go func() { _ = h.Start(ctx) }()

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
		go func() { _ = h.Start(ctx) }()

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
	h.seedClaimAt(delivery.seedHeight(6)) // store holds up to block 5

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
	h.seedClaimAt(delivery.seedHeight(6))

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
	h.seedClaimAt(delivery.seedHeight(6))

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
		h.seedClaimAt(delivery.seedHeight(501)) // store holds up to block 500; delivery resumes at 501

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
		h.seedClaimAt(delivery.seedHeight(1)) // height 1 means empty: delivery resumes at block 0

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
	h.seedClaimAt(delivery.seedHeight(6))

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
	h.seedClaimAt(delivery.seedHeight(6))

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
			logger:    sdk.NoOpLogger{},
			delivery:  errDelivery,
			notifPeer: peer,
			notifReq:  &notification.StreamAllRequest{},
		}

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

func (e *errDeliverySyncer) Ready() error { return errors.New("not ready") }
