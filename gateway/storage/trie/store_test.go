/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package trie

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// newTestStore creates an in-memory Store for tests.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	ts, err := New("", types.EmptyRootHash)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// ---- test helpers ----

func makeBlock(num uint64, txs ...blocks.Transaction) blocks.Block {
	return blocks.Block{Number: num, Transactions: txs}
}

func makeTx(valid bool, writes ...blocks.KVWrite) blocks.Transaction {
	return blocks.Transaction{
		Valid: valid,
		NsRWS: []blocks.NsReadWriteSet{{
			Namespace: "evmcc",
			RWS:       blocks.ReadWriteSet{Writes: writes},
		}},
	}
}

func balWrite(hexAddr string, bal *big.Int) blocks.KVWrite {
	return blocks.KVWrite{Key: "acc:" + hexAddr + ":bal", Value: bal.Bytes()}
}

func nonceWrite(hexAddr string, nonce uint64) blocks.KVWrite {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, nonce)
	return blocks.KVWrite{Key: "acc:" + hexAddr + ":nonce", Value: b}
}

func codeWrite(hexAddr string, code []byte) blocks.KVWrite {
	return blocks.KVWrite{Key: "acc:" + hexAddr + ":code", Value: code}
}

func storageWrite(hexAddr, hexSlot, hexValue string) blocks.KVWrite {
	return blocks.KVWrite{Key: "str:" + hexAddr + ":" + hexSlot, Value: []byte(hexValue)}
}

func deleteWrite(key string) blocks.KVWrite {
	return blocks.KVWrite{Key: key, IsDelete: true}
}

// openStateAt is a test helper that opens a read-only StateDB at the given root.
func openStateAt(t *testing.T, ts *Store, root common.Hash) *state.StateDB {
	t.Helper()
	sdb, err := state.New(root, ts.db)
	if err != nil {
		t.Fatalf("open state at %s: %v", root, err)
	}
	return sdb
}

// ---- tests ----

func TestNew(t *testing.T) {
	ts := newTestStore(t)
	if ts.Root() != types.EmptyRootHash {
		t.Errorf("expected EmptyRootHash, got %s", ts.Root())
	}
}

func TestCommit_EmptyBlock(t *testing.T) {
	ts := newTestStore(t)
	root, err := ts.Commit(t.Context(), makeBlock(1))
	if err != nil {
		t.Fatal(err)
	}
	if root != types.EmptyRootHash {
		t.Errorf("empty block should not change root, got %s", root)
	}
}

func TestCommit_InvalidTxSkipped(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0xaaaa").Hex()
	block := makeBlock(1, makeTx(false, balWrite(addr, big.NewInt(999))))
	root, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}
	if root != types.EmptyRootHash {
		t.Errorf("invalid tx must not affect root, got %s", root)
	}
}

func TestCommit_SetBalance(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x1111")
	expected := big.NewInt(1_000_000)

	block := makeBlock(1, makeTx(true, balWrite(addr.Hex(), expected)))
	root, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}
	if root == types.EmptyRootHash {
		t.Error("expected non-empty root after balance write")
	}

	sdb := openStateAt(t, ts, root)
	got := sdb.GetBalance(addr).ToBig()
	if got.Cmp(expected) != 0 {
		t.Errorf("balance: want %s, got %s", expected, got)
	}
}

func TestCommit_ZeroBalanceValue(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x2222").Hex()
	// big.NewInt(0).Bytes() == []byte{} — the endorser's encoding of zero
	block := makeBlock(1, makeTx(true, balWrite(addr, big.NewInt(0))))
	_, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatalf("zero balance must not error: %v", err)
	}
}

func TestCommit_SetNonce(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x3333")

	block := makeBlock(1, makeTx(true, nonceWrite(addr.Hex(), 42)))
	root, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}

	sdb := openStateAt(t, ts, root)
	if got := sdb.GetNonce(addr); got != 42 {
		t.Errorf("nonce: want 42, got %d", got)
	}
}

