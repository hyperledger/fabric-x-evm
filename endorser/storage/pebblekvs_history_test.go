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

// countPrefix returns how many keys the store holds under prefix.
func countPrefix(t *testing.T, kvs *PebbleKVS, prefix byte) int {
	t.Helper()
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

// writeBlock commits one block holding a single write of "v<block>" to ns1/k.
func writeBlock(t *testing.T, kvs *PebbleKVS, block uint64) {
	t.Helper()
	if err := kvs.Handle(context.Background(), mkBlock(block, 0, fmt.Sprintf("tx%d", block), true, "ns1",
		blocks.KVWrite{Key: "k", Value: fmt.Appendf(nil, "v%d", block)})); err != nil {
		t.Fatalf("Handle block %d: %v", block, err)
	}
}

// TestPebbleHistoryWindowReads checks the retained window: reads inside it see the
// value as of that block, and reads older than it are refused rather than answered
// with a newer value.
func TestPebbleHistoryWindowReads(t *testing.T) {
	const (
		window    = 4
		numBlocks = 10
	)
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	// One write per block, so block i holds value "vi".
	for i := uint64(1); i <= numBlocks; i++ {
		writeBlock(t, kvs, i)
	}

	// Inside the window each block resolves to its own write.
	for i := uint64(numBlocks - window); i <= numBlocks; i++ {
		want := fmt.Sprintf("v%d", i)
		wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", i), want)
	}

	// Outside it, the versions are pruned, so the read is refused.
	if _, err := kvs.NewSnapshot(new(uint64(2))); err == nil {
		t.Errorf("expected NewSnapshot(2) to fail: block 2 is outside the %d-block window", window)
	}

	// The latest value is always available.
	wantValue(t, mustGet(t, kvs, "ns1", "k"), fmt.Sprintf("v%d", numBlocks))
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
				Value: fmt.Appendf(nil, "r%d", r),
			}
		}
		mustHandle(t, kvs, mkBlock(r, 0, fmt.Sprintf("tx%d", r), true, "ns1", writes...))
	}

	// Without pruning this would be numKeys*(rewrites-1) = 195. With a window of 3
	// it should stay within a small multiple of numKeys*window.
	retained := countPrefix(t, kvs, prefixHistory)
	if maxExpected := numKeys * (window + 1); retained > maxExpected {
		t.Errorf("history not pruned: %d entries retained, expected at most %d", retained, maxExpected)
	}
	if retained == 0 {
		t.Error("expected some retained history for in-window time travel")
	}
	// The index is pruned alongside the entries it names.
	if index := countPrefix(t, kvs, prefixIndex); index != retained {
		t.Errorf("index and history disagree: %d index entries for %d history entries", index, retained)
	}
}

// TestPebbleSnapshotIsolation pins a snapshot at head and then commits a newer
// block. Records are overwritten in place, so a snapshot that only point-read the
// current record would observe the later write.
func TestPebbleSnapshotIsolation(t *testing.T) {
	kvs, err := NewPebbleKVS(t.TempDir(), 128)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)

	snap, err := kvs.NewSnapshot(nil) // pinned at head, the common case
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer snap.Close()

	if rec, err := snap.Get("ns1", "k"); err != nil {
		t.Fatalf("first Get: %v", err)
	} else {
		wantValue(t, rec, "v1")
	}

	writeBlock(t, kvs, 2)

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

// TestPebbleHistoryReadsAcrossGaps covers a key whose value is much older than
// the window: the retained record is keyed by the block it was written in, not by
// the block that superseded it, so every height between the two must resolve to
// it — and heights before the key existed must read as absent.
func TestPebbleHistoryReadsAcrossGaps(t *testing.T) {
	kvs, err := NewPebbleKVS(t.TempDir(), 128)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 5)  // "v5" stands until block 40 replaces it
	writeBlock(t, kvs, 40) // supersedes it, retaining the block-5 record

	for _, block := range []uint64{5, 6, 20, 39} {
		wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", block), "v5")
	}
	wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", 40), "v40")

	// Before the key existed there is nothing to return, at any height.
	for _, block := range []uint64{0, 4} {
		if rec := mustGetAsOf(t, kvs, "ns1", "k", block); !absent(rec) {
			t.Errorf("as of block %d, before the key was written: got %+v, want absent", block, rec)
		}
	}
}

