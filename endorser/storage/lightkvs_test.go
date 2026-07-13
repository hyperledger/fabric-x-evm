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

// TestNewLightKVS tests the creation of a new LightKVS instance
func TestNewLightKVS(t *testing.T) {
	kvs := NewLightKVS(1)
	if kvs == nil {
		t.Fatal("NewLightKVS returned nil")
	}

	// Verify initial snapshot exists and is empty
	snapshot := kvs.Current.Load()
	if snapshot == nil {
		t.Fatal("initial snapshot is nil")
	}
	if snapshot.BlockNumber != 0 {
		t.Errorf("expected initial block number 0, got %d", snapshot.BlockNumber)
	}
	if len(snapshot.Data) != 0 {
		t.Errorf("expected empty initial data, got %d entries", len(snapshot.Data))
	}
}

// TestNewSnapshot tests creating a new snapshot reader
func TestNewSnapshot(t *testing.T) {
	kvs := NewLightKVS(1)
	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	if reader == nil {
		t.Fatal("NewSnapshot returned nil")
	}

	// Verify reader is of correct type
	r, ok := reader.(*Reader)
	if !ok {
		t.Fatal("NewSnapshot did not return a *Reader")
	}
	if r.Snapshot == nil {
		t.Error("reader snapshot is nil")
	}
	if r.Kvs != kvs {
		t.Error("reader kvs reference is incorrect")
	}
}

// TestReaderGet tests reading values from a snapshot
func TestReaderGet(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add some data
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
		{
			Key:      "ns1:key2",
			Value:    []byte("value2"),
			BlockNum: 1,
			TxNum:    1,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Create reader and test Get
	reader, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	// Test existing key
	record, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.Namespace != "ns1" {
		t.Errorf("expected namespace 'ns1', got '%s'", record.Namespace)
	}
	if record.Key != "key1" {
		t.Errorf("expected key 'key1', got '%s'", record.Key)
	}
	if string(record.Value) != "value1" {
		t.Errorf("expected value 'value1', got '%s'", string(record.Value))
	}
	if record.BlockNum != 1 {
		t.Errorf("expected block num 1, got %d", record.BlockNum)
	}
	if record.TxNum != 0 {
		t.Errorf("expected tx num 0, got %d", record.TxNum)
	}
	if record.TxID != "tx1" {
		t.Errorf("expected tx id 'tx1', got '%s'", record.TxID)
	}
	if record.Version != 0 {
		t.Errorf("expected version 0, got %d", record.Version)
	}

	// Test non-existent key
	record, err = reader.Get("ns1", "nonexistent")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record != nil {
		t.Errorf("expected nil record for non-existent key, got %v", record)
	}
}

// TestReaderGetNilValue tests reading a nil value
func TestReaderGetNilValue(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add a key with nil value
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    nil,
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	record, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.Value != nil {
		t.Errorf("expected nil value, got %v", record.Value)
	}
}

// TestReaderClose tests closing a reader
func TestReaderClose(t *testing.T) {
	kvs := NewLightKVS(1)
	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}

	// Close the reader
	err = reader.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify snapshot is nil after close (need to type assert to access internal field)
	r, ok := reader.(*Reader)
	if ok && r.Snapshot != nil {
		t.Error("snapshot should be nil after Close")
	}

	// Verify Get returns error after close
	_, err = reader.Get("ns1", "key1")
	if err == nil {
		t.Error("expected error when calling Get on closed reader")
	}
	if err.Error() != "reader is closed" {
		t.Errorf("expected 'reader is closed' error, got '%v'", err)
	}
}

// TestUpdate tests updating the store
func TestUpdate(t *testing.T) {
	kvs := NewLightKVS(1)

	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
		{
			Key:      "ns1:key2",
			Value:    []byte("value2"),
			BlockNum: 1,
			TxNum:    1,
			TxID:     "tx2",
			IsDelete: false,
		},
	}

	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify data was updated
	snapshot := kvs.Current.Load()
	if snapshot.BlockNumber != 1 {
		t.Errorf("expected block number 1, got %d", snapshot.BlockNumber)
	}
	if len(snapshot.Data) != 2 {
		t.Errorf("expected 2 entries, got %d", len(snapshot.Data))
	}

	// Verify values
	vv1 := snapshot.Data["ns1:key1"]
	if vv1 == nil {
		t.Fatal("key1 not found")
	}
	if string(vv1.Value) != "value1" {
		t.Errorf("expected 'value1', got '%s'", string(vv1.Value))
	}
	if vv1.Version != 0 {
		t.Errorf("expected version 0, got %d", vv1.Version)
	}

	vv2 := snapshot.Data["ns1:key2"]
	if vv2 == nil {
		t.Fatal("key2 not found")
	}
	if string(vv2.Value) != "value2" {
		t.Errorf("expected 'value2', got '%s'", string(vv2.Value))
	}
}

