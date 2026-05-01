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

	chain, err := NewChain(dbPath, triePath)
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
			InputArgs: [][]byte{[]byte("invoke"), txBytes},
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
