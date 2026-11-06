/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl/primer"
	"github.com/hyperledger/fabric-x-evm/integration/contracts"
	"github.com/hyperledger/fabric-x-evm/integration/tcpcut"
	"github.com/hyperledger/fabric-x-evm/synchronizer"
	"github.com/hyperledger/fabric-x-evm/synchronizer/hybridx"
)

// waitForSwitch returns a channel that closes when the HybridSynchronizer
// embedded in th switches from delivery to notification mode.
// Must be called before traffic is generated so the OnSwitch callback is
// registered before the switch can happen.
func waitForSwitch(t *testing.T, th *TestHarness) <-chan struct{} {
	t.Helper()
	h, ok := th.Synchronizer.(*hybridx.HybridSynchronizer)
	if !ok {
		t.Fatal("synchronizer is not a *hybridx.HybridSynchronizer")
	}
	switched := make(chan struct{})
	h.OnSwitch(func() { close(switched) })
	return switched
}

// testHybridSwitchesToNotification verifies, against a real fabric-x committer,
// that the hybrid synchronizer actually switches from delivery to notification
// mode under continuous traffic — the scenario that was silently broken before
// the lockfree CAS protocol was introduced. It also submits a transaction after
// the switch and confirms it is correctly reflected on-chain, exercising the
// notification path's block-hash placeholder end-to-end against the real
// committer rather than the fabrictest fake the unit tests use.
func testHybridSwitchesToNotification(t *testing.T) {
	th, err := newFileConfigHarness(t, TestLogger{T: t}, evmConfig(""), "", "fabx.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Stop() })
	switched := waitForSwitch(t, th)

	node := th.Gateways[0]
	ethClient, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// Continuous traffic: fire a deploy and several calls back-to-back with no
	// delay, so each new block's notification batch races the delivery
	// synchronizer's processing of that same block — the exact condition that
	// the CAS protocol is designed to handle.
	addr := deploySmartContract(t, node, ethClient)
	const calls = 20
	for range calls {
		callSmartContract(t, ethClient, addr, node, "increment")
	}

	select {
	case <-switched:
		t.Log("hybridx switched to notification mode")
	case <-time.After(10 * time.Second):
		t.Fatal("hybridx never switched to notification mode under continuous traffic")
	}

	// The synchronizer must keep working correctly after the switch.
	callSmartContract(t, ethClient, addr, node, "increment")
	querySmartContractExpect(t, ethClient, addr, th, big.NewInt(calls+1), "getCount")
}

// blockSequenceChecker is a block handler that records the block numbers it is
// given and flags any duplicate or gap.  Registered last in the chain, it sees
// exactly what the stores see, from both the delivery and the notification path.
//
// The hybrid protocol's whole purpose is that each block is dispatched once and
// in order even as ownership moves between the two paths, so a duplicate or a gap
// here is a protocol failure rather than a cosmetic complaint.
//
// It also records which path dispatched each block, read from the synchronizer at
// Handle time.  That is not something a block itself reveals — both paths are
// required to produce identical blocks — so it has to be sampled here, and it is
// what lets a test assert that delivery replayed a particular block rather than
// notification having received it live.  pathOf is nil when the test does not care.
type blockSequenceChecker struct {
	mu sync.Mutex
	// pathOf reports who is dispatching right now.  Called from Handle, where the
	// answer is exact: notification sets the flag before dispatching anything and
	// the two paths never dispatch concurrently.  Guarded by mu because it is
	// installed after the harness is built, by which point blocks may already be
	// arriving; it is only an atomic load, so calling it under the lock costs
	// nothing.
	pathOf   func() blockPath
	seen     []uint64
	paths    map[uint64]blockPath
	problems []string
}

// watchPaths installs the path source.  Called once the harness exists, since the
// checker has to be registered as a handler before there is a synchronizer to ask.
func (c *blockSequenceChecker) watchPaths(fn func() blockPath) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pathOf = fn
}

// blockPath names the hybridx path that dispatched a block.
type blockPath string

