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

	db := snapshotDB(t, originalState, 0)

	db.CreateAccount(sender)
	db.AddBalance(sender, uint256.NewInt(1_000), tracing.BalanceChangeTransfer)
	db.SubBalance(sender, uint256.NewInt(100), tracing.BalanceChangeTransfer)

	db.CreateAccount(recipient)
	db.AddBalance(recipient, uint256.NewInt(50), tracing.BalanceChangeTransfer)

	// Nonce ops
	db.SetNonce(sender, 1, tracing.NonceChangeGenesis)

	// Code ops
	db.CreateAccount(contract)
	db.CreateContract(contract)
	code := []byte{0x60, 0x00, 0x60, 0x01, 0x01} // simple bytecode
	db.SetCode(contract, code, tracing.CodeChangeContractCreation)

	// Storage ops
	slot := common.HexToHash("0x01")
	val := common.HexToHash("0xdeadbeef")
	db.SetState(contract, slot, val)

	// Self-destruct ops (TODO: actually destroy the contract)
	_ = db.SelfDestruct(contract)
	assertEqual(t, "SelfDestruct", db.HasSelfDestructed(contract), true, true)
	_, _ = db.SelfDestruct6780(contract)

	// replay on fresh database
	freshState, err := state.NewWriteDB(Channel, Db2)
	if err != nil {
		t.Fatal(err)
	}

	// commit changes
	b := blocks.Block{Transactions: []blocks.Transaction{
		{ID: "txid", Valid: true, NsRWS: []blocks.NsReadWriteSet{{Namespace: Namespace, RWS: db.Result()}}},
	}}

	err = originalState.Handle(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}

	err = freshState.Handle(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}

	db1 := snapshotDB(t, originalState, 1)
	db2 := snapshotDB(t, freshState, 1)

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

func snapshotDB(t *testing.T, backend ReadStore, blockNum uint64) *StateDB {
	stateDB, err := NewStateDB(t.Context(), backend, Namespace, blockNum, false)
	if err != nil {
		t.Fatal(err)
	}
	return stateDB
}

func TestAddLog(t *testing.T) {
	originalState, err := state.NewWriteDB(Channel, Db1)
	if err != nil {
		t.Fatal(err)
	}

	db := snapshotDB(t, originalState, 0)

	contract := newAddress()
	topic1 := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	topic2 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	data := []byte{0xde, 0xad, 0xbe, 0xef}

	log := &ethtypes.Log{
		Address: contract,
		Topics:  []common.Hash{topic1, topic2},
		Data:    data,
	}

	db.AddLog(log)

	// Verify log was recorded in the simulation store
	logs := db.Logs()
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
}