func TestCommit_SetCode(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x4444")
	code := []byte{0x60, 0x80, 0x60, 0x40}

	block := makeBlock(1, makeTx(true, codeWrite(addr.Hex(), code)))
	root, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}

	sdb := openStateAt(t, ts, root)
	if got := sdb.GetCode(addr); string(got) != string(code) {
		t.Errorf("code: want %x, got %x", code, got)
	}
}

func TestCommit_StorageSlot(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x5555")
	slot := common.HexToHash("0x01")
	value := common.HexToHash("0xdeadbeef")
	code := []byte{0x60, 0x80} // contracts with storage always have code

	block := makeBlock(1, makeTx(true,
		codeWrite(addr.Hex(), code),
		storageWrite(addr.Hex(), slot.Hex(), value.Hex()),
	))
	root, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}

	sdb := openStateAt(t, ts, root)
	if got := sdb.GetState(addr, slot); got != value {
		t.Errorf("storage: want %s, got %s", value, got)
	}
}

func TestCommit_DeleteBalance(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x6666")

	// First set a balance.
	block1 := makeBlock(1, makeTx(true, balWrite(addr.Hex(), big.NewInt(500))))
	root1, err := ts.Commit(t.Context(), block1)
	if err != nil {
		t.Fatal(err)
	}
	sdb1 := openStateAt(t, ts, root1)
	if sdb1.GetBalance(addr).Sign() == 0 {
		t.Fatal("expected non-zero balance after block 1")
	}

	// Then delete it.
	block2 := makeBlock(2, makeTx(true, deleteWrite("acc:"+addr.Hex()+":bal")))
	root2, err := ts.Commit(t.Context(), block2)
	if err != nil {
		t.Fatal(err)
	}
	sdb2 := openStateAt(t, ts, root2)
	if got := sdb2.GetBalance(addr).Sign(); got != 0 {
		t.Errorf("expected zero balance after delete, got sign=%d", got)
	}
}

func TestCommit_DeleteStorage(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x7777")
	slot := common.HexToHash("0x02")
	code := []byte{0x60, 0x80}

	// Set storage (with code so the account survives deleteEmptyObjects), then delete the slot.
	block1 := makeBlock(1, makeTx(true,
		codeWrite(addr.Hex(), code),
		storageWrite(addr.Hex(), slot.Hex(), common.HexToHash("0xff").Hex()),
	))
	if _, err := ts.Commit(t.Context(), block1); err != nil {
		t.Fatal(err)
	}

	block2 := makeBlock(2, makeTx(true, deleteWrite("str:"+addr.Hex()+":"+slot.Hex())))
	root2, err := ts.Commit(t.Context(), block2)
	if err != nil {
		t.Fatal(err)
	}
	sdb := openStateAt(t, ts, root2)
	if got := sdb.GetState(addr, slot); got != (common.Hash{}) {
		t.Errorf("expected zero storage after delete, got %s", got)
	}
}

func TestCommit_Determinism(t *testing.T) {
	addr := common.HexToAddress("0x8888")
	block := makeBlock(1,
		makeTx(true,
			balWrite(addr.Hex(), big.NewInt(123)),
			nonceWrite(addr.Hex(), 7),
		),
	)

	ts1 := newTestStore(t)
	root1, err := ts1.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}

	ts2 := newTestStore(t)
	root2, err := ts2.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}

	if root1 != root2 {
		t.Errorf("non-deterministic: root1=%s root2=%s", root1, root2)
	}
}

func TestCommit_MultipleBlocks(t *testing.T) {
	ts := newTestStore(t)
	addr := common.HexToAddress("0x9999")

	root0 := ts.Root()

	block1 := makeBlock(1, makeTx(true, balWrite(addr.Hex(), big.NewInt(100))))
	root1, err := ts.Commit(t.Context(), block1)
	if err != nil {
		t.Fatal(err)
	}
	if root1 == root0 {
		t.Error("root must change after first block")
	}

	block2 := makeBlock(2, makeTx(true, nonceWrite(addr.Hex(), 1)))
	root2, err := ts.Commit(t.Context(), block2)
	if err != nil {
		t.Fatal(err)
	}
	if root2 == root1 {
		t.Error("root must change after second block")
	}
}