const (
	pathDelivery     blockPath = "delivery"
	pathNotification blockPath = "notification"
	pathUnknown      blockPath = "unknown" // checker built without a pathOf
)

func (c *blockSequenceChecker) Handle(_ context.Context, b blocks.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := pathUnknown
	if c.pathOf != nil {
		path = c.pathOf()
	}

	if len(c.seen) > 0 {
		switch prev := c.seen[len(c.seen)-1]; {
		case b.Number == prev:
			c.problems = append(c.problems, fmt.Sprintf("block %d dispatched twice in a row", b.Number))
		case b.Number < prev:
			c.problems = append(c.problems,
				fmt.Sprintf("block %d dispatched after block %d — out of order or replayed", b.Number, prev))
		case b.Number > prev+1:
			c.problems = append(c.problems,
				fmt.Sprintf("gap: block %d followed block %d, missing %d..%d", b.Number, prev, prev+1, b.Number-1))
		}
	}
	c.seen = append(c.seen, b.Number)
	if c.paths == nil {
		c.paths = map[uint64]blockPath{}
	}
	c.paths[b.Number] = path
	return nil
}

// report returns the problems found and the blocks seen, as a snapshot.
func (c *blockSequenceChecker) report() (problems []string, seen []uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.problems), slices.Clone(c.seen)
}

// pathFor returns the path that dispatched block n, and whether the block was seen
// at all.
func (c *blockSequenceChecker) pathFor(n uint64) (blockPath, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.paths[n]
	return p, ok
}

