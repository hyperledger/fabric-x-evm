/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
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
	if _, _, _, err := ex.execute(msg()); err == nil {
		t.Fatal("expected error when MaxTxGas < intrinsic gas, got nil")
	}

	// MaxTxGas at exactly intrinsic gas → simple transfer must succeed
	ex2 := newExecutor(21_000)
	defer ex2.Close()
	if _, gas, _, err := ex2.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == intrinsic gas, got: %v", err)
	} else if gas == 0 {
		t.Fatal("expected non-zero usedGas on success")
	}

	// MaxTxGas = 0 (unlimited) → declared gas used as-is, must succeed
	ex3 := newExecutor(0)
	defer ex3.Close()
	if _, gas, _, err := ex3.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == 0 (unlimited), got: %v", err)
	} else if gas == 0 {
		t.Fatal("expected non-zero usedGas on success")
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
	eng, contract := newClearingContractEngine(t, "exec_refund")

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

// A receipt reports gas after refunds, unlike Call: Execute must carry UsedGas.
func TestEVMEngineExecute_ReportsPostRefundGas(t *testing.T) {
	eng, contract := newClearingContractEngine(t, "exec_post_refund")

	res, err := eng.Execute(t.Context(), signTestTx(t, types.NewTransaction(0, contract, big.NewInt(0), 100_000, big.NewInt(1), nil)), 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}

	// 26,006 before refunds minus the 4,800 clear refund (under the 1/5 cap).
	const want = 21_206
	if got := mustExecutionMetadata(t, res).GetGasUsed(); got != want {
		t.Errorf("gas used = %d, want %d (the post-refund figure)", got, want)
	}
}

// newClearingContractEngine returns an engine whose ledger holds a contract that
// clears storage slot 0. The slot is primed nonzero and committed, so clearing
// it earns an EIP-3529 refund: the refund only applies when the slot's value
// *before this transaction* was nonzero.
func newClearingContractEngine(t *testing.T, dbName string) (*EVMEngine, ethcommon.Address) {
	t.Helper()
	backend := newTestBackend(t, dbName)

	contract := newAddress()
	// PUSH1 0x00; PUSH1 0x00; SSTORE; STOP -- clears slot 0 to zero.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x55, 0x00}

	setup := snapshotDB(t, backend, 0)
	setup.CreateAccount(contract)
	setup.SetCode(contract, code, tracing.CodeChangeContractCreation)
	setup.SetState(contract, ethcommon.HexToHash("0x00"), ethcommon.HexToHash("0x01"))
	err := backend.UpdateWorldState(t.Context(), blocks.Block{
		Number: 0,
		Transactions: []blocks.Transaction{{
			ID: "setup", Number: 0, Status: blocks.StatusCommitted,
			NsRWS: []blocks.NsReadWriteSet{{Namespace: Namespace, RWS: setup.Result()}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return newTestEngine(backend), contract
}

func newTestBackend(t *testing.T, dbName string) *state.VersionedDB {
	t.Helper()
	backend, err := state.NewWriteDB(Channel, "file:"+dbName+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func newTestEngine(backend *state.VersionedDB) *EVMEngine {
	return NewEVMEngine(Namespace, &testVersionedDBSnapshotter{db: backend}, EVMConfig{ChainConfig: common.BuildChainConfig(4011)}, false)
}

// signTestTx signs tx with a fresh key, so its nonce 0 matches an empty account.
func signTestTx(t *testing.T, tx *types.Transaction) *types.Transaction {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(4011)), key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// TestEVMEngineExecute_NonRevertFailureIsCommittedNotRejected verifies that a
// valid tx whose EVM execution faults without reverting (an invalid opcode,
// here) is endorsed as a committed outcome carrying an RWS and an exec-failure
// event, the same shape a revert gets - not returned as a bare Go error the
// way a pre-execution rejection (bad nonce, bad signature, ...) is.
func TestEVMEngineExecute_NonRevertFailureIsCommittedNotRejected(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_nonrevert_failure?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: common.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	// Contract creation whose init code is a single INVALID opcode (0xfe):
	// intrinsic gas is paid, then execution faults without reverting.
	tx := types.NewContractCreation(0, big.NewInt(0), 100_000, big.NewInt(1), []byte{0xfe})
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(4011)), key)
	if err != nil {
		t.Fatal(err)
	}

	res, err := eng.Execute(t.Context(), signed, uint64(1_700_000_000))
	if err != nil {
		t.Fatalf("expected a committed outcome, got error: %v", err)
	}
	if res.Status != common.StatusExecFailure {
		t.Errorf("Status = %d, want %d (StatusExecFailure)", res.Status, common.StatusExecFailure)
	}
	if len(res.RWS.Writes) == 0 {
		t.Error("expected RWS to record the sender's nonce increment, got no writes")
	}
	// The outcome travels in Payload; invalid opcode burns all the gas.
	md := mustExecutionMetadata(t, res)
	if md.GetStatus() != endorsementpb.ExecutionStatus_EXECUTION_STATUS_EXEC_FAILED {
		t.Errorf("payload status = %v, want exec failed", md.GetStatus())
	}
	if md.GetGasUsed() != 100_000 {
		t.Errorf("gas used = %d, want 100000", md.GetGasUsed())
	}
	if len(md.GetReason()) != 0 {
		t.Errorf("reason = %x, want empty", md.GetReason())
	}
}

// mustExecutionMetadata checks the EVM marker and decodes the outcome from Payload.
func mustExecutionMetadata(t *testing.T, res endorsement.ExecutionResult) *endorsementpb.ExecutionMetadata {
	t.Helper()
	if res.EventName != common.ProposalTypeEVMTx {
		t.Errorf("EventName = %q, want %q", res.EventName, common.ProposalTypeEVMTx)
	}
	md, err := common.UnmarshalExecutionMetadata(res.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if md.GetTimestamp() != 1_700_000_000 {
		t.Errorf("timestamp = %d, want 1700000000", md.GetTimestamp())
	}
	return md
}

// executeCreation runs a contract creation with the given init code on an empty ledger.
func executeCreation(t *testing.T, dbName string, initCode []byte) endorsement.ExecutionResult {
	t.Helper()
	eng := newTestEngine(newTestBackend(t, dbName))
	res, err := eng.Execute(t.Context(), signTestTx(t, types.NewContractCreation(0, big.NewInt(0), 100_000, big.NewInt(1), initCode)), 1_700_000_000)
	if err != nil {
		t.Fatalf("expected a committed outcome, got error: %v", err)
	}
	return res
}

// A revert is committed with its return data as the reason and the gas it used.
func TestEVMEngineExecute_RevertCarriesReasonInPayload(t *testing.T) {
	// PUSH4 0xdeadbeef; PUSH1 0; MSTORE; PUSH1 4; PUSH1 28; REVERT
	res := executeCreation(t, "exec_revert", []byte{0x63, 0xde, 0xad, 0xbe, 0xef, 0x60, 0x00, 0x52, 0x60, 0x04, 0x60, 0x1c, 0xfd})

	if res.Status != common.StatusEVMRevert {
		t.Errorf("Status = %d, want %d (StatusEVMRevert)", res.Status, common.StatusEVMRevert)
	}
	if len(res.Event) != 0 {
		t.Errorf("Event = %x, want none on a revert", res.Event)
	}
	md := mustExecutionMetadata(t, res)
	if md.GetStatus() != endorsementpb.ExecutionStatus_EXECUTION_STATUS_REVERTED {
		t.Errorf("payload status = %v, want reverted", md.GetStatus())
	}
	if !bytes.Equal(md.GetReason(), []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Errorf("reason = %x, want deadbeef", md.GetReason())
	}
	if md.GetGasUsed() == 0 || md.GetGasUsed() >= 100_000 {
		t.Errorf("gas used = %d, want between intrinsic and the limit", md.GetGasUsed())
	}
}

// A success carries its logs as the event and the outcome in Payload.
func TestEVMEngineExecute_SuccessCarriesLogsAndGas(t *testing.T) {
	// LOG0 of 0 bytes, then STOP
	res := executeCreation(t, "exec_success", []byte{0x60, 0x00, 0x60, 0x00, 0xa0, 0x00})

	if res.Status != common.StatusOK {
		t.Errorf("Status = %d, want %d", res.Status, common.StatusOK)
	}
	logs, err := common.UnmarshalLogs(res.Event)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %v (err %v), want one", logs, err)
	}
	md := mustExecutionMetadata(t, res)
	if md.GetStatus() != endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS {
		t.Errorf("payload status = %v, want success", md.GetStatus())
	}
	if md.GetGasUsed() == 0 || len(md.GetReason()) != 0 {
		t.Errorf("gas used = %d reason = %x, want non-zero gas and no reason", md.GetGasUsed(), md.GetReason())
	}
}

// Two endorsers executing the same tx must produce identical Payload bytes.
func TestEVMEngineExecute_PayloadIsDeterministic(t *testing.T) {
	eng := newTestEngine(newTestBackend(t, "exec_determinism"))
	// LOG0 of 0 bytes, then STOP
	signed := signTestTx(t, types.NewContractCreation(0, big.NewInt(0), 100_000, big.NewInt(1), []byte{0x60, 0x00, 0x60, 0x00, 0xa0, 0x00}))

	a, err := eng.Execute(t.Context(), signed, 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := eng.Execute(t.Context(), signed, 1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Payload, b.Payload) {
		t.Errorf("payloads differ: %x vs %x", a.Payload, b.Payload)
	}
}
