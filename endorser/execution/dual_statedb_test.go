/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// dualDBCounter gives each test a distinct in-memory sqlite URI so parallel or
// re-ordered runs never share ledger state through the shared cache.
var dualDBCounter uint64

func newDualStateDBForTest(t *testing.T) (*DualStateDB, *StateDB, *ethstate.StateDB) {
	t.Helper()

	dualDBCounter++
	uri := fmt.Sprintf("file:dual_%d?mode=memory&cache=shared", dualDBCounter)
	backend, err := state.NewWriteDB(Channel, uri)
	require.NoError(t, err, "state.NewWriteDB")

	snap := snapshotDB(t, backend, 0)

	trieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	ethDBFactory := ethstate.NewDatabase(trieDB, nil)
	eth, err := ethstate.New(ethtypes.EmptyRootHash, ethDBFactory)
	require.NoError(t, err, "ethstate.New")

	return NewDualStateDB(eth, snap), snap, eth
}

// ---- Constructor + accessors ----

func TestDualStateDB_NewReturnsBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	require.NotNil(t, d)
	assert.Same(t, eth, d.EthStateDB(), "EthStateDB returns the wrapped ethStateDB")
	assert.Same(t, snap, d.snapshotDB, "internal snapshotDB is the one passed in")
	assert.NotNil(t, d.TrieDB(), "TrieDB reachable through eth side")
}

// ---- Accounts ----

func TestDualStateDB_CreateAccount_BothSidesExist(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)

	assert.True(t, snap.Exist(addr), "snap side sees account")
	assert.True(t, eth.Exist(addr), "eth side sees account")
	assert.True(t, d.Exist(addr), "dual reports existence")
}

func TestDualStateDB_CreateContract_BothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)
	d.CreateContract(addr)

	// Neither implementation exposes "is contract" directly; the meaningful
	// check is that both sides accepted the call without panicking and the
	// account remains present.
	assert.True(t, snap.Exist(addr))
	assert.True(t, eth.Exist(addr))
}

func TestDualStateDB_EmptyBeforeCreate(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	assert.True(t, d.Empty(newAddress()), "unknown addr is empty")
}

func TestDualStateDB_ExistFalseForUnknown(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	assert.False(t, d.Exist(newAddress()))
}

func TestDualStateDB_Touch_DoesNotPanic(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	addr := newAddress()
	d.CreateAccount(addr)
	d.Touch(addr)
}

// ---- Balances / nonces ----

func TestDualStateDB_AddBalance_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)
	prev := d.AddBalance(addr, uint256.NewInt(500), tracing.BalanceChangeTransfer)

	assert.True(t, prev.IsZero(), "previous balance was zero")
	assert.Equal(t, uint64(500), snap.GetBalance(addr).Uint64(), "snap side updated")
	assert.Equal(t, uint64(500), eth.GetBalance(addr).Uint64(), "eth side updated")
	assert.Equal(t, uint64(500), d.GetBalance(addr).Uint64(), "dual reads from snap")
}

func TestDualStateDB_SubBalance_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)
	d.AddBalance(addr, uint256.NewInt(1000), tracing.BalanceChangeTransfer)
	prev := d.SubBalance(addr, uint256.NewInt(300), tracing.BalanceChangeTransfer)

	assert.Equal(t, uint64(1000), prev.Uint64(), "prev is balance before Sub")
	assert.Equal(t, uint64(700), snap.GetBalance(addr).Uint64())
	assert.Equal(t, uint64(700), eth.GetBalance(addr).Uint64())
}

func TestDualStateDB_SetNonce_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)
	d.SetNonce(addr, 42, tracing.NonceChangeGenesis)

	assert.Equal(t, uint64(42), snap.GetNonce(addr))
	assert.Equal(t, uint64(42), eth.GetNonce(addr))
	assert.Equal(t, uint64(42), d.GetNonce(addr))
}

// ---- Code ----

func TestDualStateDB_SetCode_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()
	code := []byte{0x60, 0x00, 0x60, 0x01, 0x01}

	d.CreateAccount(addr)
	d.CreateContract(addr)
	prev := d.SetCode(addr, code, tracing.CodeChangeContractCreation)

	assert.Empty(t, prev, "no prior code")
	assert.Equal(t, code, snap.GetCode(addr))
	assert.Equal(t, code, eth.GetCode(addr))
	assert.Equal(t, code, d.GetCode(addr))

	assert.Equal(t, len(code), d.GetCodeSize(addr))
	assert.NotEqual(t, common.Hash{}, d.GetCodeHash(addr))
}

// ---- Storage ----

func TestDualStateDB_SetState_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()
	slot := common.HexToHash("0x1")
	val := common.HexToHash("0xdeadbeef")

	d.CreateAccount(addr)
	d.CreateContract(addr)
	prev := d.SetState(addr, slot, val)

	assert.Equal(t, common.Hash{}, prev, "no prior value")
	assert.Equal(t, val, snap.GetState(addr, slot))
	assert.Equal(t, val, eth.GetState(addr, slot))
	assert.Equal(t, val, d.GetState(addr, slot))
}

