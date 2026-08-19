/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"testing"
)

func TestNewRevertibleLightKVS(t *testing.T) {
	base := NewLightKVS(2)
	kvs := NewRevertibleLightKVS(base)
	if kvs == nil {
		t.Fatal("NewRevertibleLightKVS returned nil")
	}
	if kvs.NextIndex.Load() != 0 {
		t.Errorf("expected NextIndex reset to 0, got %d", kvs.NextIndex.Load())
	}
}

// TestRevertibleLightKVS_SatisfiesRevertible is a compile-time-ish check that the
// type gateway/app and gateway/testimpl rely on actually implements Revertible.
func TestRevertibleLightKVS_SatisfiesRevertible(t *testing.T) {
	var _ Revertible = NewRevertibleLightKVS(NewLightKVS(2))
}

// TestRevertibleLightKVS_RevertToBlock exercises the same sequence Hardhat's
// evm_snapshot/evm_revert drives: commit a couple of blocks, then roll back to an
// earlier one and verify reads reflect the rolled-back state.
func TestRevertibleLightKVS_RevertToBlock(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(4))

	// Block 1: key1 = v1
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update block 1 failed: %v", err)
	}

	// Block 2: key1 = v2, key2 = vB (new key)
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v2"), BlockNum: 2, TxNum: 0, TxID: "tx2"},
		{Key: "ns1:key2", Value: []byte("vB"), BlockNum: 2, TxNum: 1, TxID: "tx2b"},
	}); err != nil {
		t.Fatalf("Update block 2 failed: %v", err)
	}

	if err := kvs.RevertToBlock(1); err != nil {
		t.Fatalf("RevertToBlock(1) failed: %v", err)
	}

	blockNum, err := kvs.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber failed: %v", err)
	}
	if blockNum != 1 {
		t.Errorf("expected block number 1 after revert, got %d", blockNum)
	}

	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	// key1 should read back its block-1 value.
	record1, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get key1 failed: %v", err)
	}
	if record1 == nil || string(record1.Value) != "v1" {
		t.Errorf("expected key1 = 'v1' after revert, got %+v", record1)
	}

	// key2 was created after the revert target; it must not read back as "vB".
	record2, err := reader.Get("ns1", "key2")
	if err != nil {
		t.Fatalf("Get key2 failed: %v", err)
	}
	if record2 != nil && string(record2.Value) == "vB" {
		t.Errorf("key2 should not retain its post-revert value, got %+v", record2)
	}
}

// TestRevertibleLightKVS_RevertToBlock_NoOp reverting to the current block is a
// successful no-op.
func TestRevertibleLightKVS_RevertToBlock_NoOp(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(2))

	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if err := kvs.RevertToBlock(1); err != nil {
		t.Fatalf("RevertToBlock to current block should be a no-op, got error: %v", err)
	}
}

// TestRevertibleLightKVS_RevertToBlock_NotFound reverting to an unknown block errors.
func TestRevertibleLightKVS_RevertToBlock_NotFound(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(2))

	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if err := kvs.RevertToBlock(99); err == nil {
		t.Error("expected error reverting to a block never seen, got nil")
	}
}

// TestRevertibleLightKVS_RevertToBlock_SkipsEmptyBlocks is the revert-path twin of
// TestRevertibleLightKVS_NewSnapshot_SkipsEmptyBlocks: an empty block never calls
// Update, so it never gets a history entry, but evm_revert to that exact block
// number must still succeed by falling back to the nearest older snapshot.
func TestRevertibleLightKVS_RevertToBlock_SkipsEmptyBlocks(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(8))

	// Blocks 1 and 5 wrote; 2-4 were empty (no Update, so no history entry).
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update block 1: %v", err)
	}
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v5"), BlockNum: 5, TxNum: 0, TxID: "tx5"},
	}); err != nil {
		t.Fatalf("Update block 5: %v", err)
	}

	// Revert to block 3: no exact entry, but state hasn't changed since block 1.
	if err := kvs.RevertToBlock(3); err != nil {
		t.Fatalf("RevertToBlock(3) should fall back to the nearest older snapshot, got error: %v", err)
	}

	blockNum, err := kvs.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber failed: %v", err)
	}
	if blockNum != 3 {
		t.Errorf("expected reported block number 3 after revert, got %d", blockNum)
	}

	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	rec, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("expected key1='v1' after revert to empty block 3, got %+v", rec)
	}
}