// TestUpdateVersionIncrement tests that versions increment correctly
func TestUpdateVersionIncrement(t *testing.T) {
	kvs := NewLightKVS(1)

	// First update
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	snapshot := kvs.Current.Load()
	if snapshot.Data["ns1:key1"].Version != 0 {
		t.Errorf("expected version 0, got %d", snapshot.Data["ns1:key1"].Version)
	}

	// Second update to same key
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value2"),
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	snapshot = kvs.Current.Load()
	if snapshot.Data["ns1:key1"].Version != 1 {
		t.Errorf("expected version 1, got %d", snapshot.Data["ns1:key1"].Version)
	}

	// Third update
	updates3 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value3"),
			BlockNum: 3,
			TxNum:    0,
			TxID:     "tx3",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates3)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	snapshot = kvs.Current.Load()
	if snapshot.Data["ns1:key1"].Version != 2 {
		t.Errorf("expected version 2, got %d", snapshot.Data["ns1:key1"].Version)
	}
}

// TestUpdateDelete tests deleting keys
func TestUpdateDelete(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add a key
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify key exists
	snapshot := kvs.Current.Load()
	if _, ok := snapshot.Data["ns1:key1"]; !ok {
		t.Fatal("key1 should exist")
	}

	// Delete the key
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			IsDelete: true,
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx2",
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify key is deleted
	snapshot = kvs.Current.Load()
	if _, ok := snapshot.Data["ns1:key1"]; ok {
		t.Error("key1 should be deleted")
	}
}

// TestUpdateEmptyBatch tests updating with an empty batch
func TestUpdateEmptyBatch(t *testing.T) {
	kvs := NewLightKVS(1)

	err := kvs.Update([]KeyValueVersion{})
	if err != nil {
		t.Fatalf("Update with empty batch failed: %v", err)
	}

	snapshot := kvs.Current.Load()
	if snapshot.BlockNumber != 0 {
		t.Errorf("expected block number 0, got %d", snapshot.BlockNumber)
	}
}

// TestSnapshotIsolation tests that readers see a consistent snapshot
func TestSnapshotIsolation(t *testing.T) {
	kvs := NewLightKVS(1)

	// Initial data
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Create reader before update
	reader1, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader1.Close()

	// Update the store
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value2"),
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Create reader after update
	reader2, err := kvs.NewSnapshot(2)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader2.Close()

	// Reader1 should see old value
	record1, err := reader1.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record1.Value) != "value1" {
		t.Errorf("reader1 expected 'value1', got '%s'", string(record1.Value))
	}

	// Reader2 should see new value
	record2, err := reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record2.Value) != "value2" {
		t.Errorf("reader2 expected 'value2', got '%s'", string(record2.Value))
	}
}

// TestConcurrentReaders tests multiple concurrent readers
func TestConcurrentReaders(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add initial data
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Create multiple readers concurrently
	numReaders := 100
	var wg sync.WaitGroup
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			reader, err := kvs.NewSnapshot(1)
			if err != nil {
				t.Errorf("NewSnapshot failed: %v", err)
				return
			}
			defer reader.Close()

			record, err := reader.Get("ns1", "key1")
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			if record == nil {
				t.Error("expected record, got nil")
				return
			}
			if string(record.Value) != "value1" {
				t.Errorf("expected 'value1', got '%s'", string(record.Value))
			}
		}()
	}

	wg.Wait()
}

