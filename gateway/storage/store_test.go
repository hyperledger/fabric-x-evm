/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewStore(db)
	if err := store.Init(); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	return store
}

func insertTestBlock(t *testing.T, store *Store, blockNum uint64, blockHash []byte) {
	t.Helper()
	err := store.InsertBlock(t.Context(), domain.Block{
		BlockNumber: blockNum,
		BlockHash:   blockHash,
		ParentHash:  make([]byte, 32),
		StateRoot:   make([]byte, 32),
		Timestamp:   1000,
	})
	if err != nil {
		t.Fatalf("failed to insert block: %v", err)
	}
}

func insertTestLog(t *testing.T, store *Store, blockNum uint64, txHash, address []byte, logIndex int64, topics [][]byte) {
	t.Helper()

	// Pad topics to 4 elements
	padded := make([][]byte, 4)
	copy(padded, topics)

	_, err := store.db.ExecContext(t.Context(), `
		INSERT INTO logs (block_number, tx_hash, tx_index, log_index, address, topic0, topic1, topic2, topic3, data)
		VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		blockNum, txHash, logIndex, address, padded[0], padded[1], padded[2], padded[3], []byte{})
	if err != nil {
		t.Fatalf("failed to insert log: %v", err)
	}
}

func TestGetLogs_BlockRange(t *testing.T) {
	store := setupTestDB(t)

	// Insert blocks and logs
	for i := uint64(1); i <= 5; i++ {
		blockHash := make([]byte, 32)
		blockHash[0] = byte(i)
		insertTestBlock(t, store, i, blockHash)

		txHash := make([]byte, 32)
		txHash[0] = byte(i)
		addr := make([]byte, 20)
		addr[0] = byte(i)
		insertTestLog(t, store, i, txHash, addr, 0, nil)
	}

	tests := []struct {
		name      string
		filter    domain.LogFilter
		wantCount int
	}{
		{
			name:      "no filter returns all",
			filter:    domain.LogFilter{},
			wantCount: 5,
		},
		{
			name:      "from block 3",
			filter:    domain.LogFilter{FromBlock: new(uint64(3))},
			wantCount: 3,
		},
		{
			name:      "to block 2",
			filter:    domain.LogFilter{ToBlock: new(uint64(2))},
			wantCount: 2,
		},
		{
			name:      "block range 2-4",
			filter:    domain.LogFilter{FromBlock: new(uint64(2)), ToBlock: new(uint64(4))},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := store.GetLogs(t.Context(), tt.filter)
			if err != nil {
				t.Fatalf("GetLogs error: %v", err)
			}
			if len(logs) != tt.wantCount {
				t.Errorf("got %d logs, want %d", len(logs), tt.wantCount)
			}
		})
	}
}

func TestGetLogs_BlockHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := make([]byte, 32)
	blockHash[0] = 0xAB
	insertTestBlock(t, store, 1, blockHash)

	txHash := make([]byte, 32)
	txHash[0] = 0x01
	addr := make([]byte, 20)
	insertTestLog(t, store, 1, txHash, addr, 0, nil)

	// Insert another block with different hash
	otherHash := make([]byte, 32)
	otherHash[0] = 0xCD
	insertTestBlock(t, store, 2, otherHash)

	txHash2 := make([]byte, 32)
	txHash2[0] = 0x02
	insertTestLog(t, store, 2, txHash2, addr, 0, nil)

	logs, err := store.GetLogs(t.Context(), domain.LogFilter{BlockHash: &blockHash})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("got %d logs, want 1", len(logs))
	}
	if logs[0].BlockNumber != 1 {
		t.Errorf("got block %d, want 1", logs[0].BlockNumber)
	}
}

func TestGetLogs_AddressFilter(t *testing.T) {
	store := setupTestDB(t)

	blockHash := make([]byte, 32)
	insertTestBlock(t, store, 1, blockHash)

	addr1 := make([]byte, 20)
	addr1[0] = 0x01
	addr2 := make([]byte, 20)
	addr2[0] = 0x02
	addr3 := make([]byte, 20)
	addr3[0] = 0x03

	txHash := make([]byte, 32)
	insertTestLog(t, store, 1, txHash, addr1, 0, nil)

	txHash[0] = 0x01
	insertTestLog(t, store, 1, txHash, addr2, 1, nil)

	txHash[0] = 0x02
	insertTestLog(t, store, 1, txHash, addr3, 2, nil)

	tests := []struct {
		name      string
		addresses [][]byte
		wantCount int
	}{
		{
			name:      "single address",
			addresses: [][]byte{addr1},
			wantCount: 1,
		},
		{
			name:      "two addresses (OR)",
			addresses: [][]byte{addr1, addr2},
			wantCount: 2,
		},
		{
			name:      "all addresses",
			addresses: [][]byte{addr1, addr2, addr3},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := store.GetLogs(t.Context(), domain.LogFilter{Addresses: tt.addresses})
			if err != nil {
				t.Fatalf("GetLogs error: %v", err)
			}
			if len(logs) != tt.wantCount {
				t.Errorf("got %d logs, want %d", len(logs), tt.wantCount)
			}
		})
	}
}

func TestGetLogs_TopicFilter(t *testing.T) {
	store := setupTestDB(t)

	blockHash := make([]byte, 32)
	insertTestBlock(t, store, 1, blockHash)

	addr := make([]byte, 20)

	topicA := make([]byte, 32)
	topicA[0] = 0xAA
	topicB := make([]byte, 32)
	topicB[0] = 0xBB
	topicC := make([]byte, 32)
	topicC[0] = 0xCC
	topicD := make([]byte, 32)
	topicD[0] = 0xDD

	// Log 1: topics [A, C]
	txHash1 := make([]byte, 32)
	txHash1[0] = 0x01
	insertTestLog(t, store, 1, txHash1, addr, 0, [][]byte{topicA, topicC})

	// Log 2: topics [A, D]
	txHash2 := make([]byte, 32)
	txHash2[0] = 0x02
	insertTestLog(t, store, 1, txHash2, addr, 1, [][]byte{topicA, topicD})

	// Log 3: topics [B, C]
	txHash3 := make([]byte, 32)
	txHash3[0] = 0x03
	insertTestLog(t, store, 1, txHash3, addr, 2, [][]byte{topicB, topicC})

	// Log 4: topics [B, D]
	txHash4 := make([]byte, 32)
	txHash4[0] = 0x04
	insertTestLog(t, store, 1, txHash4, addr, 3, [][]byte{topicB, topicD})

	tests := []struct {
		name      string
		topics    [][][]byte
		wantCount int
	}{
		{
			name:      "no topic filter",
			topics:    nil,
			wantCount: 4,
		},
		{
			name:      "{{A}} - topic0 = A",
			topics:    [][][]byte{{topicA}},
			wantCount: 2,
		},
		{
			name:      "{{B}} - topic0 = B",
			topics:    [][][]byte{{topicB}},
			wantCount: 2,
		},
		{
			name:      "{{}, {C}} - any topic0, topic1 = C",
			topics:    [][][]byte{{}, {topicC}},
			wantCount: 2,
		},
		{
			name:      "{{A}, {C}} - topic0 = A AND topic1 = C",
			topics:    [][][]byte{{topicA}, {topicC}},
			wantCount: 1,
		},
		{
			name:      "{{A, B}} - topic0 = A OR B",
			topics:    [][][]byte{{topicA, topicB}},
			wantCount: 4,
		},
		{
			name:      "{{A, B}, {C}} - (topic0 = A OR B) AND topic1 = C",
			topics:    [][][]byte{{topicA, topicB}, {topicC}},
			wantCount: 2,
		},
		{
			name:      "{{A}, {C, D}} - topic0 = A AND (topic1 = C OR D)",
			topics:    [][][]byte{{topicA}, {topicC, topicD}},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := store.GetLogs(t.Context(), domain.LogFilter{Topics: tt.topics})
			if err != nil {
				t.Fatalf("GetLogs error: %v", err)
			}
			if len(logs) != tt.wantCount {
				t.Errorf("got %d logs, want %d", len(logs), tt.wantCount)
			}
		})
	}
}

func TestGetLogs_CombinedFilters(t *testing.T) {
	store := setupTestDB(t)

	blockHash := make([]byte, 32)
	insertTestBlock(t, store, 1, blockHash)

	addr1 := make([]byte, 20)
	addr1[0] = 0x01
	addr2 := make([]byte, 20)
	addr2[0] = 0x02

	topicA := make([]byte, 32)
	topicA[0] = 0xAA
	topicB := make([]byte, 32)
	topicB[0] = 0xBB

	// Log 1: addr1, topic A
	txHash1 := make([]byte, 32)
	txHash1[0] = 0x01
	insertTestLog(t, store, 1, txHash1, addr1, 0, [][]byte{topicA})

	// Log 2: addr1, topic B
	txHash2 := make([]byte, 32)
	txHash2[0] = 0x02
	insertTestLog(t, store, 1, txHash2, addr1, 1, [][]byte{topicB})

	// Log 3: addr2, topic A
	txHash3 := make([]byte, 32)
	txHash3[0] = 0x03
	insertTestLog(t, store, 1, txHash3, addr2, 2, [][]byte{topicA})

	// Filter: addr1 AND topic A
	logs, err := store.GetLogs(t.Context(), domain.LogFilter{
		Addresses: [][]byte{addr1},
		Topics:    [][][]byte{{topicA}},
	})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("got %d logs, want 1", len(logs))
	}
}

func makeHash(prefix byte) []byte {
	h := make([]byte, 32)
	h[0] = prefix
	return h
}

func makeAddress(prefix byte) []byte {
	a := make([]byte, 20)
	a[0] = prefix
	return a
}

func insertTestTransaction(t *testing.T, store *Store, blockNum uint64, blockHash, txHash []byte, txIndex int64) {
	t.Helper()
	// Use txHash to create unique fabric_tx_id
	fabricTxID := fmt.Sprintf("fabric-tx-%x", txHash[:8])
	_, err := store.db.ExecContext(t.Context(), `
		INSERT INTO transactions (tx_hash, block_hash, block_number, tx_index, raw_tx, from_address, to_address, contract_address, status, fabric_tx_id, fabric_tx_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		txHash, blockHash, blockNum, txIndex, []byte{0x01, 0x02}, makeAddress(0x11), makeAddress(0x22), nil, 1, fabricTxID, 0)
	if err != nil {
		t.Fatalf("failed to insert transaction: %v", err)
	}
}

// Block retrieval tests

func TestBlockNumber(t *testing.T) {
	store := setupTestDB(t)

	// Empty database should return 0
	num, err := store.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber error: %v", err)
	}
	if num != 0 {
		t.Errorf("expected 0 for empty db, got %d", num)
	}

	// Insert blocks and check the number increases
	insertTestBlock(t, store, 1, makeHash(0x01))
	num, err = store.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber error: %v", err)
	}
	if num != 1 {
		t.Errorf("expected 1, got %d", num)
	}

	insertTestBlock(t, store, 5, makeHash(0x05))
	num, err = store.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber error: %v", err)
	}
	if num != 5 {
		t.Errorf("expected 5, got %d", num)
	}
}

func TestBlockNumberByHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0xBC)
	insertTestBlock(t, store, 77, blockHash)

	num, err := store.BlockNumberByHash(t.Context(), blockHash)
	if err != nil {
		t.Fatalf("BlockNumberByHash error: %v", err)
	}
	if num == nil {
		t.Fatal("expected block number, got nil")
	}
	if *num != 77 {
		t.Errorf("expected block number 77, got %d", *num)
	}

	missing, err := store.BlockNumberByHash(t.Context(), makeHash(0xFF))
	if err != nil {
		t.Fatalf("BlockNumberByHash (missing) error: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing hash, got %d", *missing)
	}
}

func TestGetBlockByNumber(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0xAB)
	insertTestBlock(t, store, 42, blockHash)

	// Add a transaction to the block
	txHash := makeHash(0xCD)
	insertTestTransaction(t, store, 42, blockHash, txHash, 0)

	// Test retrieval
	block, err := store.GetBlockByNumber(t.Context(), 42, false)
	if err != nil {
		t.Fatalf("GetBlockByNumber error: %v", err)
	}
	if block == nil {
		t.Fatal("expected block, got nil")
	}
	if block.BlockNumber != 42 {
		t.Errorf("expected block number 42, got %d", block.BlockNumber)
	}
	if len(block.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(block.Transactions))
	}

	// Test non-existent block
	block, err = store.GetBlockByNumber(t.Context(), 999, false)
	if err != nil {
		t.Fatalf("GetBlockByNumber error: %v", err)
	}
	if block != nil {
		t.Error("expected nil for non-existent block")
	}
}

func TestGetBlockByHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0xEF)
	insertTestBlock(t, store, 10, blockHash)

	// Add a transaction to the block
	txHash := makeHash(0x99)
	insertTestTransaction(t, store, 10, blockHash, txHash, 0)

	// Test retrieval
	block, err := store.GetBlockByHash(t.Context(), blockHash, false)
	if err != nil {
		t.Fatalf("GetBlockByHash error: %v", err)
	}
	if block == nil {
		t.Fatal("expected block, got nil")
	}
	if block.BlockNumber != 10 {
		t.Errorf("expected block number 10, got %d", block.BlockNumber)
	}
	if len(block.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(block.Transactions))
	}

	// Test non-existent block
	nonExistentHash := makeHash(0xFF)
	block, err = store.GetBlockByHash(t.Context(), nonExistentHash, false)
	if err != nil {
		t.Fatalf("GetBlockByHash error: %v", err)
	}
	if block != nil {
		t.Error("expected nil for non-existent block")
	}
}

func TestLatestBlock(t *testing.T) {
	store := setupTestDB(t)

	// Empty database should return nil
	block, err := store.LatestBlock(t.Context(), false)
	if err != nil {
		t.Fatalf("LatestBlock error: %v", err)
	}
	if block != nil {
		t.Error("expected nil for empty db")
	}

	// Insert blocks
	insertTestBlock(t, store, 1, makeHash(0x01))
	insertTestBlock(t, store, 3, makeHash(0x03))
	insertTestBlock(t, store, 2, makeHash(0x02))

	// Add transaction to block 3
	insertTestTransaction(t, store, 3, makeHash(0x03), makeHash(0xAA), 0)

	block, err = store.LatestBlock(t.Context(), false)
	if err != nil {
		t.Fatalf("LatestBlock error: %v", err)
	}
	if block == nil {
		t.Fatal("expected block, got nil")
	}
	if block.BlockNumber != 3 {
		t.Errorf("expected latest block 3, got %d", block.BlockNumber)
	}
	if len(block.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(block.Transactions))
	}
}

