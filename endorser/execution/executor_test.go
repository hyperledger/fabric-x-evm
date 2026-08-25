/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"
)

func TestNewExecutor_WrapsStateDBWhenDebugEnabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_debug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{
		ChainConfig: common.BuildChainConfig(4011),
		DebugLogs:   true,
	}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	ex, err := eng.newExecutor(nil, uint64(1_700_000_000))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	if _, ok := ex.state.(*StateDBLogger); !ok {
		t.Fatalf("expected *StateDBLogger, got %T", ex.state)
	}
}

// TestExecute_MaxTxGas verifies that MaxTxGas caps msg.GasLimit before execution.
// A tx declared with large gas but a tight MaxTxGas must fail; once MaxTxGas is
// raised to cover intrinsic cost the same tx must succeed.
func TestExecute_MaxTxGas(t *testing.T) {
	to := ethcommon.HexToAddress("0xdead")

	newExecutor := func(maxTxGas uint64) *Executor {
		backend, err := state.NewWriteDB(Channel, "file:exec_maxtxgas?mode=memory&cache=shared")
		if err != nil {
			t.Fatal(err)
		}
		kvs := &testVersionedDBSnapshotter{db: backend}
		cfg := EVMConfig{
			ChainConfig: common.BuildChainConfig(4011),
			MaxTxGas:    maxTxGas,
		}
		eng := NewEVMEngine(Namespace, kvs, cfg, false)
		ex, err := eng.newExecutor(nil, uint64(1_700_000_000))
		if err != nil {
			t.Fatal(err)
		}
		return ex
	}

	// msg with declared gas well above MaxTxGas
	msg := func() *gethcore.Message {
		return &gethcore.Message{
			To:       &to,
			GasLimit: 100_000,
			Value:    new(uint256.Int),
		}
	}

	// MaxTxGas below intrinsic gas (21000 for a simple transfer) → must fail
	ex := newExecutor(1_000)
	defer ex.Close()
	if _, _, err := ex.execute(msg()); err == nil {
		t.Fatal("expected error when MaxTxGas < intrinsic gas, got nil")
	}

	// MaxTxGas at exactly intrinsic gas → simple transfer must succeed
	ex2 := newExecutor(21_000)
	defer ex2.Close()
	if _, gas, err := ex2.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == intrinsic gas, got: %v", err)
	} else if gas == 0 {
		t.Fatal("expected non-zero usedGas on success")
	}

	// MaxTxGas = 0 (unlimited) → declared gas used as-is, must succeed
	ex3 := newExecutor(0)
	defer ex3.Close()
	if _, gas, err := ex3.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == 0 (unlimited), got: %v", err)
	} else if gas == 0 {
		t.Fatal("expected non-zero usedGas on success")
	}
}

func TestNewExecutor_BareStateDBWhenDebugDisabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_nodebug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{
		ChainConfig: common.BuildChainConfig(4011),
		DebugLogs:   false,
	}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	ex, err := eng.newExecutor(nil, uint64(1_700_000_000))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	if _, ok := ex.state.(*StateDB); !ok {
		t.Fatalf("expected *StateDB, got %T", ex.state)
	}
}

// TestResolveStateBlockRef maps big.Int heights onto NewSnapshot args.
func TestResolveStateBlockRef(t *testing.T) {
	if got := resolveStateBlockRef(nil); got != nil {
		t.Fatalf("nil -> %v, want nil (latest)", got)
	}
	if got := resolveStateBlockRef(big.NewInt(-5)); got != nil {
		t.Fatalf("negative -> %v, want nil (latest)", got)
	}
	got0 := resolveStateBlockRef(big.NewInt(0))
	if got0 == nil || *got0 != 0 {
		t.Fatalf("0 -> %v, want pointer to 0 (earliest)", got0)
	}
	got7 := resolveStateBlockRef(big.NewInt(7))
	if got7 == nil || *got7 != 7 {
		t.Fatalf("7 -> %v, want pointer to 7", got7)
	}
	if stateDBBlockNum(nil) != 0 {
		t.Fatalf("stateDBBlockNum(nil) want 0")
	}
	if stateDBBlockNum(got7) != 7 {
		t.Fatalf("stateDBBlockNum(7) want 7")
	}
}

