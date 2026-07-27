/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// kvsBackend names a KVS implementation and a constructor for it. The parity
// suite runs the same black-box assertions against every backend so that
// PebbleKVS is verified to be a drop-in replacement for LightKVS at the KVS
// interface boundary (the white-box tests in lightkvs_test.go exercise
// LightKVS-internal structure and are not part of this contract).
type kvsBackend struct {
	name string
	// make builds a fresh, empty KVS. historySize is honored by LightKVS and
	// ignored by PebbleKVS (which keeps full history).
	make func(t *testing.T, historySize int) KVS
}

func kvsBackends() []kvsBackend {
	return []kvsBackend{
		{
			name: "LightKVS",
			make: func(t *testing.T, historySize int) KVS {
				return NewLightKVS(historySize)
			},
		},
		{
			name: "PebbleKVS",
			make: func(t *testing.T, historySize int) KVS {
				kvs, err := NewPebbleKVS(t.TempDir(), historySize)
				if err != nil {
					t.Fatalf("NewPebbleKVS failed: %v", err)
				}
				t.Cleanup(func() { _ = kvs.Close() })
				return kvs
			},
		},
	}
}

// forEachBackend runs fn as a subtest against every KVS backend.
func forEachBackend(t *testing.T, historySize int, fn func(t *testing.T, kvs KVS)) {
	t.Helper()
	for _, b := range kvsBackends() {
		b := b
		t.Run(b.name, func(t *testing.T) {
			fn(t, b.make(t, historySize))
		})
	}
}

// absent reports whether a read result represents an absent key. LightKVS
// returns nil for a deleted/missing key; PebbleKVS returns a tombstone record
// with IsDelete=true. Both mean "no live value", which is how the execution
// layer interprets them.
func absent(rec *blocks.WriteRecord) bool {
	return rec == nil || rec.IsDelete
}

// mkBlock builds a single-tx block writing the given ns/key/value writes.
func mkBlock(number uint64, txNum int64, txID string, valid bool, ns string, writes ...blocks.KVWrite) blocks.Block {
	return blocks.Block{
		Number: number,
		Transactions: []blocks.Transaction{{
			ID:     txID,
			Number: txNum,
			Valid:  valid,
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: ns,
				RWS:       blocks.ReadWriteSet{Writes: writes},
			}},
		}},
	}
}

// mkMultiTxBlock builds a block whose i-th transaction (Number i) contributes
// writes[i], all valid. It exercises multiple writes to the same key within one
// block — each in a distinct tx — which the per-key version logic must order.
func mkMultiTxBlock(number uint64, ns string, writes ...blocks.KVWrite) blocks.Block {
	txs := make([]blocks.Transaction, len(writes))
	for i, w := range writes {
		txs[i] = blocks.Transaction{
			ID:     fmt.Sprintf("tx%d", i),
			Number: int64(i),
			Valid:  true,
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: ns,
				RWS:       blocks.ReadWriteSet{Writes: []blocks.KVWrite{w}},
			}},
		}
	}
	return blocks.Block{Number: number, Transactions: txs}
}

func TestParityGetAndVersionIncrement(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()

		// Block 1: initial write.
		if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
			blocks.KVWrite{Key: "key1", Value: []byte("v1")})); err != nil {
			t.Fatalf("Handle block 1: %v", err)
		}

		rec, err := kvs.Get("ns1", "key1", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "v1" {
			t.Fatalf("expected v1, got %+v", rec)
		}
		if rec.Version != 0 {
			t.Errorf("expected version 0 on first write, got %d", rec.Version)
		}
		if rec.Namespace != "ns1" || rec.Key != "key1" {
			t.Errorf("expected ns1/key1 echoed back, got %s/%s", rec.Namespace, rec.Key)
		}

		// Blocks 2 and 3: overwrite same key, version must increment.
		if err := kvs.Handle(ctx, mkBlock(2, 0, "tx2", true, "ns1",
			blocks.KVWrite{Key: "key1", Value: []byte("v2")})); err != nil {
			t.Fatalf("Handle block 2: %v", err)
		}
		if err := kvs.Handle(ctx, mkBlock(3, 0, "tx3", true, "ns1",
			blocks.KVWrite{Key: "key1", Value: []byte("v3")})); err != nil {
			t.Fatalf("Handle block 3: %v", err)
		}

		rec, err = kvs.Get("ns1", "key1", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "v3" {
			t.Fatalf("expected v3, got %+v", rec)
		}
		if rec.Version != 2 {
			t.Errorf("expected version 2 after 3 writes, got %d", rec.Version)
		}

		// Missing key reads as absent.
		rec, err = kvs.Get("ns1", "nope", 0)
		if err != nil {
			t.Fatalf("Get missing: %v", err)
		}
		if !absent(rec) {
			t.Errorf("expected absent for missing key, got %+v", rec)
		}
	})
}