// Transaction retrieval tests

func TestGetTransactionByHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x01)
	insertTestBlock(t, store, 1, blockHash)

	txHash := makeHash(0xAA)
	insertTestTransaction(t, store, 1, blockHash, txHash, 0)

	// Test retrieval
	tx, err := store.GetTransactionByHash(t.Context(), txHash)
	if err != nil {
		t.Fatalf("GetTransactionByHash error: %v", err)
	}
	if tx == nil {
		t.Fatal("expected transaction, got nil")
	}
	if tx.BlockNumber != 1 {
		t.Errorf("expected block number 1, got %d", tx.BlockNumber)
	}

	// Test non-existent transaction
	tx, err = store.GetTransactionByHash(t.Context(), makeHash(0xFF))
	if err != nil {
		t.Fatalf("GetTransactionByHash error: %v", err)
	}
	if tx != nil {
		t.Error("expected nil for non-existent transaction")
	}
}

func TestGetTransactionByBlockHashAndIndex(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x02)
	insertTestBlock(t, store, 2, blockHash)

	// Insert multiple transactions
	insertTestTransaction(t, store, 2, blockHash, makeHash(0xA0), 0)
	insertTestTransaction(t, store, 2, blockHash, makeHash(0xA1), 1)
	insertTestTransaction(t, store, 2, blockHash, makeHash(0xA2), 2)

	// Test retrieval by index
	tx, err := store.GetTransactionByBlockHashAndIndex(t.Context(), blockHash, 1)
	if err != nil {
		t.Fatalf("GetTransactionByBlockHashAndIndex error: %v", err)
	}
	if tx == nil {
		t.Fatal("expected transaction, got nil")
	}
	if tx.TxIndex != 1 {
		t.Errorf("expected tx index 1, got %d", tx.TxIndex)
	}

	// Test non-existent index
	tx, err = store.GetTransactionByBlockHashAndIndex(t.Context(), blockHash, 99)
	if err != nil {
		t.Fatalf("GetTransactionByBlockHashAndIndex error: %v", err)
	}
	if tx != nil {
		t.Error("expected nil for non-existent index")
	}
}

