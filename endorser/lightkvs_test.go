/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"
	"sync"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// TestNewLightKVS tests the creation of a new LightKVS instance
func TestNewLightKVS(t *testing.T) {
	kvs := NewLightKVS()
	if kvs == nil {
		t.Fatal("NewLightKVS returned nil")
	}

	// Verify initial snapshot exists and is empty
	snapshot := kvs.current.Load()
	if snapshot == nil {
		t.Fatal("initial snapshot is nil")
	}
	if snapshot.blockNumber != 0 {
		t.Errorf("expected initial block number 0, got %d", snapshot.blockNumber)
	}
	if len(snapshot.data) != 0 {
		t.Errorf("expected empty initial data, got %d entries", len(snapshot.data))
	}
}

// TestNewSnapshot tests creating a new snapshot reader
func TestNewSnapshot(t *testing.T) {
	kvs := NewLightKVS()
	reader := kvs.NewSnapshot()
	if reader == nil {
		t.Fatal("NewSnapshot returned nil")
	}

	// Verify reader is of correct type
	r, ok := reader.(*Reader)
	if !ok {
		t.Fatal("NewSnapshot did not return a *Reader")
	}
	if r.snapshot == nil {
		t.Error("reader snapshot is nil")
	}
	if r.kvs != kvs {
		t.Error("reader kvs reference is incorrect")
	}
}

// TestReaderGet tests reading values from a snapshot
func TestReaderGet(t *testing.T) {
	kvs := NewLightKVS()

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
	reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()

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

	reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()
	reader := kvs.NewSnapshot().(*Reader)

	// Close the reader
	err := reader.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify snapshot is nil after close
	if reader.snapshot != nil {
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
	kvs := NewLightKVS()

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
	snapshot := kvs.current.Load()
	if snapshot.blockNumber != 1 {
		t.Errorf("expected block number 1, got %d", snapshot.blockNumber)
	}
	if len(snapshot.data) != 2 {
		t.Errorf("expected 2 entries, got %d", len(snapshot.data))
	}

	// Verify values
	vv1 := snapshot.data["ns1:key1"]
	if vv1 == nil {
		t.Fatal("key1 not found")
	}
	if string(vv1.Value) != "value1" {
		t.Errorf("expected 'value1', got '%s'", string(vv1.Value))
	}
	if vv1.Version != 0 {
		t.Errorf("expected version 0, got %d", vv1.Version)
	}

	vv2 := snapshot.data["ns1:key2"]
	if vv2 == nil {
		t.Fatal("key2 not found")
	}
	if string(vv2.Value) != "value2" {
		t.Errorf("expected 'value2', got '%s'", string(vv2.Value))
	}
}

// TestUpdateVersionIncrement tests that versions increment correctly
func TestUpdateVersionIncrement(t *testing.T) {
	kvs := NewLightKVS()

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

	snapshot := kvs.current.Load()
	if snapshot.data["ns1:key1"].Version != 0 {
		t.Errorf("expected version 0, got %d", snapshot.data["ns1:key1"].Version)
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

	snapshot = kvs.current.Load()
	if snapshot.data["ns1:key1"].Version != 1 {
		t.Errorf("expected version 1, got %d", snapshot.data["ns1:key1"].Version)
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

	snapshot = kvs.current.Load()
	if snapshot.data["ns1:key1"].Version != 2 {
		t.Errorf("expected version 2, got %d", snapshot.data["ns1:key1"].Version)
	}
}

// TestUpdateDelete tests deleting keys
func TestUpdateDelete(t *testing.T) {
	kvs := NewLightKVS()

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
	snapshot := kvs.current.Load()
	if _, ok := snapshot.data["ns1:key1"]; !ok {
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
	snapshot = kvs.current.Load()
	if _, ok := snapshot.data["ns1:key1"]; ok {
		t.Error("key1 should be deleted")
	}
}

// TestUpdateEmptyBatch tests updating with an empty batch
func TestUpdateEmptyBatch(t *testing.T) {
	kvs := NewLightKVS()

	err := kvs.Update([]KeyValueVersion{})
	if err != nil {
		t.Fatalf("Update with empty batch failed: %v", err)
	}

	snapshot := kvs.current.Load()
	if snapshot.blockNumber != 0 {
		t.Errorf("expected block number 0, got %d", snapshot.blockNumber)
	}
}

// TestSnapshotIsolation tests that readers see a consistent snapshot
func TestSnapshotIsolation(t *testing.T) {
	kvs := NewLightKVS()

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
	reader1 := kvs.NewSnapshot().(*Reader)
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
	reader2 := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()

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
			reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()
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
	reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()
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

	reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()
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
	reader1 := kvs.NewSnapshot().(*Reader)
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
	reader2 := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()
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
	snapshot := kvs.current.Load()
	if snapshot.blockNumber != 0 {
		t.Errorf("expected block number 0, got %d", snapshot.blockNumber)
	}
}

// TestBlockNumber tests the BlockNumber method
func TestBlockNumber(t *testing.T) {
	kvs := NewLightKVS()
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
	kvs := NewLightKVS()

	err := kvs.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestGetMethod tests the Get method on LightKVS
func TestGetMethod(t *testing.T) {
	kvs := NewLightKVS()

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
	kvs := NewLightKVS()

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

	reader := kvs.NewSnapshot().(*Reader)
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
	kvs := NewLightKVS()

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

	snapshot1 := kvs.current.Load()
	ptr1 := snapshot1.data["ns1:key1"]

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

	snapshot2 := kvs.current.Load()
	ptr2 := snapshot2.data["ns1:key1"]

	// key1 should share the same pointer (structural sharing)
	if ptr1 != ptr2 {
		t.Error("expected structural sharing for unchanged key1")
	}

	// key2 should have a different pointer
	ptr1_key2 := snapshot1.data["ns1:key2"]
	ptr2_key2 := snapshot2.data["ns1:key2"]
	if ptr1_key2 == ptr2_key2 {
		t.Error("expected different pointer for updated key2")
	}
}

// TestConcurrentReadersWithUpdates tests readers during concurrent updates
func TestConcurrentReadersWithUpdates(t *testing.T) {
	kvs := NewLightKVS()

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
			reader := kvs.NewSnapshot().(*Reader)
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