func TestParityNilValueRoundTrip(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
			blocks.KVWrite{Key: "key1", Value: nil})); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		rec, err := kvs.Get("ns1", "key1", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil {
			t.Fatal("expected record for nil-value write, got nil")
		}
		if rec.IsDelete {
			t.Fatal("nil value must not be a delete")
		}
		if rec.Value != nil {
			t.Errorf("expected nil value preserved, got %v", rec.Value)
		}
	})
}

func TestParityMultipleNamespacesAndColonKeys(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		// Include a key containing ':' to prove ns/key composition is
		// unambiguous (the namespace has no colon, so the split point is well
		// defined; PebbleKVS length-prefixes the composed key).
		block := blocks.Block{
			Number: 1,
			Transactions: []blocks.Transaction{{
				ID: "tx1", Number: 0, Valid: true,
				NsRWS: []blocks.NsReadWriteSet{
					{Namespace: "ns1", RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
						{Key: "a", Value: []byte("ns1-a")},
						{Key: "b:c:d", Value: []byte("ns1-bcd")},
					}}},
					{Namespace: "ns2", RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
						{Key: "a", Value: []byte("ns2-a")},
					}}},
				},
			}},
		}
		if err := kvs.Handle(ctx, block); err != nil {
			t.Fatalf("Handle: %v", err)
		}

		cases := map[[2]string]string{
			{"ns1", "a"}:     "ns1-a",
			{"ns1", "b:c:d"}: "ns1-bcd",
			{"ns2", "a"}:     "ns2-a",
		}
		for k, want := range cases {
			rec, err := kvs.Get(k[0], k[1], 0)
			if err != nil {
				t.Fatalf("Get %v: %v", k, err)
			}
			if rec == nil || string(rec.Value) != want {
				t.Errorf("Get %v: expected %q, got %+v", k, want, rec)
			}
		}
	})
}

func TestParityInvalidTransactionsSkipped(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		block := blocks.Block{
			Number: 1,
			Transactions: []blocks.Transaction{
				{ID: "bad", Number: 0, Valid: false, NsRWS: []blocks.NsReadWriteSet{
					{Namespace: "ns1", RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
						{Key: "k", Value: []byte("bad")},
					}}},
				}},
				{ID: "good", Number: 1, Valid: true, NsRWS: []blocks.NsReadWriteSet{
					{Namespace: "ns1", RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
						{Key: "k2", Value: []byte("good")},
					}}},
				}},
			},
		}
		if err := kvs.Handle(ctx, block); err != nil {
			t.Fatalf("Handle: %v", err)
		}

		rec, err := kvs.Get("ns1", "k", 0)
		if err != nil {
			t.Fatalf("Get invalid: %v", err)
		}
		if !absent(rec) {
			t.Errorf("write from invalid tx must not be visible, got %+v", rec)
		}

		rec, err = kvs.Get("ns1", "k2", 0)
		if err != nil {
			t.Fatalf("Get valid: %v", err)
		}
		if rec == nil || string(rec.Value) != "good" {
			t.Errorf("expected good, got %+v", rec)
		}
	})
}

func TestParityDeleteTombstoneContract(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()

		// Block 1: create.
		if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
			blocks.KVWrite{Key: "key1", Value: []byte("v1")})); err != nil {
			t.Fatalf("Handle create: %v", err)
		}
		// Block 2: delete.
		if err := kvs.Handle(ctx, mkBlock(2, 0, "tx2", true, "ns1",
			blocks.KVWrite{Key: "key1", IsDelete: true})); err != nil {
			t.Fatalf("Handle delete: %v", err)
		}

		// As of the latest block the key is effectively absent for both
		// backends (LightKVS: nil; PebbleKVS: IsDelete tombstone).
		rec, err := kvs.Get("ns1", "key1", 0)
		if err != nil {
			t.Fatalf("Get after delete: %v", err)
		}
		if !absent(rec) {
			t.Errorf("expected key absent after delete, got %+v", rec)
		}
	})
}

