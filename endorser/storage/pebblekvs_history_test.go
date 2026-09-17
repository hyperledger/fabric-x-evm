/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// TestPebbleHistoryWindowReads checks the retained window: reads inside it see the
// value as of that block, and reads older than it are refused rather than answered
// with a newer value.
func TestPebbleHistoryWindowReads(t *testing.T) {
	const window = 4
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	// One write per block, so block i holds value "vi".
	const blocks_ = 10
	for i := uint64(1); i <= blocks_; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}

	// Inside the window each block resolves to its own write.
	for i := uint64(blocks_ - window + 1); i <= blocks_; i++ {
		rec, err := kvs.Get("ns1", "k", i)
		if err != nil {
			t.Fatalf("Get as of block %d: %v", i, err)
		}
		want := fmt.Sprintf("v%d", i)
		if rec == nil || string(rec.Value) != want {
			t.Errorf("as of block %d: want %q, got %+v", i, want, rec)
		}
	}

	// Outside it, the versions are pruned, so the read is refused.
	if _, err := kvs.NewSnapshot(new(uint64(2))); err == nil {
		t.Errorf("expected NewSnapshot(2) to fail: block 2 is outside the %d-block window", window)
	}

	// The latest value is always available.
	rec, err := kvs.Get("ns1", "k", 0)
	if err != nil {
		t.Fatalf("Get latest: %v", err)
	}
	if rec == nil || string(rec.Value) != fmt.Sprintf("v%d", blocks_) {
		t.Errorf("latest: want v%d, got %+v", blocks_, rec)
	}
}

// TestPebbleHistoryPruneBoundsGrowth checks that rewriting the same keys does not
// grow the retained history without bound: the number of history entries settles
// at roughly keys x window rather than keys x rewrites.
func TestPebbleHistoryPruneBoundsGrowth(t *testing.T) {
	const (
		window   = 3
		numKeys  = 5
		rewrites = 40
	)
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	for r := uint64(1); r <= rewrites; r++ {
		writes := make([]blocks.KVWrite, numKeys)
		for k := range writes {
			writes[k] = blocks.KVWrite{
				Key:   fmt.Sprintf("k%d", k),
				Value: []byte(fmt.Sprintf("r%d", r)),
			}
		}
		if err := kvs.Handle(ctx, mkBlock(r, 0, fmt.Sprintf("tx%d", r), true, "ns1", writes...)); err != nil {
			t.Fatalf("Handle block %d: %v", r, err)
		}
	}

	// Count retained history entries directly.
	it := kvs.db.NewIterator([]byte{prefixHistory}, nil)
	defer it.Release()
	retained := 0
	for it.Next() {
		retained++
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterate history: %v", err)
	}

	// Without pruning this would be numKeys*(rewrites-1) = 195. With a window of 3
	// it should stay within a small multiple of numKeys*window.
	if maxExpected := numKeys * (window + 1); retained > maxExpected {
		t.Errorf("history not pruned: %d entries retained, expected at most %d", retained, maxExpected)
	}
	if retained == 0 {
		t.Error("expected some retained history for in-window time travel")
	}
}

// TestPebbleSnapshotIsolation pins a snapshot at head and then commits a newer
// block. Values are overwritten in place, so a snapshot that only point-read the
// current record would observe the later write.
func TestPebbleSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), 128)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("v1")})); err != nil {
		t.Fatalf("Handle block 1: %v", err)
	}

	snap, err := kvs.NewSnapshot(nil) // pinned at head, the common case
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer snap.Close()

	if rec, err := snap.Get("ns1", "k"); err != nil {
		t.Fatalf("first Get: %v", err)
	} else if rec == nil || string(rec.Value) != "v1" {
		t.Fatalf("first Get: want v1, got %+v", rec)
	}

	if err := kvs.Handle(ctx, mkBlock(2, 0, "tx2", true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("v2")})); err != nil {
		t.Fatalf("Handle block 2: %v", err)
	}

	rec, err := snap.Get("ns1", "k")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("snapshot pinned at block 1 observed a later commit: want v1, got %+v", rec)
	}
	if rec != nil && rec.BlockNum != 1 {
		t.Errorf("want record from block 1, got block %d", rec.BlockNum)
	}
}