// TestHandle tests the Handle method with blocks
func TestHandle(t *testing.T) {
	kvs := NewLightKVS(1)
	ctx := context.Background()

	// Create a block with transactions
	block := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			{
				ID:     "tx1",
				Number: 0,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns1",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key1",
									Value:    []byte("value1"),
									IsDelete: false,
								},
								{
									Key:      "key2",
									Value:    []byte("value2"),
									IsDelete: false,
								},
							},
						},
					},
				},
			},
			{
				ID:     "tx2",
				Number: 1,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns2",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key3",
									Value:    []byte("value3"),
									IsDelete: false,
								},
							},
						},
					},
				},
			},
		},
	}

	err := kvs.Handle(ctx, block)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Verify data was stored
	reader, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	record1, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record1 == nil {
		t.Fatal("expected record1, got nil")
	}
	if string(record1.Value) != "value1" {
		t.Errorf("expected 'value1', got '%s'", string(record1.Value))
	}
	if record1.TxID != "tx1" {
		t.Errorf("expected tx id 'tx1', got '%s'", record1.TxID)
	}

	record2, err := reader.Get("ns1", "key2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record2 == nil {
		t.Fatal("expected record2, got nil")
	}
	if string(record2.Value) != "value2" {
		t.Errorf("expected 'value2', got '%s'", string(record2.Value))
	}

	record3, err := reader.Get("ns2", "key3")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record3 == nil {
		t.Fatal("expected record3, got nil")
	}
	if string(record3.Value) != "value3" {
		t.Errorf("expected 'value3', got '%s'", string(record3.Value))
	}
	if record3.TxID != "tx2" {
		t.Errorf("expected tx id 'tx2', got '%s'", record3.TxID)
	}
}

// TestHandleInvalidTransactions tests that invalid transactions are skipped
func TestHandleInvalidTransactions(t *testing.T) {
	kvs := NewLightKVS(1)
	ctx := context.Background()

	block := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			{
				ID:     "tx1",
				Number: 0,
				Valid:  false, // Invalid transaction
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns1",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key1",
									Value:    []byte("value1"),
									IsDelete: false,
								},
							},
						},
					},
				},
			},
			{
				ID:     "tx2",
				Number: 1,
				Valid:  true, // Valid transaction
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns1",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key2",
									Value:    []byte("value2"),
									IsDelete: false,
								},
							},
						},
					},
				},
			},
		},
	}

	err := kvs.Handle(ctx, block)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	// key1 from invalid tx should not exist
	record1, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record1 != nil {
		t.Error("key1 from invalid transaction should not exist")
	}

	// key2 from valid tx should exist
	record2, err := reader.Get("ns1", "key2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record2 == nil {
		t.Fatal("expected record2, got nil")
	}
	if string(record2.Value) != "value2" {
		t.Errorf("expected 'value2', got '%s'", string(record2.Value))
	}
}

// TestHandleDeletes tests handling delete operations
func TestHandleDeletes(t *testing.T) {
	kvs := NewLightKVS(1)
	ctx := context.Background()

	// First, add a key
	block1 := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			{
				ID:     "tx1",
				Number: 0,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns1",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key1",
									Value:    []byte("value1"),
									IsDelete: false,
								},
							},
						},
					},
				},
			},
		},
	}

	err := kvs.Handle(ctx, block1)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Verify key exists
	reader1, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	record1, err := reader1.Get("ns1", "key1")
	reader1.Close()
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record1 == nil {
		t.Fatal("expected record1, got nil")
	}

	// Now delete the key
	block2 := blocks.Block{
		Number: 2,
		Transactions: []blocks.Transaction{
			{
				ID:     "tx2",
				Number: 0,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: "ns1",
						RWS: blocks.ReadWriteSet{
							Writes: []blocks.KVWrite{
								{
									Key:      "key1",
									IsDelete: true,
								},
							},
						},
					},
				},
			},
		},
	}

	err = kvs.Handle(ctx, block2)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Verify key is deleted
	reader2, err := kvs.NewSnapshot(2)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader2.Close()
	record2, err := reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record2 != nil {
		t.Error("key1 should be deleted")
	}
}

// TestHandleEmptyBlock tests handling a block with no transactions
func TestHandleEmptyBlock(t *testing.T) {
	kvs := NewLightKVS(1)
	ctx := context.Background()

	block := blocks.Block{
		Number:       1,
		Transactions: []blocks.Transaction{},
	}

	err := kvs.Handle(ctx, block)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Block number should not change for empty blocks
	snapshot := kvs.Current.Load()
	if snapshot.BlockNumber != 0 {
		t.Errorf("expected block number 0, got %d", snapshot.BlockNumber)
	}
}

// TestBlockNumber tests the BlockNumber method
func TestBlockNumber(t *testing.T) {
	kvs := NewLightKVS(1)
	ctx := context.Background()

	// Initial block number should be 0
	blockNum, err := kvs.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber failed: %v", err)
	}
	if blockNum != 0 {
		t.Errorf("expected block number 0, got %d", blockNum)
	}

	// Update with block 5
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 5,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Block number should be 5
	blockNum, err = kvs.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber failed: %v", err)
	}
	if blockNum != 5 {
		t.Errorf("expected block number 5, got %d", blockNum)
	}
}

