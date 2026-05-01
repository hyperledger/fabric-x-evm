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

func sampleLedgerDerivedBlock(blockNum uint64) domain.Block {
	blockHash := makeHash(byte(blockNum))
	txHash := makeHash(byte(0x40 + blockNum))

	return domain.Block{
		BlockNumber: blockNum,
		BlockHash:   blockHash,
		ParentHash:  makeHash(byte(blockNum - 1)),
		StateRoot:   makeHash(byte(0x80 + blockNum)),
		Timestamp:   int64(1000 + blockNum),
		Transactions: []domain.Transaction{{
			TxHash:         txHash,
			BlockHash:      blockHash,
			BlockNumber:    blockNum,
			TxIndex:        0,
			RawTx:          []byte{0x01, byte(blockNum)},
			FromAddress:    makeAddress(byte(0x10 + blockNum)),
			ToAddress:      makeAddress(byte(0x20 + blockNum)),
			Status:         1,
			FabricTxID:     fmt.Sprintf("fabric-tx-%d", blockNum),
			FabricTxStatus: 0,
			Logs: []domain.Log{{
				BlockNumber: blockNum,
				BlockHash:   blockHash,
				TxHash:      txHash,
				TxIndex:     0,
				LogIndex:    0,
				Address:     makeAddress(byte(0x30 + blockNum)),
				Topics:      [][]byte{makeHash(byte(0x50 + blockNum))},
				Data:        []byte{byte(blockNum)},
			}},
		}},
	}
}

func countTableRows(t *testing.T, store *Store, table string) int {
	t.Helper()

	var count int
	err := store.db.QueryRowContext(t.Context(), fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestInsertBlock_ReprocessingSameBlockDoesNotDuplicateRows(t *testing.T) {
	store := setupTestDB(t)
	block := sampleLedgerDerivedBlock(7)

	require.NoError(t, store.InsertBlock(t.Context(), block))
	require.NoError(t, store.InsertBlock(t.Context(), block))

	require.Equal(t, 1, countTableRows(t, store, "blocks"))
	require.Equal(t, 1, countTableRows(t, store, "transactions"))
	require.Equal(t, 1, countTableRows(t, store, "logs"))

	latest, err := store.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, block.BlockHash, latest.BlockHash)
	require.Len(t, latest.Transactions, 1)
	require.Equal(t, block.Transactions[0].TxHash, latest.Transactions[0].TxHash)

	logs, err := store.GetLogs(t.Context(), domain.LogFilter{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, block.Transactions[0].Logs[0].TxHash, logs[0].TxHash)
}

func TestInsertBlock_RollbackLeavesNoPartialRowsAndRetrySucceeds(t *testing.T) {
	store := setupTestDB(t)
	block := sampleLedgerDerivedBlock(9)
	block.Transactions[0].FromAddress = []byte{0x99} // violates the 20-byte address check

	err := store.InsertBlock(t.Context(), block)
	require.Error(t, err)

	require.Equal(t, 0, countTableRows(t, store, "blocks"))
	require.Equal(t, 0, countTableRows(t, store, "transactions"))
	require.Equal(t, 0, countTableRows(t, store, "logs"))

	block.Transactions[0].FromAddress = makeAddress(0x19)
	require.NoError(t, store.InsertBlock(t.Context(), block))

	require.Equal(t, 1, countTableRows(t, store, "blocks"))
	require.Equal(t, 1, countTableRows(t, store, "transactions"))
	require.Equal(t, 1, countTableRows(t, store, "logs"))
}