func TestParityTimeTravelReads(t *testing.T) {
	// historySize large enough that LightKVS retains blocks 1..3 in its ring.
	forEachBackend(t, 128, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		for i := uint64(1); i <= 3; i++ {
			val := []byte{byte('0' + i)}
			if err := kvs.Handle(ctx, mkBlock(i, 0, "tx", true, "ns1",
				blocks.KVWrite{Key: "k", Value: val})); err != nil {
				t.Fatalf("Handle block %d: %v", i, err)
			}
		}

		// Reading as of each block returns that block's value.
		for i := uint64(1); i <= 3; i++ {
			rec, err := kvs.Get("ns1", "k", i)
			if err != nil {
				t.Fatalf("Get as of block %d: %v", i, err)
			}
			want := string([]byte{byte('0' + i)})
			if rec == nil || string(rec.Value) != want {
				t.Errorf("as of block %d: expected %q, got %+v", i, want, rec)
			}
		}

		// A snapshot pins its block: writes after it are invisible.
		snap, err := kvs.NewSnapshot(2)
		if err != nil {
			t.Fatalf("NewSnapshot(2): %v", err)
		}
		defer snap.Close()
		rec, err := snap.Get("ns1", "k")
		if err != nil {
			t.Fatalf("snapshot Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "2" {
			t.Errorf("snapshot at block 2 expected \"2\", got %+v", rec)
		}
	})
}

func TestParityBlockNumber(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		if n, err := kvs.BlockNumber(ctx); err != nil || n != 0 {
			t.Fatalf("initial block number: got %d, err %v", n, err)
		}
		if err := kvs.Handle(ctx, mkBlock(5, 0, "tx", true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte("v")})); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		if n, err := kvs.BlockNumber(ctx); err != nil || n != 5 {
			t.Errorf("after block 5: got %d, err %v", n, err)
		}
	})
}

// TestPebblePersistenceAcrossReopen verifies the persistence win that motivated
// #218: after closing and reopening the store, committed state and the block
// checkpoint survive.
func TestPebblePersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	kvs, err := NewPebbleKVS(dir, 8)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := kvs.Handle(ctx, mkBlock(7, 0, "tx1", true, "ns1",
		blocks.KVWrite{Key: "key1", Value: []byte("persisted")})); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if err := kvs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen the same directory.
	kvs2, err := NewPebbleKVS(dir, 8)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer kvs2.Close()

	n, err := kvs2.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	if n != 7 {
		t.Errorf("expected block 7 to survive restart, got %d", n)
	}

	rec, err := kvs2.Get("ns1", "key1", 0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil || string(rec.Value) != "persisted" {
		t.Errorf("expected persisted value after restart, got %+v", rec)
	}
}

// TestParityMultiWritePerBlockValue asserts both backends agree that the
// highest-tx write to a key within a block wins the read. They intentionally
// differ only on the resulting version number (see
// TestPebbleVersionSemanticsMatchVersionedDB), not on the value.
func TestParityMultiWritePerBlockValue(t *testing.T) {
	forEachBackend(t, 8, func(t *testing.T, kvs KVS) {
		ctx := context.Background()
		if err := kvs.Handle(ctx, mkMultiTxBlock(1, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte("lo")},
			blocks.KVWrite{Key: "k", Value: []byte("hi")})); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		rec, err := kvs.Get("ns1", "k", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "hi" {
			t.Errorf("expected highest-tx value \"hi\", got %+v", rec)
		}
	})
}