func TestGetTransactionByBlockNumberAndIndex(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x03)
	insertTestBlock(t, store, 3, blockHash)

	// Insert multiple transactions
	insertTestTransaction(t, store, 3, blockHash, makeHash(0xB0), 0)
	insertTestTransaction(t, store, 3, blockHash, makeHash(0xB1), 1)

	// Test retrieval by index
	tx, err := store.GetTransactionByBlockNumberAndIndex(t.Context(), 3, 0)
	if err != nil {
		t.Fatalf("GetTransactionByBlockNumberAndIndex error: %v", err)
	}
	if tx == nil {
		t.Fatal("expected transaction, got nil")
	}
	if tx.TxIndex != 0 {
		t.Errorf("expected tx index 0, got %d", tx.TxIndex)
	}

	// Test non-existent block/index
	tx, err = store.GetTransactionByBlockNumberAndIndex(t.Context(), 999, 0)
	if err != nil {
		t.Fatalf("GetTransactionByBlockNumberAndIndex error: %v", err)
	}
	if tx != nil {
		t.Error("expected nil for non-existent block")
	}
}

// Block transaction count tests

func TestGetBlockTxCountByHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x04)
	insertTestBlock(t, store, 4, blockHash)

	// Insert 3 transactions
	for i := range 3 {
		insertTestTransaction(t, store, 4, blockHash, makeHash(byte(0xC0+i)), int64(i))
	}

	count, err := store.GetBlockTxCountByHash(t.Context(), blockHash)
	if err != nil {
		t.Fatalf("GetBlockTxCountByHash error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 transactions, got %d", count)
	}

	// Empty block
	emptyBlockHash := makeHash(0x05)
	insertTestBlock(t, store, 5, emptyBlockHash)
	count, err = store.GetBlockTxCountByHash(t.Context(), emptyBlockHash)
	if err != nil {
		t.Fatalf("GetBlockTxCountByHash error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 transactions, got %d", count)
	}
}

func TestGetBlockTxCountByNumber(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x06)
	insertTestBlock(t, store, 6, blockHash)

	// Insert 2 transactions
	insertTestTransaction(t, store, 6, blockHash, makeHash(0xD0), 0)
	insertTestTransaction(t, store, 6, blockHash, makeHash(0xD1), 1)

	count, err := store.GetBlockTxCountByNumber(t.Context(), 6)
	if err != nil {
		t.Fatalf("GetBlockTxCountByNumber error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 transactions, got %d", count)
	}
}

// InsertBlock with transactions and logs