// TestClose tests the Close method
func TestClose(t *testing.T) {
	kvs := NewLightKVS(1)

	err := kvs.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestGetMethod tests the Get method on LightKVS
func TestGetMethod(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add data
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Test Get method
	record, err := kvs.Get("ns1", "key1", 0)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if string(record.Value) != "value1" {
		t.Errorf("expected 'value1', got '%s'", string(record.Value))
	}
}

// TestTruncateValue tests the truncateValue helper function
func TestTruncateValue(t *testing.T) {
	tests := []struct {
		name     string
		value    []byte
		maxLen   int
		expected string
	}{
		{
			name:     "nil value",
			value:    nil,
			maxLen:   10,
			expected: "<nil>",
		},
		{
			name:     "short value",
			value:    []byte{0x01, 0x02, 0x03},
			maxLen:   10,
			expected: "010203",
		},
		{
			name:     "long value",
			value:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b},
			maxLen:   5,
			expected: "0102030405...",
		},
		{
			name:     "exact length",
			value:    []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			maxLen:   5,
			expected: "0102030405",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateValue(tt.value, tt.maxLen)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestKeyValueVersionToLogUpdate tests the toLogUpdate method
func TestKeyValueVersionToLogUpdate(t *testing.T) {
	kvv := KeyValueVersion{
		Key:      "test:key",
		Value:    []byte{0x01, 0x02, 0x03},
		BlockNum: 10,
		TxNum:    5,
		TxID:     "tx123",
		IsDelete: false,
	}

	logUpdate := kvv.toLogUpdate()

	if logUpdate.Key != "test:key" {
		t.Errorf("expected key 'test:key', got '%s'", logUpdate.Key)
	}
	if logUpdate.Value != "010203" {
		t.Errorf("expected value '010203', got '%s'", logUpdate.Value)
	}
	if logUpdate.BlockNum != 10 {
		t.Errorf("expected block num 10, got %d", logUpdate.BlockNum)
	}
	if logUpdate.TxNum != 5 {
		t.Errorf("expected tx num 5, got %d", logUpdate.TxNum)
	}
	if logUpdate.TxID != "tx123" {
		t.Errorf("expected tx id 'tx123', got '%s'", logUpdate.TxID)
	}
	if logUpdate.IsDelete != false {
		t.Errorf("expected is_delete false, got %v", logUpdate.IsDelete)
	}
}

// TestMultipleNamespaces tests handling multiple namespaces
func TestMultipleNamespaces(t *testing.T) {
	kvs := NewLightKVS(1)

	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
		{
			Key:      "ns2:key1",
			Value:    []byte("value2"),
			BlockNum: 1,
			TxNum:    1,
			TxID:     "tx2",
			IsDelete: false,
		},
		{
			Key:      "ns1:key2",
			Value:    []byte("value3"),
			BlockNum: 1,
			TxNum:    2,
			TxID:     "tx3",
			IsDelete: false,
		},
	}

	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	reader, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot failed: %v", err)
	}
	defer reader.Close()

	// Test ns1:key1
	record1, err := reader.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record1 == nil || string(record1.Value) != "value1" {
		t.Error("ns1:key1 mismatch")
	}

	// Test ns2:key1
	record2, err := reader.Get("ns2", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record2 == nil || string(record2.Value) != "value2" {
		t.Error("ns2:key1 mismatch")
	}

	// Test ns1:key2
	record3, err := reader.Get("ns1", "key2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if record3 == nil || string(record3.Value) != "value3" {
		t.Error("ns1:key2 mismatch")
	}
}

// TestStructuralSharing tests that unchanged values are shared between snapshots
func TestStructuralSharing(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add initial data
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
		{
			Key:      "ns1:key2",
			Value:    []byte("value2"),
			BlockNum: 1,
			TxNum:    1,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	snapshot1 := kvs.Current.Load()
	ptr1 := snapshot1.Data["ns1:key1"]

	// Update only key2
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key2",
			Value:    []byte("value2-updated"),
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx3",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	snapshot2 := kvs.Current.Load()
	ptr2 := snapshot2.Data["ns1:key1"]

	// key1 should share the same pointer (structural sharing)
	if ptr1 != ptr2 {
		t.Error("expected structural sharing for unchanged key1")
	}

	// key2 should have a different pointer
	ptr1_key2 := snapshot1.Data["ns1:key2"]
	ptr2_key2 := snapshot2.Data["ns1:key2"]
	if ptr1_key2 == ptr2_key2 {
		t.Error("expected different pointer for updated key2")
	}
}

// TestConcurrentReadersWithUpdates tests readers during concurrent updates
func TestConcurrentReadersWithUpdates(t *testing.T) {
	kvs := NewLightKVS(1)

	// Add initial data
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var wg sync.WaitGroup
	numReaders := 50
	numUpdates := 50

	// Start readers
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()
			// Request block 0 (current snapshot) to get whatever state exists at this moment
			reader, err := kvs.NewSnapshot(0)
			if err != nil {
				t.Errorf("NewSnapshot failed: %v", err)
				return
			}
			defer reader.Close()

			// Each reader should see a consistent snapshot
			record, err := reader.Get("ns1", "key1")
			if err != nil {
				t.Errorf("Reader %d: Get failed: %v", id, err)
				return
			}
			if record == nil {
				t.Errorf("Reader %d: expected record, got nil", id)
				return
			}
			// Value should be one of the valid values
			val := string(record.Value)
			if val != "value1" && val != "value2" && val != "value3" {
				t.Errorf("Reader %d: unexpected value '%s'", id, val)
			}
		}(i)
	}

	// Perform updates concurrently (single writer, but testing atomicity)
	for i := 0; i < numUpdates; i++ {
		updates := []KeyValueVersion{
			{
				Key:      "ns1:key1",
				Value:    []byte("value2"),
				BlockNum: uint64(i + 2),
				TxNum:    0,
				TxID:     "tx" + string(rune(i+2)),
				IsDelete: false,
			},
		}
		err := kvs.Update(updates)
		if err != nil {
			t.Fatalf("Update %d failed: %v", i, err)
		}
	}

	wg.Wait()
}

// TestSnapshotByBlockNumber tests getting snapshots by specific block numbers
func TestSnapshotByBlockNumber(t *testing.T) {
	kvs := NewLightKVS(2)

	// Add data at block 1
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value_block1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update block 1 failed: %v", err)
	}

	// Add data at block 2
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value_block2"),
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update block 2 failed: %v", err)
	}

	// Test getting current snapshot (block 0 means current)
	readerCurrent, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot(0) failed: %v", err)
	}
	defer readerCurrent.Close()

	record, err := readerCurrent.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block2" {
		t.Errorf("Expected current snapshot to have value_block2, got %s", string(record.Value))
	}

	// Test getting snapshot by specific block number (current block)
	reader2, err := kvs.NewSnapshot(2)
	if err != nil {
		t.Fatalf("NewSnapshot(2) failed: %v", err)
	}
	defer reader2.Close()

	record, err = reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block2" {
		t.Errorf("Expected block 2 snapshot to have value_block2, got %s", string(record.Value))
	}

	// Test getting snapshot from history (block 1 should still be in history)
	reader1, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot(1) failed: %v", err)
	}
	defer reader1.Close()

	record, err = reader1.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block1" {
		t.Errorf("Expected block 1 snapshot to have value_block1, got %s", string(record.Value))
	}
}

