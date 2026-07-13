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

	reader, err := kvs.NewSnapshot(0)
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

	reader, err := kvs.NewSnapshot(0)
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