func TestInsertBlock_WithTransactionsAndLogs(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x10)
	txHash := makeHash(0x20)

	block := domain.Block{
		BlockNumber: 100,
		BlockHash:   blockHash,
		ParentHash:  makeHash(0x0F),
		StateRoot:   makeHash(0x01),
		Timestamp:   1234567890,
		Transactions: []domain.Transaction{
			{
				TxHash:         txHash,
				BlockHash:      blockHash,
				BlockNumber:    100,
				TxIndex:        0,
				RawTx:          []byte{0x01, 0x02, 0x03},
				FromAddress:    makeAddress(0x11),
				ToAddress:      makeAddress(0x22),
				Status:         1,
				FabricTxID:     "fabric-123",
				FabricTxStatus: 0,
				Logs: []domain.Log{
					{
						BlockNumber: 100,
						TxHash:      txHash,
						TxIndex:     0,
						LogIndex:    0,
						Address:     makeAddress(0x33),
						Topics:      [][]byte{makeHash(0xAA), makeHash(0xBB)},
						Data:        []byte{0xDE, 0xAD},
					},
					{
						BlockNumber: 100,
						TxHash:      txHash,
						TxIndex:     0,
						LogIndex:    1,
						Address:     makeAddress(0x33),
						Topics:      [][]byte{makeHash(0xCC)},
						Data:        []byte{0xBE, 0xEF},
					},
				},
			},
		},
	}

	err := store.InsertBlock(t.Context(), block)
	if err != nil {
		t.Fatalf("InsertBlock error: %v", err)
	}

	// Verify block was inserted
	retrievedBlock, err := store.GetBlockByNumber(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("GetBlockByNumber error: %v", err)
	}
	if retrievedBlock == nil {
		t.Fatal("expected block, got nil")
	}
	if retrievedBlock.Timestamp != 1234567890 {
		t.Errorf("expected timestamp 1234567890, got %d", retrievedBlock.Timestamp)
	}

	// Verify transaction was inserted
	if len(retrievedBlock.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(retrievedBlock.Transactions))
	}
	if retrievedBlock.Transactions[0].FabricTxID != "fabric-123" {
		t.Errorf("expected fabric tx id 'fabric-123', got %s", retrievedBlock.Transactions[0].FabricTxID)
	}

	// Verify logs were inserted
	retrievedLogs, err := store.GetLogs(t.Context(), domain.LogFilter{FromBlock: new(uint64(100)), ToBlock: new(uint64(100))})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if len(retrievedLogs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(retrievedLogs))
	}
}

// GetLogsByTxHash test

func TestGetLogsByTxHash(t *testing.T) {
	store := setupTestDB(t)

	blockHash := makeHash(0x07)
	insertTestBlock(t, store, 7, blockHash)

	txHash1 := makeHash(0xE1)
	txHash2 := makeHash(0xE2)

	// Insert logs for txHash1
	insertTestLog(t, store, 7, txHash1, makeAddress(0x01), 0, [][]byte{makeHash(0xAA)})
	insertTestLog(t, store, 7, txHash1, makeAddress(0x01), 1, [][]byte{makeHash(0xBB)})

	// Insert log for txHash2
	insertTestLog(t, store, 7, txHash2, makeAddress(0x02), 0, [][]byte{makeHash(0xCC)})

	// Get logs for txHash1
	logs, err := store.GetLogsByTxHash(t.Context(), txHash1)
	if err != nil {
		t.Fatalf("GetLogsByTxHash error: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs for txHash1, got %d", len(logs))
	}

	// Get logs for txHash2
	logs, err = store.GetLogsByTxHash(t.Context(), txHash2)
	if err != nil {
		t.Fatalf("GetLogsByTxHash error: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log for txHash2, got %d", len(logs))
	}

	// Get logs for non-existent tx
	logs, err = store.GetLogsByTxHash(t.Context(), makeHash(0xFF))
	if err != nil {
		t.Fatalf("GetLogsByTxHash error: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs for non-existent tx, got %d", len(logs))
	}
}