// TestPebbleVersionSemanticsMatchVersionedDB pins the two intentional version
// divergences from LightKVS (see the PebbleKVS doc comment): PebbleKVS mirrors
// the sqlite VersionedDB's MAX(version)+1 rule — the store the fabric-x MVCC
// read-set is validated against — rather than LightKVS's per-block-shared,
// reset-on-delete numbering.
func TestPebbleVersionSemanticsMatchVersionedDB(t *testing.T) {
	ctx := context.Background()

	t.Run("consecutive versions for multiple writes to one key in a block", func(t *testing.T) {
		kvs, err := NewPebbleKVS(t.TempDir(), 8)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer kvs.Close()

		// One block, two txs both writing key "k": tx0 → version 0, tx1 →
		// version 1 (consecutive, VersionedDB-style). The highest-tx write wins.
		if err := kvs.Handle(ctx, mkMultiTxBlock(1, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte("from-tx0")},
			blocks.KVWrite{Key: "k", Value: []byte("from-tx1")})); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		rec, err := kvs.Get("ns1", "k", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "from-tx1" {
			t.Fatalf("expected highest-tx value from-tx1, got %+v", rec)
		}
		if rec.Version != 1 {
			t.Errorf("expected version 1 (tx0→0, tx1→1), got %d", rec.Version)
		}
	})

	t.Run("version is monotonic across a tombstone", func(t *testing.T) {
		kvs, err := NewPebbleKVS(t.TempDir(), 8)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer kvs.Close()

		// write (v0) @1, delete (v1 tombstone) @2, rewrite (v2) @3: the per-key
		// counter does not reset after the delete, unlike LightKVS (which would
		// restart the rewrite at version 0).
		if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte("v0")})); err != nil {
			t.Fatalf("Handle write: %v", err)
		}
		if err := kvs.Handle(ctx, mkBlock(2, 0, "tx2", true, "ns1",
			blocks.KVWrite{Key: "k", IsDelete: true})); err != nil {
			t.Fatalf("Handle delete: %v", err)
		}
		if err := kvs.Handle(ctx, mkBlock(3, 0, "tx3", true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte("again")})); err != nil {
			t.Fatalf("Handle rewrite: %v", err)
		}
		rec, err := kvs.Get("ns1", "k", 0)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec == nil || string(rec.Value) != "again" {
			t.Fatalf("expected latest value \"again\", got %+v", rec)
		}
		if rec.Version != 2 {
			t.Errorf("expected version 2 (monotonic across tombstone), got %d", rec.Version)
		}
	})
}

// TestPebbleHandleTxMultiBlock exercises the HandleTx notification path with
// writes spanning two blocks delivered out of block order, covering PebbleKVS's
// group-by-block + ascending-commit logic (LightKVS instead collapses a batch
// into one snapshot, so this is a PebbleKVS-specific contract).
func TestPebbleHandleTxMultiBlock(t *testing.T) {
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	mkNotif := func(block, tx uint64, id, val string) common.TxNotification {
		return common.TxNotification{
			BlockNum: block, TxNum: tx, FabricTxID: id,
			Status: committerpb.Status_COMMITTED,
			NsRWS: []blocks.NsReadWriteSet{{Namespace: "ns1", RWS: blocks.ReadWriteSet{
				Writes: []blocks.KVWrite{{Key: "k", Value: []byte(val)}},
			}}},
		}
	}
	// Deliver block 2 before block 1 to prove the store commits in ascending
	// block order (the checkpoint must advance monotonically to 2).
	if err := kvs.HandleTx(ctx, []common.TxNotification{
		mkNotif(2, 0, "b2", "block2"),
		mkNotif(1, 0, "b1", "block1"),
	}); err != nil {
		t.Fatalf("HandleTx: %v", err)
	}

	if n, err := kvs.BlockNumber(ctx); err != nil || n != 2 {
		t.Errorf("expected head block 2, got %d (err %v)", n, err)
	}
	if rec, err := kvs.Get("ns1", "k", 0); err != nil {
		t.Fatalf("Get latest: %v", err)
	} else if rec == nil || string(rec.Value) != "block2" {
		t.Errorf("expected latest \"block2\", got %+v", rec)
	}
	// Time-travel to block 1 sees the earlier write.
	if rec, err := kvs.Get("ns1", "k", 1); err != nil {
		t.Fatalf("Get as of block 1: %v", err)
	} else if rec == nil || string(rec.Value) != "block1" {
		t.Errorf("as of block 1 expected \"block1\", got %+v", rec)
	}
}
