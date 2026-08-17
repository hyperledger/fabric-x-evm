/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"testing"

	_ "modernc.org/sqlite"
)

// setupSnapshotStore builds a SnapshotStore over blocks 0..upTo, each carrying
// one transaction, and leaves the cached tip at upTo.
func setupSnapshotStore(t *testing.T, upTo uint64) *SnapshotStore {
	t.Helper()
	store := setupTestDB(t)
	for n := uint64(0); n <= upTo; n++ {
		blockHash := makeHash(byte(n))
		insertTestBlock(t, store, n, blockHash)
		insertTestTransaction(t, store, n, blockHash, makeHash(byte(0xA0+n)), 0)
	}
	store.CachedBlockNumber.Store(upTo)
	return NewSnapshotStore(store)
}

func countRows(t *testing.T, s *SnapshotStore, table string) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestSnapshotStore_SatisfiesRevertible(t *testing.T) {
	var _ Revertible = NewSnapshotStore(setupTestDB(t))
}

// TestSnapshotStore_Snapshot returns the current tip without touching the data:
// the rows at or below that height are the snapshot.
func TestSnapshotStore_Snapshot(t *testing.T) {
	s := setupSnapshotStore(t, 5)

	before := countRows(t, s, "blocks")
	got, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got != 5 {
		t.Errorf("Snapshot() = %d, want 5", got)
	}
	if after := countRows(t, s, "blocks"); after != before {
		t.Errorf("Snapshot must not modify the store: blocks %d -> %d", before, after)
	}
}

// TestSnapshotStore_RevertToBlock drops everything above the target, in both the
// blocks table and the transactions that reference it, and moves the tip back.
func TestSnapshotStore_RevertToBlock(t *testing.T) {
	s := setupSnapshotStore(t, 5)

	if err := s.RevertToBlock(t.Context(), 2); err != nil {
		t.Fatalf("RevertToBlock(2): %v", err)
	}

	if got := countRows(t, s, "blocks"); got != 3 { // 0, 1, 2
		t.Errorf("blocks after revert = %d, want 3", got)
	}
	if got := countRows(t, s, "transactions"); got != 3 {
		t.Errorf("transactions after revert = %d, want 3", got)
	}
	if got := s.CachedBlockNumber.Load(); got != 2 {
		t.Errorf("cached tip after revert = %d, want 2", got)
	}

	latest, err := s.LatestBlock(t.Context(), false)
	if err != nil {
		t.Fatalf("LatestBlock: %v", err)
	}
	if latest == nil || latest.BlockNumber != 2 {
		t.Errorf("latest block after revert = %+v, want block 2", latest)
	}
}

// TestSnapshotStore_RevertToBlock_Repeated walks backwards through several
// snapshots, which is the only direction evm_revert ever goes.
func TestSnapshotStore_RevertToBlock_Repeated(t *testing.T) {
	s := setupSnapshotStore(t, 9)

	for _, target := range []uint64{7, 4, 0} {
		if err := s.RevertToBlock(t.Context(), target); err != nil {
			t.Fatalf("RevertToBlock(%d): %v", target, err)
		}
		if got := countRows(t, s, "blocks"); got != int(target)+1 {
			t.Errorf("after revert to %d: blocks = %d, want %d", target, got, target+1)
		}
		if got := s.CachedBlockNumber.Load(); got != target {
			t.Errorf("after revert to %d: cached tip = %d", target, got)
		}
	}
}

// TestSnapshotStore_RevertToBlock_CurrentIsNoOp reverting to the tip changes
// nothing and succeeds.
func TestSnapshotStore_RevertToBlock_CurrentIsNoOp(t *testing.T) {
	s := setupSnapshotStore(t, 3)

	if err := s.RevertToBlock(t.Context(), 3); err != nil {
		t.Fatalf("RevertToBlock(3): %v", err)
	}
	if got := countRows(t, s, "blocks"); got != 4 {
		t.Errorf("blocks = %d, want 4 (unchanged)", got)
	}
}

// TestSnapshotStore_RevertToBlock_ForwardRejected a revert only ever moves
// backwards; asking for a height above the tip is a caller error, not a
// silently-ignored no-op.
func TestSnapshotStore_RevertToBlock_ForwardRejected(t *testing.T) {
	s := setupSnapshotStore(t, 3)

	if err := s.RevertToBlock(t.Context(), 9); err == nil {
		t.Fatal("RevertToBlock(9) with tip at 3 should fail")
	}
	if got := s.CachedBlockNumber.Load(); got != 3 {
		t.Errorf("failed revert must leave the tip alone, got %d", got)
	}
	if got := countRows(t, s, "blocks"); got != 4 {
		t.Errorf("failed revert must leave the data alone, got %d blocks", got)
	}
}

// TestSnapshotStore_SnapshotThenRevertRoundTrip is the sequence evm_snapshot and
// evm_revert actually drive: name the tip, commit more blocks, come back.
func TestSnapshotStore_SnapshotThenRevertRoundTrip(t *testing.T) {
	s := setupSnapshotStore(t, 2)

	id, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	for n := uint64(3); n <= 6; n++ {
		blockHash := makeHash(byte(n))
		insertTestBlock(t, s.Store, n, blockHash)
		insertTestTransaction(t, s.Store, n, blockHash, makeHash(byte(0xA0+n)), 0)
	}
	s.CachedBlockNumber.Store(6)

	if err := s.RevertToBlock(t.Context(), id); err != nil {
		t.Fatalf("RevertToBlock(%d): %v", id, err)
	}
	if got := countRows(t, s, "blocks"); got != 3 {
		t.Errorf("blocks after round trip = %d, want 3", got)
	}
	if got := s.CachedBlockNumber.Load(); got != id {
		t.Errorf("cached tip after round trip = %d, want %d", got, id)
	}
}