func TestAddMultipleLogs(t *testing.T) {
	originalState, err := state.NewWriteDB(Channel, Db2)
	if err != nil {
		t.Fatal(err)
	}

	db := snapshotDB(t, originalState, 0)

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

	db.AddLog(log1)
	db.AddLog(log2)

	logs := db.Logs()
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

// TestSnapshotRevertRWS tests that snapshot/revert properly affects the read-write set.
// When operations are reverted, they should not appear in the final RWS.
func TestSnapshotRevertRWS(t *testing.T) {
	// Setup: Create a DB with some initial state
	originalState, err := state.NewWriteDB(Channel, Db1)
	if err != nil {
		t.Fatal(err)
	}

	// Prime the DB with initial values so we have something to read
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	slot1 := common.HexToHash("0x01")
	slot2 := common.HexToHash("0x02")

	// Setup initial state
	setupDB := snapshotDB(t, originalState, 0)
	setupDB.CreateAccount(addr1)
	setupDB.SetState(addr1, slot1, common.HexToHash("0xAAAA"))
	setupDB.CreateAccount(addr2)
	setupDB.SetState(addr2, slot2, common.HexToHash("0xBBBB"))

	// Commit initial state
	setupRWS := setupDB.Result()
	err = originalState.UpdateWorldState(t.Context(), blocks.Block{
		Number: 0,
		Transactions: []blocks.Transaction{{
			ID: "setup", Number: 0, Valid: true,
			NsRWS: []blocks.NsReadWriteSet{{Namespace: Namespace, RWS: setupRWS}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test 1: Operations WITH revert - only pre-snapshot ops should appear in RWS
	t.Run("WithRevert", func(t *testing.T) {
		db := snapshotDB(t, originalState, 1)

		// Pre-snapshot operations
		val1 := db.GetState(addr1, slot1) // Read 1
		if val1 != common.HexToHash("0xAAAA") {
			t.Fatalf("expected 0xAAAA, got %v", val1)
		}
		db.SetState(addr1, slot1, common.HexToHash("0xCCCC")) // Write 1

		// Take snapshot
		snapID := db.Snapshot()

		// Post-snapshot operations (these will be reverted)
		val2 := db.GetState(addr2, slot2) // Read 2 (will be reverted)
		if val2 != common.HexToHash("0xBBBB") {
			t.Fatalf("expected 0xBBBB, got %v", val2)
		}
		db.SetState(addr2, slot2, common.HexToHash("0xDDDD")) // Write 2 (will be reverted)
		db.SetState(addr1, slot1, common.HexToHash("0xEEEE")) // Write 3 (will be reverted)

		// Revert to snapshot
		db.RevertToSnapshot(snapID)

		// Get RWS - should only contain pre-snapshot operations
		rws := db.Result()

		// Check reads: should only have addr1:slot1, NOT addr2:slot2
		if len(rws.Reads) != 1 {
			t.Fatalf("expected 1 read, got %d: %v", len(rws.Reads), rws.Reads)
		}
		expectedReadKey := storeKey(addr1, slot1)
		if rws.Reads[0].Key != expectedReadKey {
			t.Errorf("expected read key %s, got %s", expectedReadKey, rws.Reads[0].Key)
		}

		// Check writes: should only have addr1:slot1=0xCCCC, NOT addr2:slot2
		if len(rws.Writes) != 1 {
			t.Fatalf("expected 1 write, got %d: %v", len(rws.Writes), rws.Writes)
		}
		if rws.Writes[0].Key != expectedReadKey {
			t.Errorf("expected write key %s, got %s", expectedReadKey, rws.Writes[0].Key)
		}
		expectedValue := common.HexToHash("0xCCCC").Bytes()
		if !bytes.Equal(rws.Writes[0].Value, expectedValue) {
			t.Errorf("expected write value %x, got %x", expectedValue, rws.Writes[0].Value)
		}
	})

	// Test 2: Operations WITHOUT revert - all ops should appear in RWS
	t.Run("WithoutRevert", func(t *testing.T) {
		db := snapshotDB(t, originalState, 1)

		// Pre-snapshot operations
		val1 := db.GetState(addr1, slot1) // Read 1
		if val1 != common.HexToHash("0xAAAA") {
			t.Fatalf("expected 0xAAAA, got %v", val1)
		}
		db.SetState(addr1, slot1, common.HexToHash("0xCCCC")) // Write 1

		// Take snapshot (but don't revert)
		_ = db.Snapshot()

		// Post-snapshot operations (these will NOT be reverted)
		val2 := db.GetState(addr2, slot2) // Read 2
		if val2 != common.HexToHash("0xBBBB") {
			t.Fatalf("expected 0xBBBB, got %v", val2)
		}
		db.SetState(addr2, slot2, common.HexToHash("0xDDDD")) // Write 2
		db.SetState(addr1, slot1, common.HexToHash("0xEEEE")) // Write 3 (overwrites Write 1)

		// Get RWS - should contain ALL operations
		rws := db.Result()

		// Check reads: should have both addr1:slot1 AND addr2:slot2
		if len(rws.Reads) != 2 {
			t.Fatalf("expected 2 reads, got %d: %v", len(rws.Reads), rws.Reads)
		}

		// Check writes: should have both addresses
		// Note: addr1:slot1 appears once with final value 0xEEEE (Write 3 overwrote Write 1)
		if len(rws.Writes) != 2 {
			t.Fatalf("expected 2 writes, got %d: %v", len(rws.Writes), rws.Writes)
		}

		// Verify addr1:slot1 has final value 0xEEEE
		key1 := storeKey(addr1, slot1)
		key2 := storeKey(addr2, slot2)
		foundKey1, foundKey2 := false, false
		for _, w := range rws.Writes {
			if w.Key == key1 {
				foundKey1 = true
				expectedValue := common.HexToHash("0xEEEE").Bytes()
				if !bytes.Equal(w.Value, expectedValue) {
					t.Errorf("addr1:slot1 expected value %x, got %x", expectedValue, w.Value)
				}
			}
			if w.Key == key2 {
				foundKey2 = true
				expectedValue := common.HexToHash("0xDDDD").Bytes()
				if !bytes.Equal(w.Value, expectedValue) {
					t.Errorf("addr2:slot2 expected value %x, got %x", expectedValue, w.Value)
				}
			}
		}
		if !foundKey1 || !foundKey2 {
			t.Errorf("missing expected writes: foundKey1=%v, foundKey2=%v", foundKey1, foundKey2)
		}
	})
}