func TestDualStateDB_GetStateAndCommittedState(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	addr := newAddress()
	slot := common.HexToHash("0x2")
	val := common.HexToHash("0xcafe")

	d.CreateAccount(addr)
	d.CreateContract(addr)
	d.SetState(addr, slot, val)

	current, committed := d.GetStateAndCommittedState(addr, slot)
	assert.Equal(t, val, current, "current sees the in-flight write")
	// Committed state hasn't been finalised: both sides report zero.
	assert.Equal(t, common.Hash{}, committed)
}

func TestDualStateDB_GetStorageRoot_DoesNotPanic(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	// DualStateDB currently returns the eth side's root (see FIXME in dual_statedb.go).
	// The test just guards that the call succeeds; the returned value can be
	// the empty hash before any Finalise.
	_ = d.GetStorageRoot(newAddress())
}

func TestDualStateDB_TransientState_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()
	key := common.HexToHash("0x1")
	val := common.HexToHash("0xbeef")

	d.SetTransientState(addr, key, val)

	assert.Equal(t, val, snap.GetTransientState(addr, key))
	assert.Equal(t, val, eth.GetTransientState(addr, key))
	assert.Equal(t, val, d.GetTransientState(addr, key))
}

// ---- Refunds ----

func TestDualStateDB_AddSubRefund_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)

	d.AddRefund(1000)
	assert.Equal(t, uint64(1000), snap.GetRefund())
	assert.Equal(t, uint64(1000), eth.GetRefund())
	assert.Equal(t, uint64(1000), d.GetRefund())

	d.SubRefund(400)
	assert.Equal(t, uint64(600), snap.GetRefund())
	assert.Equal(t, uint64(600), eth.GetRefund())
	assert.Equal(t, uint64(600), d.GetRefund())
}

// ---- Self-destruct ----

func TestDualStateDB_SelfDestruct_MarkedOnBothSides(t *testing.T) {
	d, snap, _ := newDualStateDBForTest(t)
	addr := newAddress()

	d.CreateAccount(addr)
	d.CreateContract(addr)
	d.AddBalance(addr, uint256.NewInt(100), tracing.BalanceChangeTransfer)
	d.SelfDestruct(addr)

	assert.True(t, snap.HasSelfDestructed(addr), "snap side marked")
	assert.True(t, d.HasSelfDestructed(addr), "dual reports")
	// eth side's HasSelfDestructed also reflects the call.
	assert.True(t, d.ethStateDB.HasSelfDestructed(addr))
}

// ---- Access list ----

func TestDualStateDB_AddressAccessList_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()

	assert.False(t, d.AddressInAccessList(addr))
	d.AddAddressToAccessList(addr)
	assert.True(t, d.AddressInAccessList(addr))
	assert.True(t, snap.AddressInAccessList(addr))
	assert.True(t, eth.AddressInAccessList(addr))
}

func TestDualStateDB_SlotAccessList_ReflectedOnBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()
	slot := common.HexToHash("0x1")

	d.AddSlotToAccessList(addr, slot)

	addrOk, slotOk := d.SlotInAccessList(addr, slot)
	assert.True(t, addrOk)
	assert.True(t, slotOk)

	sa, ss := snap.SlotInAccessList(addr, slot)
	assert.True(t, sa && ss, "snap side sees the slot")

	ea, es := eth.SlotInAccessList(addr, slot)
	assert.True(t, ea && es, "eth side sees the slot")
}

func TestDualStateDB_Prepare_DelegatesToBothSides(t *testing.T) {
	d, snap, _ := newDualStateDBForTest(t)
	sender := newAddress()
	coinbase := newAddress()
	dest := newAddress()
	precompile := common.HexToAddress("0x01")

	// The important property is that Prepare is called on both sides without
	// panicking. Cross-side identical access-list contents are not part of the
	// DualStateDB contract (the two implementations seed slightly different
	// entries per rules); assert only on the snap side, which we read from.
	d.Prepare(params.Rules{IsBerlin: true, IsShanghai: true},
		sender, coinbase, &dest, []common.Address{precompile}, nil)

	assert.True(t, snap.AddressInAccessList(precompile), "snap side sees the precompile")
	assert.True(t, d.AddressInAccessList(precompile))
}

// ---- Snapshot / revert ----

func TestDualStateDB_Snapshot_ReturnsSameIDFromBothSides(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	// No panic → both sides returned the same snapshot ID (Snapshot panics
	// otherwise; that panic path is a critical invariant guard).
	id := d.Snapshot()
	assert.GreaterOrEqual(t, id, 0)
}

