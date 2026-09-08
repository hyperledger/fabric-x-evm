/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"
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