// TestSnapshotHistoryEviction tests that old snapshots are evicted from history
func TestSnapshotHistoryEviction(t *testing.T) {
	kvs := NewLightKVS(2)

	// Create snapshots for blocks 1, 2, 3, 4
	// History size is 2, so blocks 1 and 2 should eventually be evicted
	for i := 1; i <= 4; i++ {
		updates := []KeyValueVersion{
			{
				Key:      "ns1:key1",
				Value:    []byte(fmt.Sprintf("value_block%d", i)),
				BlockNum: uint64(i),
				TxNum:    0,
				TxID:     fmt.Sprintf("tx%d", i),
				IsDelete: false,
			},
		}
		err := kvs.Update(updates)
		if err != nil {
			t.Fatalf("Update block %d failed: %v", i, err)
		}
	}

	// Current snapshot should be block 4
	readerCurrent, err := kvs.NewSnapshot(4)
	if err != nil {
		t.Fatalf("NewSnapshot(4) failed: %v", err)
	}
	defer readerCurrent.Close()

	record, err := readerCurrent.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block4" {
		t.Errorf("Expected block 4 snapshot to have value_block4, got %s", string(record.Value))
	}

	// Block 3 should be in history (most recent old snapshot)
	reader3, err := kvs.NewSnapshot(3)
	if err != nil {
		t.Fatalf("NewSnapshot(3) failed: %v", err)
	}
	defer reader3.Close()

	record, err = reader3.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block3" {
		t.Errorf("Expected block 3 snapshot to have value_block3, got %s", string(record.Value))
	}

	// Block 2 should be in history (second most recent old snapshot)
	reader2, err := kvs.NewSnapshot(2)
	if err != nil {
		t.Fatalf("NewSnapshot(2) failed: %v", err)
	}
	defer reader2.Close()

	record, err = reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record.Value) != "value_block2" {
		t.Errorf("Expected block 2 snapshot to have value_block2, got %s", string(record.Value))
	}

	// Block 1 should have been evicted (history only keeps 2 snapshots)
	_, err = kvs.NewSnapshot(1)
	if err == nil {
		t.Error("Expected error when requesting evicted block 1, got nil")
	}
	expectedErr := "snapshot not found for block number 1"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

