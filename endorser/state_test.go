/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"
)

const (
	Channel   = "mychannel"
	Namespace = "ns"
	Db1       = "file:db1?mode=memory&cache=shared"
	Db2       = "file:db2?mode=memory&cache=shared"
)

func TestOpsReplayOnFreshDB(t *testing.T) {
	sender := newAddress()
	recipient := newAddress()
	contract := newAddress()

	originalState, err := state.NewWriteDB(Channel, Db1)
	if err != nil {
		t.Fatal(err)
	}

	sim, simDB := snapshotDB(t, originalState, 0)

	simDB.CreateAccount(sender)
	simDB.AddBalance(sender, uint256.NewInt(1_000), tracing.BalanceChangeTransfer)
	simDB.SubBalance(sender, uint256.NewInt(100), tracing.BalanceChangeTransfer)

	simDB.CreateAccount(recipient)
	simDB.AddBalance(recipient, uint256.NewInt(50), tracing.BalanceChangeTransfer)

	// Nonce ops
	simDB.SetNonce(sender, 1, tracing.NonceChangeGenesis)

	// Code ops
	simDB.CreateAccount(contract)
	simDB.CreateContract(contract)
	code := []byte{0x60, 0x00, 0x60, 0x01, 0x01} // simple bytecode
	simDB.SetCode(contract, code, tracing.CodeChangeContractCreation)

	// Storage ops
	slot := common.HexToHash("0x01")
	val := common.HexToHash("0xdeadbeef")
	simDB.SetState(contract, slot, val)

	// Self-destruct ops (TODO: actually destroy the contract)
	_ = simDB.SelfDestruct(contract)
	assertEqual(t, "SelfDestruct", simDB.HasSelfDestructed(contract), true, true)
	_, _ = simDB.SelfDestruct6780(contract)

	// replay on fresh database
	freshState, err := state.NewWriteDB(Channel, Db2)
	if err != nil {
		t.Fatal(err)
	}

	// commit changes
	b := blocks.Block{Transactions: []blocks.Transaction{
		{ID: "txid", Valid: true, NsRWS: []blocks.NsReadWriteSet{{Namespace: Namespace, RWS: sim.Result()}}},
	}}

	err = originalState.Handle(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}

	err = freshState.Handle(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}

	_, db1 := snapshotDB(t, originalState, 1)
	_, db2 := snapshotDB(t, freshState, 1)

	assertEqual(t, "Balance", db1.GetBalance(sender).String(), db2.GetBalance(sender).String(), uint256.NewInt(900).String())
	assertEqual(t, "Balance", db1.GetBalance(recipient).String(), db2.GetBalance(recipient).String(), uint256.NewInt(50).String())

	assertEqual(t, "Nonce", db1.GetNonce(sender), db2.GetNonce(sender), 1)
	assertEqual(t, "Nonce", db1.GetNonce(recipient), db2.GetNonce(recipient), 0)
	assertEqual(t, "Nonce", db1.GetNonce(contract), db2.GetNonce(contract), 0)

	assertEqual(t, "CodeSize", db1.GetCodeSize(contract), db2.GetCodeSize(contract), 5)
	assertEqual(t, "CodeHash", db1.GetCodeHash(contract), db2.GetCodeHash(contract), common.HexToHash("0x82a273ecd861ddb1956c611bea90e997c95527b61cf089a9762b4fdddd31e5d8"))

	assertEqual(t, "Storage", db1.GetState(contract, slot), db2.GetState(contract, slot), val)

}

func newAddress() common.Address {
	key, _ := crypto.GenerateKey()
	return crypto.PubkeyToAddress(key.PublicKey)
}

func assertEqual[T comparable](t *testing.T, label string, got1, got2, expected T) {
	t.Helper()
	if got1 != got2 {
		t.Errorf("%s mismatch: original=%v replay=%v", label, got1, got2)
	}
	if got1 != expected {
		t.Errorf("%s mismatch: got=%v should be=%v", label, got1, expected)
	}
}

func snapshotDB(t *testing.T, backend state.ReadStore, blockNum uint64) (*state.SimulationStore, *SnapshotDB) {
	sim, err := state.NewSimulationStore(t.Context(), backend, Namespace, blockNum, false)
	if err != nil {
		t.Fatal(err)
	}
	return sim, NewSnapshotDB(sim)
}

func TestAddLog(t *testing.T) {
	originalState, err := state.NewWriteDB(Channel, Db1)
	if err != nil {
		t.Fatal(err)
	}

	sim, simDB := snapshotDB(t, originalState, 0)

	contract := newAddress()
	topic1 := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	topic2 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	data := []byte{0xde, 0xad, 0xbe, 0xef}

	log := &ethtypes.Log{
		Address: contract,
		Topics:  []common.Hash{topic1, topic2},
		Data:    data,
	}

	simDB.AddLog(log)

	// Verify log was recorded in the simulation store
	logs := sim.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	if !bytes.Equal(logs[0].Address, contract.Bytes()) {
		t.Errorf("log address mismatch: got %x, want %x", logs[0].Address, contract.Bytes())
	}
	if len(logs[0].Topics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(logs[0].Topics))
	}
	if !bytes.Equal(logs[0].Topics[0], topic1.Bytes()) {
		t.Errorf("topic[0] mismatch: got %x, want %x", logs[0].Topics[0], topic1.Bytes())
	}
	if !bytes.Equal(logs[0].Topics[1], topic2.Bytes()) {
		t.Errorf("topic[1] mismatch: got %x, want %x", logs[0].Topics[1], topic2.Bytes())
	}
	if !bytes.Equal(logs[0].Data, data) {
		t.Errorf("log data mismatch: got %x, want %x", logs[0].Data, data)
	}

	// Verify the operation was recorded
	ops := simDB.Ops()
	var foundLogOp bool
	for _, op := range ops {
		if op.Type == OpAddLog {
			foundLogOp = true
			if op.Log == nil {
				t.Error("OpAddLog recorded but Log is nil")
			}
			if op.Log.Address != contract {
				t.Errorf("OpAddLog address mismatch: got %s, want %s", op.Log.Address, contract)
			}
			break
		}
	}
	if !foundLogOp {
		t.Error("OpAddLog operation was not recorded")
	}
}

func TestAddMultipleLogs(t *testing.T) {
	originalState, err := state.NewWriteDB(Channel, Db2)
	if err != nil {
		t.Fatal(err)
	}

	sim, simDB := snapshotDB(t, originalState, 0)

	contract1 := newAddress()
	contract2 := newAddress()

	log1 := &ethtypes.Log{
		Address: contract1,
		Topics:  []common.Hash{common.HexToHash("0x01")},
		Data:    []byte{0x01},
	}
	log2 := &ethtypes.Log{
		Address: contract2,
		Topics:  []common.Hash{common.HexToHash("0x02"), common.HexToHash("0x03")},
		Data:    []byte{0x02, 0x03},
	}

	simDB.AddLog(log1)
	simDB.AddLog(log2)

	logs := sim.Logs()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}

	if !bytes.Equal(logs[0].Address, contract1.Bytes()) {
		t.Errorf("first log address mismatch")
	}
	if !bytes.Equal(logs[1].Address, contract2.Bytes()) {
		t.Errorf("second log address mismatch")
	}
}
