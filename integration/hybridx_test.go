/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/hyperledger/fabric-x-evm/integration/contracts"
)

// switchWatchingLogger wraps TestLogger and additionally closes switched the
// first time it observes hybridx's "switching to notification" log line, so
// tests can wait on the switch without reaching into HybridSynchronizer's
// unexported state.
type switchWatchingLogger struct {
	TestLogger
	once     sync.Once
	switched chan struct{}
}

func newSwitchWatchingLogger(t *testing.T) *switchWatchingLogger {
	return &switchWatchingLogger{
		TestLogger: TestLogger{T: t},
		switched:   make(chan struct{}),
	}
}

func (l *switchWatchingLogger) Infof(format string, v ...any) {
	l.TestLogger.Infof(format, v...)
	if strings.HasPrefix(format, "hybridx: switching to notification") {
		l.once.Do(func() { close(l.switched) })
	}
}

// testHybridSwitchesToNotification verifies, against a real fabric-x committer,
// that the hybrid synchronizer actually switches from delivery to notification
// mode under continuous traffic — the scenario that was silently broken before
// the lockfree CAS protocol was introduced. It also submits a transaction after
// the switch and confirms it is correctly reflected on-chain, exercising the
// notification path's block-hash placeholder end-to-end against the real
// committer rather than the fabrictest fake the unit tests use.
func testHybridSwitchesToNotification(t *testing.T) {
	logs := newSwitchWatchingLogger(t)
	th, err := newFileConfigHarness(t, logs, evmConfig(""), "", "fabx.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Stop() })

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
	case <-logs.switched:
		t.Log("hybridx switched to notification mode")
	case <-time.After(10 * time.Second):
		t.Fatal("hybridx never switched to notification mode under continuous traffic")
	}

	// The synchronizer must keep working correctly after the switch.
	callSmartContract(t, ethClient, addr, node, "increment")
	querySmartContractExpect(t, ethClient, addr, th, big.NewInt(calls+1), "getCount")
}

// testHybridRevertAfterSwitch verifies that a transaction which reverts at the
// EVM level is correctly recorded with receipt.Status=0 when it arrives via the
// notification path (i.e. after the hybridx switch). This exercises the
// Metadata[1] event extraction fix: in fabric-x format the revert event is
// carried in Metadata[1] of the notification batch, not in a BlindWrite.
func testHybridRevertAfterSwitch(t *testing.T) {
	logs := newSwitchWatchingLogger(t)
	th, err := newFileConfigHarness(t, logs, evmConfig(""), "", "fabx.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Stop() })

	node := th.Gateways[0]
	ethClient, err := NewEthClient(contracts.CounterMetaData, th.ethChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := NewNativeEthClient(node)
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
	case <-logs.switched:
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