// TestPebbleHistoryAcrossMultiWriteBlock reads around a block whose transactions
// wrote the same key twice. Only the surviving write is stored under the key, so
// the block's own height must resolve to it while the height below resolves to
// what the block superseded.
func TestPebbleHistoryAcrossMultiWriteBlock(t *testing.T) {
	kvs, err := NewPebbleKVS(t.TempDir(), 128)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1) // "v1"
	mustHandle(t, kvs, mkMultiTxBlock(2, "ns1",
		blocks.KVWrite{Key: "k", Value: []byte("lo")},
		blocks.KVWrite{Key: "k", Value: []byte("hi")}))
	writeBlock(t, kvs, 3) // supersedes the block-2 record, retaining it

	// Block 2 resolves to its highest-tx write, at the tip and from history alike.
	wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", 2), "hi")
	// Block 1 is unaffected by either of block 2's writes.
	wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", 1), "v1")
	wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", 3), "v3")
}

// TestPebbleFutureHeightServesTip asks for a height past the tip, which LightKVS
// serves from its current snapshot. The view must be pinned to the tip, not to the
// height asked for, or commits made after it was taken leak into it.
func TestPebbleFutureHeightServesTip(t *testing.T) {
	kvs, err := NewPebbleKVS(t.TempDir(), 128)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)

	snap, err := kvs.NewSnapshot(new(uint64(100))) // far past the tip
	if err != nil {
		t.Fatalf("NewSnapshot(100): %v", err)
	}
	defer snap.Close()

	writeBlock(t, kvs, 2)

	rec, err := snap.Get("ns1", "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("a view asked for a future height observed a later commit: want v1, got %+v", rec)
	}
}

// TestPebbleWindowBoundSurvivesLargerHistorySize reopens with a bigger window than
// the store was pruned under. The bound must come from what was pruned, not from the
// configured historySize.
func TestPebbleWindowBoundSurvivesLargerHistorySize(t *testing.T) {
	dir := t.TempDir()

	kvs, err := NewPebbleKVS(dir, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := uint64(1); i <= 10; i++ {
		writeBlock(t, kvs, i)
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
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	for _, b := range []uint64{1, 2, 3, 50, 51, 52} {
		writeBlock(t, kvs, b)
	}

	// Blocks 1..3 are far outside the window at head 52 and must be gone.
	if got := countPrefix(t, kvs, prefixIndex); got > window {
		t.Errorf("index entries leaked across the block gap: %d retained, want at most %d", got, window)
	}
	if got := countPrefix(t, kvs, prefixHistory); got > window {
		t.Errorf("history entries leaked across the block gap: %d retained, want at most %d", got, window)
	}
}

// TestPebbleSnapshotSurvivesPrune holds a reader open while enough blocks commit to
// prune past its pinned height. The reader's pin must keep the history it falls back
// to alive; otherwise keys it can see read as absent, and the endorser holds one
// reader for a whole simulation.
func TestPebbleSnapshotSurvivesPrune(t *testing.T) {
	const window = 2 // the production default
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)

	snap, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer snap.Close()

	// Commit well past the window while the reader is open.
	for i := uint64(2); i <= 8; i++ {
		writeBlock(t, kvs, i)

		rec, err := snap.Get("ns1", "k")
		if err != nil {
			t.Fatalf("Get after block %d: %v", i, err)
		}
		if rec == nil || string(rec.Value) != "v1" {
			t.Fatalf("after block %d the pinned reader lost its value: want v1, got %+v", i, rec)
		}
	}
}

// TestPebbleLowestReaderPinHoldsHistory opens two readers at different heights.
// Pruning has to respect the lowest of them, not just the newest — and has to
// resume once that one closes.
func TestPebbleLowestReaderPinHoldsHistory(t *testing.T) {
	const window = 2
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)
	older, err := kvs.NewSnapshot(nil) // pinned at 1
	if err != nil {
		t.Fatalf("NewSnapshot at block 1: %v", err)
	}

	writeBlock(t, kvs, 2)
	newer, err := kvs.NewSnapshot(nil) // pinned at 2
	if err != nil {
		t.Fatalf("NewSnapshot at block 2: %v", err)
	}

	// Commit past the window with both open: the lower pin governs, so both keep
	// reading their own heights.
	for i := uint64(3); i <= 8; i++ {
		writeBlock(t, kvs, i)
	}
	if rec, err := older.Get("ns1", "k"); err != nil {
		t.Fatalf("older Get: %v", err)
	} else if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("reader pinned at block 1 lost its value: want v1, got %+v", rec)
	}
	if rec, err := newer.Get("ns1", "k"); err != nil {
		t.Fatalf("newer Get: %v", err)
	} else if rec == nil || string(rec.Value) != "v2" {
		t.Errorf("reader pinned at block 2 lost its value: want v2, got %+v", rec)
	}

	// Closing the lower one lets pruning advance to the remaining pin, which must
	// still be respected.
	if err := older.Close(); err != nil {
		t.Fatalf("close older: %v", err)
	}
	for i := uint64(9); i <= 14; i++ {
		writeBlock(t, kvs, i)
	}
	if rec, err := newer.Get("ns1", "k"); err != nil {
		t.Fatalf("newer Get after older closed: %v", err)
	} else if rec == nil || string(rec.Value) != "v2" {
		t.Errorf("reader pinned at block 2 lost its value after the lower pin went away: want v2, got %+v", rec)
	}

	// With both gone, pruning catches up and the old heights stop being readable.
	if err := newer.Close(); err != nil {
		t.Fatalf("close newer: %v", err)
	}
	for i := uint64(15); i <= 20; i++ {
		writeBlock(t, kvs, i)
	}
	if _, err := kvs.NewSnapshot(new(uint64(2))); err == nil {
		t.Error("expected NewSnapshot(2) to fail once no reader pins it")
	}
}