// TestNewSnapshotAt_ZeroIsExplicitGenesis ensures earliest (block 0) is not remapped
// to the tip after the chain has advanced (issue #293).
func TestNewSnapshotAt_ZeroIsExplicitGenesis(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_zero_snapshot?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: common.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	_, reader, err := eng.newSnapshotAt(big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	got := reader.(*testVersionedDBReader).blockNumber
	if got != 0 {
		t.Errorf("newSnapshotAt(0) resolved to block %d, want 0", got)
	}
}

// TestNewSnapshotAt_NegativeBlockNumberResolvesToLatest guards against callers upstream
// handing this engine a negative *big.Int carrying an unresolved go-ethereum block-tag
// sentinel (e.g. "earliest" == -5 in the pinned go-ethereum version). blockNumber.Uint64()
// on a negative big.Int silently returns the absolute value instead of erroring, so
// "earliest" would resolve to block 5 instead of "latest" — returning real but wrong
// state instead of failing loudly.
func TestNewSnapshotAt_NegativeBlockNumberResolvesToLatest(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_negblock_snapshot?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: common.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	wantLatest, err := backend.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, reader, err := eng.newSnapshotAt(big.NewInt(-5))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	got := reader.(*testVersionedDBReader).blockNumber
	if got != wantLatest {
		t.Errorf("newSnapshotAt(-5) resolved to block %d, want latest (%d)", got, wantLatest)
	}
}

// TestNewExecutor_NegativeBlockNumberResolvesToLatest is the newExecutor counterpart of
// TestNewSnapshotAt_NegativeBlockNumberResolvesToLatest; it backs eth_call/eth_estimateGas
// rather than eth_getBalance/eth_getCode/eth_getStorageAt/eth_getTransactionCount.
func TestNewExecutor_NegativeBlockNumberResolvesToLatest(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_negblock_executor?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: common.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	wantLatest, err := backend.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ex, err := eng.newExecutor(big.NewInt(-5), uint64(1_700_000_000))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	got := ex.reader.(*testVersionedDBReader).blockNumber
	if got != wantLatest {
		t.Errorf("newExecutor(-5) resolved to block %d, want latest (%d)", got, wantLatest)
	}
}

// TestCall_ReportsPreRefundGasNotPostRefund verifies that Call/ApplyMessage
// return go-ethereum's pre-refund MaxUsedGas, not the post-refund UsedGas.
// EstimateGas seeds its search from this value; if it silently became the
// net figure again, every call that clears storage to zero would report less
// gas than it actually needs on resubmission, since EIP-3529's refund is
// only credited to the final bill and is never spendable mid-execution.
func TestCall_ReportsPreRefundGasNotPostRefund(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_refund?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}

	contract := newAddress()
	slot := ethcommon.HexToHash("0x00")
	// PUSH1 0x00; PUSH1 0x00; SSTORE; STOP -- clears slot 0 to zero.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x55, 0x00}

	// Prime the slot with a nonzero original value and commit it, so the
	// SSTORE-to-zero below actually earns an EIP-3529 clear refund: the
	// refund only applies when the slot's value *before this transaction*
	// was nonzero.
	setup := snapshotDB(t, backend, 0)
	setup.CreateAccount(contract)
	setup.SetCode(contract, code, tracing.CodeChangeContractCreation)
	setup.SetState(contract, slot, ethcommon.HexToHash("0x01"))
	err = backend.UpdateWorldState(t.Context(), blocks.Block{
		Number: 0,
		Transactions: []blocks.Transaction{{
			ID: "setup", Number: 0, Valid: true,
			NsRWS: []blocks.NsReadWriteSet{{Namespace: Namespace, RWS: setup.Result()}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: common.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	ex, err := eng.newExecutor(nil, uint64(1_700_000_000))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	_, gotGas, err := ex.Call(ethereum.CallMsg{To: &contract})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 21,000 intrinsic + 6 (two PUSH1s) + 5,000 (cold-access SSTORE clear:
	// 2,100 cold surcharge + 2,900 warm reset). If the post-refund figure
	// leaked back in, this would read 21,206 (26,006 minus the uncapped
	// 4,800 clear refund) instead.
	const want = 26_006
	if gotGas != want {
		t.Errorf("gas = %d, want %d (the pre-refund figure)", gotGas, want)
	}
}