// waitForCount polls the Counter contract until getCount reads want, or fails the
// test after timeout.  Used where a value becomes visible only once the
// synchronizer has applied the block carrying it, so there is nothing local to
// wait on.
func waitForCount(t *testing.T, gw *core.Gateway, client *EthClient, addr ethcommon.Address, want int64, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		res := querySmartContract(t, gw, client, addr, "getCount")
		if len(res) > 0 {
			if got, ok := res[0].(*big.Int); ok && got.Cmp(big.NewInt(want)) == 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("getCount never reached %d within %s (last read: %v)", want, timeout, res)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// txWatcher reports when a specific Ethereum transaction commits, observed over a
// connection of the test's own straight to the real committer — not through the
// proxy the node under test goes through.  That independence is the whole point: it
// answers "has the chain moved on without this node", which the node itself cannot
// be asked, since all it can report is what it has managed to apply.
//
// It is a notification.AllTxHandler, so it sees the same committed-block feed
// hybridx's notification path does.  The feed has no replay, so watchForCommit must
// be called before the transaction is submitted.
type txWatcher struct {
	want      ethcommon.Hash
	committed chan struct{} // closed once want is seen as committed

	mu     sync.Mutex
	once   sync.Once
	status blocks.Status // the status want was seen with, for a clearer failure
	seen   bool
}

func (w *txWatcher) HandleBatch(_ context.Context, batch notification.AllTxBatch) error {
	for _, event := range batch.Events {
		raw, ok := common.EVMTx(event.Transaction)
		if !ok {
			continue // not an EVM transaction
		}
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(raw); err != nil {
			continue
		}
		if tx.Hash() != w.want {
			continue
		}

		// Record the status whatever it is, but only signal on a commit.  A rejected
		// transaction would leave the chain where it was, so releasing the hold on it
		// would mean releasing with no gap to replay — the status is kept so that shows
		// up as an explicit failure rather than a timeout.
		w.mu.Lock()
		w.status, w.seen = event.Status, true
		w.mu.Unlock()
		if event.Status == blocks.StatusCommitted {
			w.once.Do(func() { close(w.committed) })
		}
	}
	return nil
}

// outcome reports the status the watched transaction was seen with, if at all.
func (w *txWatcher) outcome() (blocks.Status, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, w.seen
}

// watchForCommit opens an independent notification stream to the real committer and
// returns a channel that closes once tx commits there.
//
// Call it before submitting tx: the stream is a real-time feed with no history, so a
// watcher started afterwards could miss the very block it is waiting for.  The stream
// is torn down when the test ends.
//
// The returned watcher is for reporting: its outcome distinguishes "never arrived"
// from "arrived and was rejected", which are different setup failures.
func watchForCommit(t *testing.T, cfg config.Config, tx *types.Transaction) (*txWatcher, <-chan struct{}) {
	t.Helper()

	peer, err := nfabx.NewPeer(cfg.Committer.ToPeerConf(), cfg.Network.Channel)
	if err != nil {
		t.Fatalf("connect to the committer directly: %v", err)
	}

	w := &txWatcher{want: tx.Hash(), committed: make(chan struct{})}
	streamer := notification.NewAllTxStreamer(peer, []notification.AllTxHandler{w}, sdk.NoOpLogger{}, 0)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := streamer.Stream(ctx, &notification.StreamAllRequest{
			FilterNamespaces: []string{cfg.Network.Namespace},
			IncludeMetadata:  true,
		}); err != nil && ctx.Err() == nil {
			t.Logf("independent committer stream ended: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = peer.Close()
	})

	return w, w.committed
}

// testHybridRestartAfterNotificationLoss is the end-to-end counterpart to the
// unit tests around ErrNotificationLost.  It drives a real fabric-x committer,
// waits for the switch to notification, then severs the committer connection at
// the TCP layer — which is the one thing the synchronizer cannot recover from in
// place, since delivery is stopped by then and the notification stream has no
// history to replay.
//
// The synchronizer must notice, restart, and let a fresh delivery synchronizer
// catch up from the store's height before notification takes over again.  A
// blockSequenceChecker registered in the handler chain asserts the property that
// actually matters across that restart: every block is applied exactly once, in
// order, with nothing duplicated at the re-switch and nothing missed during the
// outage.
//
// The outage is held open rather than merely started, and that is what gives the
// test its teeth.  The notification stream has no replay, so the only way a missed
// block can reach the node is a delivery replay — but only if the node was really
// disconnected while that block committed.  A bare Cut does not guarantee that: the
// client reconnects as soon as it tries, so a fast enough reconnect would see the
// block live, and the test would pass without the restart machinery having done
// anything.  Holding the proxy down until the committer has confirmed the block —
// watched on a stream of the test's own, which the hold does not touch — removes the
// race instead of betting against it.
//
// The test then checks the path, not just the outcome: it asserts the block carrying
// the during-outage transaction was dispatched by delivery.  Both paths produce
// identical blocks by design, so the counter reaching its expected value cannot
// distinguish them, and without this the assertions would hold just as well if
// notification had never lost anything.
//
// The cut is made with a transparent TCP proxy in front of the committer rather
// than by reaching into the gRPC connection: the proxy forwards bytes, so mTLS is
// still negotiated end-to-end between the real client and the real committer, and
// the test depends on nothing about the SDK's internals.
func testHybridRestartAfterNotificationLoss(t *testing.T) {
	cfg, err := config.Load("fabx.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// Put the proxy between the synchronizer and the committer.  Only the
	// committer connection is rerouted: the orderers keep their own direct
	// endpoints, so transactions can still be submitted while sync is severed.
	proxy := tcpcut.New(t, cfg.Committer.Endpoint.Address())
	checker := &blockSequenceChecker{}

	th, err := newFileConfigHarness(t, TestLogger{T: t}, evmConfig(""), "", "fabx.yaml",
		map[string]any{"Committer.Endpoint.Port": proxy.Port()},
		checker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Stop() })
	switched := waitForSwitch(t, th)

	// Now that there is a synchronizer, let the checker record which path dispatches
	// each block.  It had to be registered as a handler before the harness existed.
	hybrid, ok := th.Synchronizer.(*hybridx.HybridSynchronizer)
	if !ok {
		t.Fatalf("synchronizer is %T, want *hybridx.HybridSynchronizer", th.Synchronizer)
	}
	checker.watchPaths(func() blockPath {
		if hybrid.Switched() {
			return pathNotification
		}
		return pathDelivery
	})

	node := th.Gateways[0]
	ethClient, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}

	// Drive traffic until notification owns the chain.
	addr := deploySmartContract(t, node, ethClient)
	const before = 20
	for range before {
		callSmartContract(t, ethClient, addr, node, "increment")
	}

	select {
	case <-switched:
		t.Log("hybridx switched to notification mode; cutting the committer connection")
	case <-time.After(10 * time.Second):
		t.Fatal("hybridx never switched to notification mode under continuous traffic")
	}

	// Build the transaction to commit during the outage now, while endorsement and
	// the nonce still reflect live state.
	ec, err := NewNativeEthClient(node)
	if err != nil {
		t.Fatal(err)
	}
	duringTx, err := ethClient.TxForCall(t.Context(), ec, &addr, "increment")
	if err != nil {
		t.Fatal(err)
	}

	// Watch for that transaction on a stream of the test's own, straight to the
	// committer.  Started before the submission below, because the feed has no replay,
	// and before the Hold, because the Hold must not be able to affect it.
	watcher, duringCommitted := watchForCommit(t, cfg, duringTx)

	// Sever the connection and keep it severed.  Post-switch a cut is unrecoverable
	// in place, so the synchronizer must tear the cycle down and start a new one —
	// but a plain Cut would let it reconnect immediately, possibly in time to see the
	// transaction below through notification after all.  Holding the outage open
	// until the committer has moved on is what forces the missed block to come back
	// through delivery replay, which is the machinery under test.
	proxy.Hold()

	// Commit one transaction while sync is down, so the chain really does move on
	// without this node and the restarted delivery has ground to make up.
	//
	// Exactly one: submission reaches the orderers directly and is unaffected by the
	// cut, but endorsement reads the endorser's KVS, which the synchronizer feeds.
	// With sync severed that view is frozen, so a second transaction would endorse
	// against the same counter version as the first and lose the MVCC check at the
	// committer.  One is enough to create the gap this test is about.
	if err := ec.SendTransaction(t.Context(), duringTx); err != nil {
		t.Fatalf("submitting while sync is down: %v", err)
	}
	const during = 1

	// Hold the outage until that transaction has actually committed.  This is what
	// makes the gap real rather than likely: the node provably could not have seen
	// this block, because nothing could reach the committer through the proxy while it
	// committed, and the notification stream it would have arrived on has no replay.
	select {
	case <-duringCommitted:
		t.Log("the during-outage transaction committed while the node was disconnected")
	case <-time.After(30 * time.Second):
		// Either outcome means there is no gap for delivery to replay, so the test
		// would not exercise the restart — but they are different setup failures and
		// worth telling apart.
		if status, seen := watcher.outcome(); seen {
			t.Fatalf("the transaction submitted during the outage was rejected (%s), so the chain "+
				"never moved on without the node and there is no gap for delivery to replay", status)
		}
		t.Fatal("the transaction submitted during the outage never reached the committer, so there is " +
			"no gap for delivery to replay and the test would not exercise the restart")
	}

	// The gap now exists and cannot be filled from the notification stream, so from
	// here only a restart with delivery in front can close it.
	proxy.Release()

	// The restart has to re-establish sync on its own.  WaitUntilSynced polls
	// Ready, which reports not-ready between cycles and while the new delivery is
	// catching up, so this is exactly the "recovered" signal.
	if err := synchronizer.WaitUntilSynced(t.Context(), th.Synchronizer, 60*time.Second); err != nil {
		t.Fatalf("synchronizer did not recover after the connection was cut: %v", err)
	}
	t.Log("hybridx recovered after the cut")

	// Being caught up is not the same as having applied the block committed during
	// the outage — that block is the one the restart exists to recover — so wait for
	// the counter to reflect it before carrying on.
	waitForCount(t, node, ethClient, addr, before+during, 30*time.Second)

	// The block committed during the outage must have come back via delivery replay.
	// This is the assertion that ties the test to the restart machinery: the
	// notification stream has no replay, so had this block arrived by notification it
	// would mean the node was never really disconnected while it committed, and every
	// other assertion here would have held without the restart doing anything.
	receipt, err := ec.TransactionReceipt(t.Context(), duringTx.Hash())
	if err != nil {
		t.Fatalf("receipt for the transaction committed during the outage: %v", err)
	}
	gapBlock := receipt.BlockNumber.Uint64()
	switch path, seen := checker.pathFor(gapBlock); {
	case !seen:
		t.Errorf("block %d carried the during-outage transaction but was never dispatched to the handler chain", gapBlock)
	case path != pathDelivery:
		t.Errorf("block %d was dispatched by %s, want %s — the node was not disconnected while it committed, "+
			"so the delivery replay this test exists to exercise never ran",
			gapBlock, path, pathDelivery)
	default:
		t.Logf("block %d (committed during the outage) was replayed by delivery after the restart", gapBlock)
	}

	// And it must still be a working synchronizer, not merely a live one.
	const after = 5
	for range after {
		callSmartContract(t, ethClient, addr, node, "increment")
	}
	querySmartContractExpect(t, ethClient, addr, th, big.NewInt(before+during+after), "getCount")

	// The real assertion: no block was applied twice and none was skipped, across
	// the switch, the outage, and the re-switch.
	problems, seen := checker.report()
	if len(problems) > 0 {
		t.Errorf("block sequence broken across the restart (%d blocks seen, %d..%d):\n  %s",
			len(seen), seen[0], seen[len(seen)-1], strings.Join(problems, "\n  "))
	}
	t.Logf("applied %d blocks (%d..%d) with no duplicate or gap across the restart",
		len(seen), seen[0], seen[len(seen)-1])
}

// testHybridRevertAfterSwitch verifies that a transaction which reverts at the
// EVM level is correctly recorded with receipt.Status=0 when it arrives via the
// notification path (i.e. after the hybridx switch). This exercises the
// Metadata[1] event extraction fix: in fabric-x format the revert event is
// carried in Metadata[1] of the notification batch, not in a BlindWrite.
func testHybridRevertAfterSwitch(t *testing.T) {
	th, err := newFileConfigHarness(t, TestLogger{T: t}, evmConfig(""), "", "fabx.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Stop() })
	switched := waitForSwitch(t, th)

	node := th.Gateways[0]
	ethClient, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := primer.NewNativeEthClient(node)
	if err != nil {
		t.Fatal(err)
	}

	// Force the switch by submitting enough traffic.
	addr := deploySmartContract(t, node, ethClient)
	const calls = 20
	for range calls {
		callSmartContract(t, ethClient, addr, node, "increment")
	}

	select {
	case <-switched:
		t.Log("hybridx switched to notification mode; now testing revert on notification path")
	case <-time.After(10 * time.Second):
		t.Fatal("hybridx never switched to notification mode")
	}

	// decrement() reverts when the counter is already 0 — but we've already
	// incremented `calls` times so the first decrement succeeds.  Drive the
	// counter down to 0 first so the next decrement reverts at the EVM.
	for range calls {
		callSmartContract(t, ethClient, addr, node, "decrement")
	}
	// Counter is now 0; this decrement reverts.
	tx, err := ethClient.TxForCall(t.Context(), node, &addr, "decrement")
	if err != nil {
		t.Fatal(err)
	}
	if err := ec.SendTransaction(t.Context(), tx); err != nil {
		t.Fatalf("SendTransaction: %v", err)
	}
	waitForCommitT(t, ec, tx)

	receipt, err := ec.TransactionReceipt(t.Context(), tx.Hash())
	if err != nil {
		t.Fatalf("TransactionReceipt: %v", err)
	}
	if receipt.Status != types.ReceiptStatusFailed {
		t.Errorf("receipt.Status = %d on notification path, want %d (failed) — revert event not detected via Metadata[1]",
			receipt.Status, types.ReceiptStatusFailed)
	}
}