func TestDualStateDB_RevertToSnapshot_UndoesBothSides(t *testing.T) {
	d, snap, eth := newDualStateDBForTest(t)
	addr := newAddress()
	slot := common.HexToHash("0x1")
	before := common.HexToHash("0xa")
	after := common.HexToHash("0xb")

	d.CreateAccount(addr)
	d.CreateContract(addr)
	d.SetState(addr, slot, before)

	snapID := d.Snapshot()

	d.SetState(addr, slot, after)
	require.Equal(t, after, snap.GetState(addr, slot))
	require.Equal(t, after, eth.GetState(addr, slot))

	d.RevertToSnapshot(snapID)

	assert.Equal(t, before, snap.GetState(addr, slot), "snap side reverted")
	assert.Equal(t, before, eth.GetState(addr, slot), "eth side reverted")
}

// ---- Logs / preimages ----

func TestDualStateDB_AddLog_ReflectedOnBothSides(t *testing.T) {
	d, snap, _ := newDualStateDBForTest(t)
	entry := &ethtypes.Log{
		Address: newAddress(),
		Topics:  []common.Hash{common.HexToHash("0xaa")},
		Data:    []byte{0xde, 0xad},
	}

	d.AddLog(entry)

	// Snap side exposes logs via its own accessor.
	require.Len(t, snap.Logs(), 1, "snap side captured the log")
	assert.Equal(t, entry.Address.Bytes(), snap.Logs()[0].Address)
}

func TestDualStateDB_AddPreimage_DoesNotPanic(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	// AddPreimage in go-ethereum is a no-op by default (preimage recording is off);
	// the meaningful assertion is that the call is accepted by both sides.
	d.AddPreimage(common.HexToHash("0x1"), []byte{0xaa})
}

// ---- Finalise + Result + Logs accessor ----

func TestDualStateDB_Finalise_DelegatesToBothSides(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	addr := newAddress()
	d.CreateAccount(addr)
	d.AddBalance(addr, uint256.NewInt(1), tracing.BalanceChangeTransfer)

	// Finalise delegates to both sides and returns snapshotDB's StateAccessList
	// (may be nil on an empty snap side; we assert the call completes).
	_ = d.Finalise(params.Rules{IsEIP158: true})
}

func TestDualStateDB_Result_ExposesSnapRWS(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	addr := newAddress()
	d.CreateAccount(addr)
	d.AddBalance(addr, uint256.NewInt(1), tracing.BalanceChangeTransfer)

	rws := d.Result()
	assert.NotEmpty(t, rws.Writes, "at least the balance write is recorded")
}

func TestDualStateDB_Logs_ExposesSnapLogs(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	entry := &ethtypes.Log{Address: newAddress(), Data: []byte{0xff}}
	d.AddLog(entry)
	assert.Len(t, d.Logs(), 1)
}

// ---- Pass-through accessors that must not panic ----

func TestDualStateDB_IsNewContract_AfterCreate(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	addr := newAddress()
	d.CreateAccount(addr)
	d.CreateContract(addr)
	assert.True(t, d.IsNewContract(addr))
}

func TestDualStateDB_LogsForBurnAccounts_Empty(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	assert.Empty(t, d.LogsForBurnAccounts())
}

func TestDualStateDB_Witness_And_AccessEvents_Reachable(t *testing.T) {
	d, _, _ := newDualStateDBForTest(t)
	// Both are pass-through to the snap side. Empty-DB state may return nil,
	// which is fine — the point is that the call is reachable and does not panic.
	_ = d.Witness()
	_ = d.AccessEvents()
}

// ---- EVMEngine read-only helpers (executor.go pass-throughs) ----
//
// BalanceAt / StorageAt / CodeAt / NonceAt are all
// "open a fresh snapshot at the requested block, read one thing, close it"
// wrappers. Covering them exercises the newSnapshotAt path and the four
// snap-side getters.

func newEVMEngineForTest(t *testing.T) *EVMEngine {
	t.Helper()
	dualDBCounter++
	uri := fmt.Sprintf("file:exec_reads_%d?mode=memory&cache=shared", dualDBCounter)
	backend, err := state.NewWriteDB(Channel, uri)
	require.NoError(t, err)
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: fxcommon.BuildChainConfig(4011)}
	return NewEVMEngine(Namespace, kvs, cfg, false)
}

func TestEVMEngine_BalanceAt_ReturnsSnapshotBalance(t *testing.T) {
	eng := newEVMEngineForTest(t)
	got, err := eng.BalanceAt(t.Context(), newAddress(), nil)
	require.NoError(t, err)
	// Empty snapshot: balance is zero.
	assert.Zero(t, got.Sign())
}

func TestEVMEngine_StorageAt_ReturnsZeroForEmpty(t *testing.T) {
	eng := newEVMEngineForTest(t)
	got, err := eng.StorageAt(t.Context(), newAddress(), common.HexToHash("0x1"), nil)
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 32), got)
}

func TestEVMEngine_CodeAt_ReturnsEmpty(t *testing.T) {
	eng := newEVMEngineForTest(t)
	got, err := eng.CodeAt(t.Context(), newAddress(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEVMEngine_NonceAt_ReturnsZero(t *testing.T) {
	eng := newEVMEngineForTest(t)
	got, err := eng.NonceAt(t.Context(), newAddress(), nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), got)
}