// TestPebbleWindowBoundSurvivesLargerHistorySize reopens with a bigger window than
// the store was pruned under. The bound must come from what was pruned, not from the
// configured historySize.
func TestPebbleWindowBoundSurvivesLargerHistorySize(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	kvs, err := NewPebbleKVS(dir, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := uint64(1); i <= 10; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}
	if err := kvs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewPebbleKVS(dir, 100) // much larger window than was in force
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.NewSnapshot(new(uint64(3))); err == nil {
		t.Error("expected NewSnapshot(3) to fail: its versions were pruned under the smaller window")
	}
}

// TestPebblePruneHandlesBlockGaps commits a run of blocks, then jumps far ahead.
// Block numbers are not guaranteed contiguous, and a gap must not leak entries.
func TestPebblePruneHandlesBlockGaps(t *testing.T) {
	const window = 3
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	write := func(block uint64) {
		if err := kvs.Handle(ctx, mkBlock(block, 0, fmt.Sprintf("tx%d", block), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", block))})); err != nil {
			t.Fatalf("Handle block %d: %v", block, err)
		}
	}
	for _, b := range []uint64{1, 2, 3, 50, 51, 52} {
		write(b)
	}

	count := func(prefix byte) int {
		it := kvs.db.NewIterator([]byte{prefix}, nil)
		defer it.Release()
		n := 0
		for it.Next() {
			n++
		}
		if err := it.Error(); err != nil {
			t.Fatalf("iterate prefix %q: %v", prefix, err)
		}
		return n
	}

	// Blocks 1..3 are far outside the window at head 52 and must be gone.
	if got := count(prefixBlockIndex); got > window {
		t.Errorf("index entries leaked across the block gap: %d retained, want at most %d", got, window)
	}
	if got := count(prefixHistory); got > window {
		t.Errorf("history entries leaked across the block gap: %d retained, want at most %d", got, window)
	}
}

// TestPebbleSnapshotSurvivesPrune holds a reader open while enough blocks commit to
// prune past its pinned height. The reader's pin must keep the history it falls back
// to alive; otherwise keys it can see read as absent, and the endorser holds one
// reader for a whole simulation.
func TestPebbleSnapshotSurvivesPrune(t *testing.T) {
	const window = 2 // the production default
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("v1")})); err != nil {
		t.Fatalf("Handle block 1: %v", err)
	}

	snap, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer snap.Close()

	// Commit well past the window while the reader is open.
	for i := uint64(2); i <= 8; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
		rec, err := snap.Get("ns1", "k")
		if err != nil {
			t.Fatalf("Get after block %d: %v", i, err)
		}
		if rec == nil || string(rec.Value) != "v1" {
			t.Fatalf("after block %d the pinned reader lost its value: want v1, got %+v", i, rec)
		}
	}
}

// TestPebbleOldestMarkerIsMonotonic reopens with a larger window and then keeps
// committing. The marker must not follow the widened window backwards, or reads
// whose versions were already pruned get accepted again and answered as absent.
func TestPebbleOldestMarkerIsMonotonic(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	kvs, err := NewPebbleKVS(dir, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := uint64(1); i <= 10; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}
	if err := kvs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewPebbleKVS(dir, 100)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	// Keep committing under the larger window, which lowers the computed cutoff.
	for i := uint64(11); i <= 20; i++ {
		if err := reopened.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}

	if _, err := reopened.NewSnapshot(new(uint64(3))); err == nil {
		t.Error("expected NewSnapshot(3) to fail: block 3's versions were pruned under the smaller window")
	}
}

// TestPebbleWindowWidthMatchesLightKVS checks the retained range is
// [head-historySize, head], matching LightKVS. historySize=1 is used by several
// integration configs and must still serve head-1.
func TestPebbleWindowWidthMatchesLightKVS(t *testing.T) {
	const window = 1
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	for i := uint64(1); i <= 5; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}

	// head-1 must be readable with historySize=1.
	rec, err := kvs.Get("ns1", "k", 4)
	if err != nil {
		t.Fatalf("Get as of block 4 (head-1): %v", err)
	}
	if rec == nil || string(rec.Value) != "v4" {
		t.Errorf("as of block 4: want v4, got %+v", rec)
	}
}