// TestRevertibleLightKVS_RevertThenContinue verifies that after a revert the store
// can keep accepting new blocks — the core interactive-testnode usage pattern.
func TestRevertibleLightKVS_RevertThenContinue(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(4))

	for i, val := range []string{"v1", "v2", "v3"} {
		if err := kvs.Update([]KeyValueVersion{
			{Key: "ns1:key1", Value: []byte(val), BlockNum: uint64(i + 1), TxNum: 0, TxID: "tx"},
		}); err != nil {
			t.Fatalf("Update block %d failed: %v", i+1, err)
		}
	}

	if err := kvs.RevertToBlock(1); err != nil {
		t.Fatalf("RevertToBlock(1) failed: %v", err)
	}

	// Continue committing after the revert point.
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1-continued"), BlockNum: 5, TxNum: 0, TxID: "tx5"},
	}); err != nil {
		t.Fatalf("Update after revert failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	record, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record == nil || string(record.Value) != "v1-continued" {
		t.Errorf("expected 'v1-continued' after revert+continue, got %+v", record)
	}
}

// TestRevertibleLightKVS_NewSnapshot_SkipsEmptyBlocks ensures historical reads
// find state when Fabric block numbers advance without an Update for empty blocks
// (hop-by-distance lookup used to miss those snapshots).
func TestRevertibleLightKVS_NewSnapshot_SkipsEmptyBlocks(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(8))

	// Blocks 1 and 5 wrote; 2–4 were empty (no Update).
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update block 1: %v", err)
	}
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v5"), BlockNum: 5, TxNum: 0, TxID: "tx5"},
	}); err != nil {
		t.Fatalf("Update block 5: %v", err)
	}

	// Read as-of block 3: no entry for 3, but state is still v1 from block 1.
	reader, err := kvs.NewSnapshot(new(uint64(3)))
	if err != nil {
		t.Fatalf("NewSnapshot(BlockAt(3)): %v", err)
	}
	defer reader.Close()

	rec, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil || string(rec.Value) != "v1" {
		t.Fatalf("expected key1=v1 at block 3, got %+v", rec)
	}

	// Exact match at block 1 still works.
	r1, err := kvs.NewSnapshot(new(uint64(1)))
	if err != nil {
		t.Fatalf("NewSnapshot(BlockAt(1)): %v", err)
	}
	defer r1.Close()
	rec1, err := r1.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get at 1: %v", err)
	}
	if rec1 == nil || string(rec1.Value) != "v1" {
		t.Fatalf("expected key1=v1 at block 1, got %+v", rec1)
	}

	// At or past current returns latest (v5).
	rLatest, err := kvs.NewSnapshot(new(uint64(5)))
	if err != nil {
		t.Fatalf("NewSnapshot(BlockAt(5)): %v", err)
	}
	defer rLatest.Close()
	recLatest, err := rLatest.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get latest: %v", err)
	}
	if recLatest == nil || string(recLatest.Value) != "v5" {
		t.Fatalf("expected key1=v5 at block 5, got %+v", recLatest)
	}
}

// TestRevertibleLightKVS_NewSnapshot_NotFound covers the error path when no
// preserved snapshot is at or before the requested block (e.g. history slots
// cleared while current has already advanced).
func TestRevertibleLightKVS_NewSnapshot_NotFound(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(4))

	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v10"), BlockNum: 10, TxNum: 0, TxID: "tx10"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Drop the only history entry (pre-update empty snapshot) so nothing
	// older than current remains.
	kvs.History[0].Store(nil)

	if _, err := kvs.NewSnapshot(new(uint64(3))); err == nil {
		t.Fatal("expected error when no history exists at or before block 3")
	}
}

