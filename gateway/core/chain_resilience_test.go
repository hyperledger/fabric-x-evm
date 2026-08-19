/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"encoding/binary"
	"encoding/json"
	"math/big"
	"path/filepath"
	"testing"

	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	sdkstate "github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func resilienceHash(prefix byte) []byte {
	hash := make([]byte, 32)
	hash[0] = prefix
	return hash
}

func resilienceBalanceWrite(addr common.Address, bal int64) blocks.KVWrite {
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":bal",
		Value: big.NewInt(bal).Bytes(),
	}
}

func resilienceNonceWrite(addr common.Address, nonce uint64) blocks.KVWrite {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, nonce)
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":nonce",
		Value: buf,
	}
}

func resilienceEvents(t *testing.T, txID string, logs []sdkstate.Log) []byte {
	t.Helper()

	payload, err := json.Marshal(logs)
	require.NoError(t, err)

	event, err := fc.MarshalLogs(payload, "evmcc", txID)
	require.NoError(t, err)
	return event
}

func TestHandle_ReprocessingSameBlockKeepsIndexesAndTrieStable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	triePath := filepath.Join(t.TempDir(), "trie")

	chain, err := NewChain(dbPath, triePath, true)
	require.NoError(t, err)
	defer chain.Close()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	from := crypto.PubkeyToAddress(key.PublicKey)
	ethTx := createTestEthTx(t, key, to, big.NewInt(7))
	txBytes, err := ethTx.MarshalBinary()
	require.NoError(t, err)

	logs := []sdkstate.Log{{
		Address: to.Bytes(),
		Topics:  [][]byte{resilienceHash(0xAA)},
		Data:    []byte{0x01, 0x02},
	}}

	block := blocks.Block{
		Number:     1,
		Hash:       resilienceHash(0xA1),
		ParentHash: resilienceHash(0x00),
		Timestamp:  12345,
		Transactions: []blocks.Transaction{{
			ID:        "fabric-tx-1",
			Number:    0,
			Valid:     true,
			Status:    0,
			InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, txBytes},
			Events:    resilienceEvents(t, "fabric-tx-1", logs),
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: "evmcc",
				RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
					resilienceBalanceWrite(from, 93),
					resilienceNonceWrite(from, 1),
					resilienceBalanceWrite(to, 7),
				}},
			}},
		}},
	}

	require.NoError(t, chain.Handle(t.Context(), block))
	firstRoot := chain.ts.Root()

	require.NoError(t, chain.Handle(t.Context(), block))
	secondRoot := chain.ts.Root()

	require.Equal(t, firstRoot, secondRoot)

	latest, err := chain.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, uint64(1), latest.BlockNumber)
	require.Equal(t, block.Hash, latest.BlockHash)
	require.Equal(t, firstRoot.Bytes(), latest.StateRoot)
	require.Len(t, latest.Transactions, 1)
	require.Equal(t, ethTx.Hash().Bytes(), latest.Transactions[0].TxHash)

	txCount, err := chain.GetBlockTxCountByNumber(t.Context(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, txCount)

	logEntries, err := chain.GetLogs(t.Context(), domain.LogFilter{})
	require.NoError(t, err)
	require.Len(t, logEntries, 1)
	require.Equal(t, ethTx.Hash().Bytes(), logEntries[0].TxHash)
}

// TestHandle_PrevHashAdvancesOnlyAfterSuccessfulInsert locks the happy path:
// the published tip moves exactly once per durable block.
func TestHandle_PrevHashAdvancesOnlyAfterSuccessfulInsert(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	chain, err := NewChain(dbPath, "", false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close() })

	require.Equal(t, common.Hash{}, chain.prevHash)

	block1 := emptyResilienceBlock(1, 0x01, 1000)
	require.NoError(t, chain.Handle(t.Context(), block1))
	require.Equal(t, common.BytesToHash(block1.Hash), chain.prevHash)

	block2 := emptyResilienceBlock(2, 0x02, 2000)
	require.NoError(t, chain.Handle(t.Context(), block2))
	require.Equal(t, common.BytesToHash(block2.Hash), chain.prevHash)

	latest, err := chain.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, uint64(2), latest.BlockNumber)
	require.Equal(t, block1.Hash, latest.ParentHash)
}

// TestHandle_PrevHashNotAdvancedOnInsertFailure is the regression for #304
// without the trie (production withTrie=false path).
func TestHandle_PrevHashNotAdvancedOnInsertFailure(t *testing.T) {
	testPrevHashNotAdvancedOnInsertFailure(t, false)
}

// TestHandle_PrevHashNotAdvancedOnInsertFailure_WithTrie covers the same tip
// invariant when the MPT is enabled. Trie commit may still run before SQL
// (intentional; replay is idempotent). Only the published prevHash tip is gated
// on InsertBlock success.
func TestHandle_PrevHashNotAdvancedOnInsertFailure_WithTrie(t *testing.T) {
	testPrevHashNotAdvancedOnInsertFailure(t, true)
}

func emptyResilienceBlock(number uint64, hashPrefix byte, ts int64) blocks.Block {
	return blocks.Block{
		Number:     number,
		Hash:       resilienceHash(hashPrefix),
		ParentHash: resilienceHash(0x00),
		Timestamp:  ts,
	}
}

func testPrevHashNotAdvancedOnInsertFailure(t *testing.T, withTrie bool) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	triePath := ""
	if withTrie {
		triePath = filepath.Join(t.TempDir(), "trie")
	}

	chain, err := NewChain(dbPath, triePath, withTrie)
	require.NoError(t, err)
	// Close may return an error after we deliberately close the DB below.
	t.Cleanup(func() { _ = chain.Close() })

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	from := crypto.PubkeyToAddress(key.PublicKey)

	ethTx := createTestEthTx(t, key, to, big.NewInt(1))
	txBytes, err := ethTx.MarshalBinary()
	require.NoError(t, err)

	block1 := blocks.Block{
		Number:     1,
		Hash:       resilienceHash(0x01),
		ParentHash: resilienceHash(0x00),
		Timestamp:  1000,
		Transactions: []blocks.Transaction{{
			ID:        "fabric-tx-1",
			Number:    0,
			Valid:     true,
			InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, txBytes},
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: "evmcc",
				RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
					resilienceBalanceWrite(from, 99),
					resilienceNonceWrite(from, 1),
					resilienceBalanceWrite(to, 1),
				}},
			}},
		}},
	}
	require.NoError(t, chain.Handle(t.Context(), block1))
	prevHashAfterBlock1 := chain.prevHash
	require.Equal(t, common.BytesToHash(block1.Hash), prevHashAfterBlock1)

	// Force InsertBlock to fail after any trie work by closing the SQL handle.
	require.NoError(t, chain.db.Close())

	block2 := blocks.Block{
		Number:     2,
		Hash:       resilienceHash(0x02),
		ParentHash: resilienceHash(0x01),
		Timestamp:  2000,
		Transactions: []blocks.Transaction{{
			ID:        "fabric-tx-2",
			Number:    0,
			Valid:     true,
			InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, txBytes},
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: "evmcc",
				RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
					resilienceBalanceWrite(from, 98),
					resilienceNonceWrite(from, 2),
					resilienceBalanceWrite(to, 2),
				}},
			}},
		}},
	}
	err = chain.Handle(t.Context(), block2)
	require.Error(t, err, "Handle must fail when InsertBlock cannot run")

	require.Equal(t, prevHashAfterBlock1, chain.prevHash,
		"prevHash must not advance when InsertBlock fails")
}