// TestPebbleOldestMarkerIsMonotonic reopens with a larger window and then keeps
// committing. The marker must not follow the widened window backwards, or reads
// whose versions were already pruned get accepted again and answered as absent.
func TestPebbleOldestMarkerIsMonotonic(t *testing.T) {
	dir := t.TempDir()

	kvs, err := NewPebbleKVS(dir, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := uint64(1); i <= 10; i++ {
		writeBlock(t, kvs, i)
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
		writeBlock(t, reopened, i)
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
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	for i := uint64(1); i <= 5; i++ {
		writeBlock(t, kvs, i)
	}

	// head-1 must be readable with historySize=1.
	wantValue(t, mustGetAsOf(t, kvs, "ns1", "k", 4), "v4")
}

// TestPebbleDoubleCloseKeepsOtherPin closes one reader twice while a second is
// pinned at the same height: the pin must be released exactly once.
func TestPebbleDoubleCloseKeepsOtherPin(t *testing.T) {
	const window = 2
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)

	first, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot first: %v", err)
	}
	survivor, err := kvs.NewSnapshot(nil) // same height
	if err != nil {
		t.Fatalf("NewSnapshot survivor: %v", err)
	}
	defer survivor.Close()

	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if err := first.Close(); err != nil { // double close must not drop the other pin
		t.Fatalf("second close of first: %v", err)
	}

	// Commit past the window; the survivor's pin must still hold its history.
	for i := uint64(2); i <= 8; i++ {
		writeBlock(t, kvs, i)
	}
	if rec, err := survivor.Get("ns1", "k"); err != nil {
		t.Fatalf("survivor Get: %v", err)
	} else if rec == nil || string(rec.Value) != "v1" {
		t.Errorf("a double Close released the surviving reader's pin: want v1, got %+v", rec)
	}
}

// TestPebbleConcurrentReadersDuringCommits runs readers that open, read and close
// views while a writer commits behind them, which is how the endorser uses the
// store. Run it with -race to check the pin bookkeeping.
func TestPebbleConcurrentReadersDuringCommits(t *testing.T) {
	const (
		window    = 2
		numBlocks = 60
		readers   = 8
	)
	kvs, err := NewPebbleKVS(t.TempDir(), window)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	writeBlock(t, kvs, 1)

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: each iteration is one simulation — pin a view, read through it, close.
	for range readers {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}

				snap, err := kvs.NewSnapshot(nil)
				if err != nil {
					t.Errorf("NewSnapshot: %v", err)
					return
				}
				for range 5 {
					rec, err := snap.Get("ns1", "k")
					if err != nil {
						t.Errorf("Get: %v", err)
						break
					}
					// A view pinned at head always has its own block's value, whether it
					// comes from the current record or from pinned history.
					if rec == nil {
						t.Error("read through a pinned view found no value")
						break
					}
				}
				if err := snap.Close(); err != nil {
					t.Errorf("Close: %v", err)
					return
				}
			}
		})
	}

	for i := uint64(2); i <= numBlocks; i++ {
		writeBlock(t, kvs, i)
	}
	close(done)
	wg.Wait()

	// Pruning is commit-driven and was held back by the readers' pins, so commit a
	// few more blocks now that they are gone: history must fall back to the
	// configured window, proving every pin was released.
	for i := uint64(numBlocks + 1); i <= numBlocks+3; i++ {
		writeBlock(t, kvs, i)
	}
	if got := countPrefix(t, kvs, prefixHistory); got > window+1 {
		t.Errorf("reader pins were not released: %d history entries retained, want at most %d", got, window+1)
	}
}