func TestCommit_UnknownKeyPrefix(t *testing.T) {
	ts := newTestStore(t)
	block := makeBlock(1, makeTx(true,
		blocks.KVWrite{Key: "_lifecycle:somekey", Value: []byte("anything")},
		blocks.KVWrite{Key: "lscc:chaincode~collection~key", Value: []byte("data")},
	))
	_, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatalf("unknown key prefix must not error: %v", err)
	}
}

func TestCommit_MalformedKey(t *testing.T) {
	ts := newTestStore(t)
	block := makeBlock(1, makeTx(true,
		blocks.KVWrite{Key: "acc:", Value: []byte("x")},       // missing addr and field
		blocks.KVWrite{Key: "acc:0x1234", Value: []byte("x")}, // missing field
		blocks.KVWrite{Key: "str:", Value: []byte("x")},       // missing addr and slot
		blocks.KVWrite{Key: "str:0x1234", Value: []byte("x")}, // missing slot
	))
	_, err := ts.Commit(t.Context(), block)
	if err != nil {
		t.Fatalf("malformed keys must not error: %v", err)
	}
}

func TestCommit_MultipleNamespaces(t *testing.T) {
	ts := newTestStore(t)
	addr1 := common.HexToAddress("0xaaaa")
	addr2 := common.HexToAddress("0xbbbb")

	tx := blocks.Transaction{
		Valid: true,
		NsRWS: []blocks.NsReadWriteSet{
			{Namespace: "evmcc", RWS: blocks.ReadWriteSet{
				Writes: []blocks.KVWrite{balWrite(addr1.Hex(), big.NewInt(111))},
			}},
			{Namespace: "evmcc2", RWS: blocks.ReadWriteSet{
				Writes: []blocks.KVWrite{balWrite(addr2.Hex(), big.NewInt(222))},
			}},
		},
	}

	root, err := ts.Commit(t.Context(), makeBlock(1, tx))
	if err != nil {
		t.Fatal(err)
	}

	sdb := openStateAt(t, ts, root)
	if got := sdb.GetBalance(addr1).ToBig(); got.Cmp(big.NewInt(111)) != 0 {
		t.Errorf("addr1 balance: want 111, got %s", got)
	}
	if got := sdb.GetBalance(addr2).ToBig(); got.Cmp(big.NewInt(222)) != 0 {
		t.Errorf("addr2 balance: want 222, got %s", got)
	}
}

func TestPebble_PersistenceRoundTrip(t *testing.T) {
	dbPath := t.TempDir() + "/trie"
	addr := common.HexToAddress("0xc0ffee")

	// --- first run: commit a block and close ---
	ts1, err := New(dbPath, types.EmptyRootHash)
	if err != nil {
		t.Fatal(err)
	}
	block := makeBlock(1, makeTx(true, balWrite(addr.Hex(), big.NewInt(42))))
	root, err := ts1.Commit(t.Context(), block)
	if err != nil {
		t.Fatal(err)
	}
	if root == types.EmptyRootHash {
		t.Fatal("expected non-empty root after commit")
	}
	ts1.Close()

	// --- second run: reopen with the persisted root ---
	ts2, err := New(dbPath, root)
	if err != nil {
		t.Fatalf("reopen with persisted root failed: %v", err)
	}
	defer ts2.Close()

	if ts2.Root() != root {
		t.Errorf("root after reopen: want %s, got %s", root, ts2.Root())
	}
	sdb := openStateAt(t, ts2, root)
	if got := sdb.GetBalance(addr).ToBig(); got.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("balance after reopen: want 42, got %s", got)
	}
}