// TestRevertibleLightKVS_HistoryExhaustedPanics documents the deliberate trade-off:
// unlike plain LightKVS (which wraps forever), a revert-capable instance panics once
// its bounded history window fills up, since a wrapping ring buffer can't guarantee
// a stable index to revert to. This is why RevertibleLightKVS must never be used for
// a long-running, continuously-committing endorser.
func TestRevertibleLightKVS_HistoryExhaustedPanics(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(1))

	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v1"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("first Update failed: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic once history window is exhausted, got none")
		}
	}()

	_ = kvs.Update([]KeyValueVersion{
		{Key: "ns1:key1", Value: []byte("v2"), BlockNum: 2, TxNum: 0, TxID: "tx2"},
	})
}

// TestRevertibleLightKVS_RevertToBlock_DuplicateBlockNumber pins which snapshot
// wins when several carry the same block number.
//
// The testnode reaches this on every startup: FundTestAccounts seeds balances
// through the normal Handle path but numbers the write with the *current*
// height rather than advancing it, so block 0 ends up in history twice — empty,
// then funded. History is oldest-first, so resolving a revert by first match
// returns the state from before the funding and every test account reads back
// at zero.
func TestRevertibleLightKVS_RevertToBlock_DuplicateBlockNumber(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(4))

	// Two writes both labelled block 0, the way startup funding lands on top of
	// the initial state.
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:balance", Value: []byte("empty"), BlockNum: 0, TxNum: 0, TxID: "genesis"},
	}); err != nil {
		t.Fatalf("Update genesis failed: %v", err)
	}
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:balance", Value: []byte("funded"), BlockNum: 0, TxNum: 0, TxID: "funding"},
	}); err != nil {
		t.Fatalf("Update funding failed: %v", err)
	}

	// Then a real block, so the revert has to come out of history rather than
	// being a no-op against current.
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:balance", Value: []byte("spent"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update block 1 failed: %v", err)
	}

	if err := kvs.RevertToBlock(0); err != nil {
		t.Fatalf("RevertToBlock(0) failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	rec, err := reader.Get("ns1", "balance")
	if err != nil {
		t.Fatalf("Get balance failed: %v", err)
	}
	if rec == nil || string(rec.Value) != "funded" {
		t.Errorf("revert to block 0 should restore the last state written at block 0 (%q), got %+v", "funded", rec)
	}
}

// TestRevertibleLightKVS_NewSnapshot_DuplicateBlockNumber is the read-path twin
// of the above: an as-of-block-0 read must see the same state a revert to block
// 0 restores, not the earlier snapshot that shares the number.
func TestRevertibleLightKVS_NewSnapshot_DuplicateBlockNumber(t *testing.T) {
	kvs := NewRevertibleLightKVS(NewLightKVS(4))

	for _, w := range []struct{ value, txID string }{{"empty", "genesis"}, {"funded", "funding"}} {
		if err := kvs.Update([]KeyValueVersion{
			{Key: "ns1:balance", Value: []byte(w.value), BlockNum: 0, TxNum: 0, TxID: w.txID},
		}); err != nil {
			t.Fatalf("Update %s failed: %v", w.txID, err)
		}
	}
	if err := kvs.Update([]KeyValueVersion{
		{Key: "ns1:balance", Value: []byte("spent"), BlockNum: 1, TxNum: 0, TxID: "tx1"},
	}); err != nil {
		t.Fatalf("Update block 1 failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(new(uint64(0)))
	if err != nil {
		t.Fatalf("NewSnapshot(BlockAt(0)) failed: %v", err)
	}
	defer reader.Close()

	rec, err := reader.Get("ns1", "balance")
	if err != nil {
		t.Fatalf("Get balance failed: %v", err)
	}
	if rec == nil || string(rec.Value) != "funded" {
		t.Errorf("reading at block 0 should see the last state written at block 0 (%q), got %+v", "funded", rec)
	}
}
