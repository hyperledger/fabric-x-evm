/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/require"
)

type storeSnapshot struct {
	Latest domain.Block
	Blocks []domain.Block
	Txs    []domain.Transaction
	Logs   []domain.Log
}

func ledgerDerivedBlocks() []domain.Block {
	block1Hash := makeHash(0xA1)
	block2Hash := makeHash(0xA2)

	tx1Hash := makeHash(0x11)
	tx2Hash := makeHash(0x12)
	tx3Hash := makeHash(0x13)

	return []domain.Block{
		{
			BlockNumber: 1,
			BlockHash:   block1Hash,
			ParentHash:  makeHash(0x00),
			StateRoot:   makeHash(0x31),
			Timestamp:   1001,
			Transactions: []domain.Transaction{
				{
					TxHash:          tx1Hash,
					BlockHash:       block1Hash,
					BlockNumber:     1,
					TxIndex:         0,
					RawTx:           []byte{0x01, 0xA1},
					FromAddress:     makeAddress(0x21),
					ToAddress:       makeAddress(0x31),
					Status:          1,
					FabricTxID:      "fabric-tx-1",
					FabricTxStatus:  0,
					ContractAddress: nil,
					Logs: []domain.Log{
						{
							BlockNumber: 1,
							BlockHash:   block1Hash,
							TxHash:      tx1Hash,
							TxIndex:     0,
							LogIndex:    0,
							Address:     makeAddress(0x41),
							Topics:      [][]byte{makeHash(0x51)},
							Data:        []byte{0x01},
						},
					},
				},
				{
					TxHash:         tx2Hash,
					BlockHash:      block1Hash,
					BlockNumber:    1,
					TxIndex:        1,
					RawTx:          []byte{0x01, 0xA2},
					FromAddress:    makeAddress(0x22),
					ToAddress:      makeAddress(0x32),
					Status:         0,
					FabricTxID:     "fabric-tx-2",
					FabricTxStatus: 11,
				},
			},
		},
		{
			BlockNumber: 2,
			BlockHash:   block2Hash,
			ParentHash:  block1Hash,
			StateRoot:   makeHash(0x32),
			Timestamp:   1002,
			Transactions: []domain.Transaction{
				{
					TxHash:         tx3Hash,
					BlockHash:      block2Hash,
					BlockNumber:    2,
					TxIndex:        0,
					RawTx:          []byte{0x01, 0xA3},
					FromAddress:    makeAddress(0x23),
					ToAddress:      makeAddress(0x33),
					Status:         1,
					FabricTxID:     "fabric-tx-3",
					FabricTxStatus: 0,
					Logs: []domain.Log{
						{
							BlockNumber: 2,
							BlockHash:   block2Hash,
							TxHash:      tx3Hash,
							TxIndex:     0,
							LogIndex:    0,
							Address:     makeAddress(0x43),
							Topics:      [][]byte{makeHash(0x53), makeHash(0x63)},
							Data:        []byte{0x03, 0x04},
						},
					},
				},
			},
		},
	}
}

func tableColumns(t *testing.T, store *Store, table string) []string {
	t.Helper()

	rows, err := store.db.QueryContext(t.Context(), fmt.Sprintf("PRAGMA table_info(%s)", table))
	require.NoError(t, err)
	defer rows.Close()

	var (
		cid        int
		name       string
		dataType   string
		notNull    int
		defaultV   any
		primaryKey int
	)
	cols := []string{}
	for rows.Next() {
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey)
		require.NoError(t, err)
		cols = append(cols, name)
	}
	require.NoError(t, rows.Err())
	return cols
}

func userTables(t *testing.T, store *Store) []string {
	t.Helper()

	rows, err := store.db.QueryContext(t.Context(), `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	require.NoError(t, rows.Err())
	return tables
}

func captureStoreSnapshot(t *testing.T, store *Store, blocks []domain.Block) storeSnapshot {
	t.Helper()

	latest, err := store.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)

	snapshot := storeSnapshot{
		Latest: *latest,
		Blocks: make([]domain.Block, 0, len(blocks)),
		Txs:    make([]domain.Transaction, 0),
	}

	for _, block := range blocks {
		got, err := store.GetBlockByNumber(t.Context(), block.BlockNumber, false)
		require.NoError(t, err)
		require.NotNil(t, got)
		snapshot.Blocks = append(snapshot.Blocks, *got)

		for _, tx := range block.Transactions {
			gotTx, err := store.GetTransactionByHash(t.Context(), tx.TxHash)
			require.NoError(t, err)
			require.NotNil(t, gotTx)
			snapshot.Txs = append(snapshot.Txs, *gotTx)
		}
	}

	logs, err := store.GetLogs(t.Context(), domain.LogFilter{})
	require.NoError(t, err)
	snapshot.Logs = logs

	return snapshot
}

func TestSchemaContainsOnlyLedgerDerivedTables(t *testing.T) {
	store := setupTestDB(t)

	require.ElementsMatch(t, []string{"blocks", "logs", "transactions"}, userTables(t, store))

	expectedColumns := map[string][]string{
		"blocks": {
			"block_number",
			"block_hash",
			"parent_hash",
			"state_root",
			"timestamp",
			"extra_data",
		},
		"transactions": {
			"tx_hash",
			"block_hash",
			"block_number",
			"tx_index",
			"raw_tx",
			"from_address",
			"to_address",
			"contract_address",
			"status",
			"fabric_tx_id",
			"fabric_tx_status",
		},
		"logs": {
			"block_number",
			"block_hash",
			"tx_hash",
			"tx_index",
			"log_index",
			"address",
			"topic0",
			"topic1",
			"topic2",
			"topic3",
			"data",
		},
	}

	for table, want := range expectedColumns {
		got := tableColumns(t, store, table)
		require.ElementsMatchf(t, want, got, "%s columns mismatch: want %v, got %v", table, want, got)
	}
}

func TestStoreRebuildFromLedgerDerivedBlocksReproducesQueryableData(t *testing.T) {
	blocks := ledgerDerivedBlocks()

	original := setupTestDB(t)
	for _, block := range blocks {
		require.NoError(t, original.InsertBlock(t.Context(), block))
	}
	originalSnapshot := captureStoreSnapshot(t, original, blocks)

	rebuilt := setupTestDB(t)
	for _, block := range blocks {
		require.NoError(t, rebuilt.InsertBlock(t.Context(), block))
	}
	rebuiltSnapshot := captureStoreSnapshot(t, rebuilt, blocks)

	require.Equal(t, originalSnapshot, rebuiltSnapshot)
}
