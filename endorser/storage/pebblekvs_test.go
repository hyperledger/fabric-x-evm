/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"fmt"
	"strings"
	"testing"
)

// update is a test-only entry point into commitBlock for a raw batch of
// writes, not carrying any guarantee of its own: production always goes
// through Handle, which calls commitBlock directly.
func (p *PebbleKVS) update(updates []KeyValueVersion) error {
	if len(updates) == 0 {
		return nil
	}

	// Sanity check to prevent incorrectly written tests.
	blockNum := updates[0].BlockNum
	for i := range updates {
		if updates[i].BlockNum != blockNum {
			return fmt.Errorf(
				"pebble kvs: update batch spans multiple blocks (%d at index 0, %d at index %d); "+
					"writes must be grouped by block before committing",
				blockNum, updates[i].BlockNum, i)
		}
	}

	return p.commitBlock(blockNum, updates)
}

// TestPebbleUpdateRejectsMultiBlockBatch is PebbleKVS-specific: update is its
// batch primitive and rejects a batch spanning block numbers, where LightKVS
// takes the block number from the first entry.
func TestPebbleUpdateRejectsMultiBlockBatch(t *testing.T) {
	kvs, err := NewPebbleKVS(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer kvs.Close()

	err = kvs.update([]KeyValueVersion{
		{Key: "ns1:a", BlockNum: 1, TxNum: 0, Value: []byte("a")},
		{Key: "ns1:b", BlockNum: 2, TxNum: 0, Value: []byte("b")},
	})
	if err == nil {
		t.Fatal("expected error for multi-block batch, got nil")
	}
	if !strings.Contains(err.Error(), "spans multiple blocks") {
		t.Errorf("error should mention 'spans multiple blocks', got: %v", err)
	}

	if n := mustBlockNumber(t, kvs); n != 0 {
		t.Errorf("expected block 0 (nothing committed), got %d", n)
	}
}