// TestSnapshotNonExistentBlock tests error handling for non-existent block numbers
func TestSnapshotNonExistentBlock(t *testing.T) {
	kvs := NewLightKVS(2)

	// Add data at block 1
	updates := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Try to get snapshot for block 99 (higher than current, should return current)
	reader99, err := kvs.NewSnapshot(99)
	if err != nil {
		t.Fatalf("NewSnapshot(99) should not fail when requesting future block: %v", err)
	}
	defer reader99.Close()

	// Should get the current snapshot (block 1)
	record99, err := reader99.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(record99.Value) != "value1" {
		t.Errorf("Expected block 99 request to return current value 'value1', got %s", string(record99.Value))
	}

	// Block 0 (current) should always work
	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot(0) should not fail: %v", err)
	}
	defer reader.Close()
}

// TestSnapshotIsolationAcrossBlocks tests that snapshots from different blocks are isolated
func TestSnapshotIsolationAcrossBlocks(t *testing.T) {
	kvs := NewLightKVS(2)

	// Create block 1 with initial value
	updates1 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value1"),
			BlockNum: 1,
			TxNum:    0,
			TxID:     "tx1",
			IsDelete: false,
		},
	}
	err := kvs.Update(updates1)
	if err != nil {
		t.Fatalf("Update block 1 failed: %v", err)
	}

	// Get snapshot for block 1
	reader1, err := kvs.NewSnapshot(1)
	if err != nil {
		t.Fatalf("NewSnapshot(1) failed: %v", err)
	}
	defer reader1.Close()

	// Create block 2 with updated value
	updates2 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value2"),
			BlockNum: 2,
			TxNum:    0,
			TxID:     "tx2",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates2)
	if err != nil {
		t.Fatalf("Update block 2 failed: %v", err)
	}

	// Get snapshot for block 2
	reader2, err := kvs.NewSnapshot(2)
	if err != nil {
		t.Fatalf("NewSnapshot(2) failed: %v", err)
	}
	defer reader2.Close()

	// Verify reader1 still sees block 1 value
	record1, err := reader1.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get from reader1 failed: %v", err)
	}
	if string(record1.Value) != "value1" {
		t.Errorf("Reader1 should see value1, got %s", string(record1.Value))
	}

	// Verify reader2 sees block 2 value
	record2, err := reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get from reader2 failed: %v", err)
	}
	if string(record2.Value) != "value2" {
		t.Errorf("Reader2 should see value2, got %s", string(record2.Value))
	}

	// Create block 3 with another update
	updates3 := []KeyValueVersion{
		{
			Key:      "ns1:key1",
			Value:    []byte("value3"),
			BlockNum: 3,
			TxNum:    0,
			TxID:     "tx3",
			IsDelete: false,
		},
	}
	err = kvs.Update(updates3)
	if err != nil {
		t.Fatalf("Update block 3 failed: %v", err)
	}

	// Verify reader1 and reader2 still see their original values
	record1, err = reader1.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get from reader1 failed: %v", err)
	}
	if string(record1.Value) != "value1" {
		t.Errorf("Reader1 should still see value1, got %s", string(record1.Value))
	}

	record2, err = reader2.Get("ns1", "key1")
	if err != nil {
		t.Fatalf("Get from reader2 failed: %v", err)
	}
	if string(record2.Value) != "value2" {
		t.Errorf("Reader2 should still see value2, got %s", string(record2.Value))
	}
}
