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
	"github.com/hyperledger/fabric-x-sdk/blocks"

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
type blockSequenceChecker struct {
	mu       sync.Mutex
	seen     []uint64
	problems []string
}

func (c *blockSequenceChecker) Handle(_ context.Context, b blocks.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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
	return nil
}

// report returns the problems found and the blocks seen, as a snapshot.
func (c *blockSequenceChecker) report() (problems []string, seen []uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.problems), slices.Clone(c.seen)
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

	// Sever the connection.  Post-switch this is unrecoverable in place, so the
	// synchronizer must tear the cycle down and start a new one.
	proxy.Cut()

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