// TestPebbleDoubleCloseKeepsOtherPin closes one reader twice while a second is
// pinned at the same height: the pin must be released exactly once.
func TestPebbleDoubleCloseKeepsOtherPin(t *testing.T) {
	const window = 2
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	if err := kvs.Handle(ctx, mkBlock(1, 0, "tx1", true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("v1")})); err != nil {
		t.Fatalf("Handle block 1: %v", err)
	}

	first, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot first: %v", err)
	}
	survivor, err := kvs.NewSnapshot(nil) // same height
	if err != nil {
		t.Fatalf("NewSnapshot survivor: %v", err)
	}
	defer survivor.Close()

	// Close the first reader twice, concurrently.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = first.Close()
		}()
	}
	wg.Wait()

	for i := uint64(2); i <= 8; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}

	rec, err := survivor.Get("ns1", "k")
	if err != nil {
		t.Fatalf("survivor Get: %v", err)
	}
	if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("surviving reader lost its pinned value: want v1, got %+v", rec)
	}
}

// TestPebbleReplayAgainstHistoryWindow covers the interaction between the
// per-write idempotency check and pruning. A replayed write is verified against
// whatever the store still holds for its (key, block, tx) coordinate — the key's
// latest value, or a previous one retained in the window — so a divergent replay
// inside the window is still rejected. Below the window the record has been
// pruned, leaving nothing to verify against, so the replay is accepted rather
// than reported as a missing write.
func TestPebbleReplayAgainstHistoryWindow(t *testing.T) {
	const window = 2
	const blockCount = 10
	ctx := context.Background()
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	for i := uint64(1); i <= blockCount; i++ {
		if err := kvs.Handle(ctx, mkBlock(i, 0, fmt.Sprintf("tx%d", i), true, "ns1",
			blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", i))})); err != nil {
			t.Fatalf("Handle block %d: %v", i, err)
		}
	}

	if oldest := kvs.oldestRetained.Load(); oldest != blockCount-window {
		t.Fatalf("oldest retained block: got %d, want %d", oldest, blockCount-window)
	}

	// Inside the window, the retained record still catches a divergent replay.
	if err := kvs.Handle(ctx, mkBlock(blockCount-1, 0, fmt.Sprintf("tx%d", blockCount-1), true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("divergent")})); err == nil {
		t.Errorf("expected a divergent replay of in-window block %d to be rejected", blockCount-1)
	}

	// An identical replay inside the window is a no-op.
	if err := kvs.Handle(ctx, mkBlock(blockCount-1, 0, fmt.Sprintf("tx%d", blockCount-1), true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte(fmt.Sprintf("v%d", blockCount-1))})); err != nil {
		t.Errorf("identical replay of in-window block %d: %v", blockCount-1, err)
	}

	// Below the window the record is gone, so the replay is accepted unverified
	// rather than mistaken for a write that never landed.
	if err := kvs.Handle(ctx, mkBlock(2, 0, "tx2", true, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("v2")})); err != nil {
		t.Errorf("replay of pruned block 2: %v", err)
	}

	// None of the replays re-versioned the key or disturbed its latest value.
	rec, err := kvs.Get("ns1", "k", 0)
	if err != nil {
		t.Fatalf("Get latest: %v", err)
	}
	if want := fmt.Sprintf("v%d", blockCount); rec == nil || string(rec.Value) != want {
		t.Errorf("latest value: want %q, got %+v", want, rec)
	}
	if rec.Version != blockCount-1 {
		t.Errorf("latest version: got %d, want %d", rec.Version, blockCount-1)
	}
	if bn, err := kvs.BlockNumber(ctx); err != nil || bn != blockCount {
		t.Errorf("checkpoint after replays: got %d (err %v), want %d", bn, err, blockCount)
	}
}
