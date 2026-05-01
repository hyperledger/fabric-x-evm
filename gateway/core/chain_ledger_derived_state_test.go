/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"encoding/binary"
	"encoding/json"
	"math/big"
	"os"
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

type ledgerDerivedSnapshot struct {
	Latest domain.Block
	Blocks []domain.Block
	Txs    []domain.Transaction
	Logs   []domain.Log
}

func ledgerDerivedHash(prefix byte) []byte {
	hash := make([]byte, 32)
	hash[0] = prefix
	return hash
}

func ledgerDerivedBalanceWrite(addr common.Address, bal int64) blocks.KVWrite {
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":bal",
		Value: big.NewInt(bal).Bytes(),
	}
}

func ledgerDerivedNonceWrite(addr common.Address, nonce uint64) blocks.KVWrite {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, nonce)
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":nonce",
		Value: buf,
	}
}

func ledgerDerivedEvents(t *testing.T, txID string, logs []sdkstate.Log) []byte {
	t.Helper()

	payload, err := json.Marshal(logs)
	require.NoError(t, err)

	event, err := fc.MarshalLogs(payload, "evmcc", txID)
	require.NoError(t, err)
	return event
}

func newLedgerDerivedChain(t *testing.T, dir string) *Chain {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	chain, err := NewChain(filepath.Join(dir, "gateway.db"), filepath.Join(dir, "trie"))
	require.NoError(t, err)
	return chain
}

func captureLedgerDerivedSnapshot(t *testing.T, chain *Chain, blockCount int, txHashes [][]byte) ledgerDerivedSnapshot {
	t.Helper()

	latest, err := chain.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)

	snapshot := ledgerDerivedSnapshot{
		Latest: *latest,
		Blocks: make([]domain.Block, 0, blockCount),
		Txs:    make([]domain.Transaction, 0, len(txHashes)),
	}

	for i := 1; i <= blockCount; i++ {
		block, err := chain.GetBlockByNumber(t.Context(), uint64(i), false)
		require.NoError(t, err)
		require.NotNil(t, block)
		snapshot.Blocks = append(snapshot.Blocks, *block)
	}

	for _, txHash := range txHashes {
		tx, err := chain.GetTransactionByHash(t.Context(), txHash)
		require.NoError(t, err)
		require.NotNil(t, tx)
		snapshot.Txs = append(snapshot.Txs, *tx)
	}

	logs, err := chain.GetLogs(t.Context(), domain.LogFilter{})
	require.NoError(t, err)
	snapshot.Logs = logs

	return snapshot
}

func TestGatewayStateCanBeDiscardedAndRebuiltFromLedgerHistory(t *testing.T) {
	key1, err := crypto.GenerateKey()
	require.NoError(t, err)
	key2, err := crypto.GenerateKey()
	require.NoError(t, err)

	from1 := crypto.PubkeyToAddress(key1.PublicKey)
	from2 := crypto.PubkeyToAddress(key2.PublicKey)
	sink := common.HexToAddress("0x4444444444444444444444444444444444444444")
	emitter := common.HexToAddress("0x5555555555555555555555555555555555555555")

	tx1 := createTestEthTx(t, key1, sink, big.NewInt(15))
	tx1Bytes, err := tx1.MarshalBinary()
	require.NoError(t, err)

	tx2 := createTestEthTx(t, key2, sink, big.NewInt(9))
	tx2Bytes, err := tx2.MarshalBinary()
	require.NoError(t, err)

	tx3 := createTestEthTx(t, key1, sink, big.NewInt(5))
	tx3Bytes, err := tx3.MarshalBinary()
	require.NoError(t, err)

	history := []blocks.Block{
		{
			Number:     1,
			Hash:       ledgerDerivedHash(0x01),
			ParentHash: ledgerDerivedHash(0x00),
			Timestamp:  1010,
			Transactions: []blocks.Transaction{{
				ID:        "fabric-tx-1",
				Number:    0,
				Valid:     true,
				Status:    0,
				InputArgs: [][]byte{[]byte("invoke"), tx1Bytes},
				NsRWS: []blocks.NsReadWriteSet{{
					Namespace: "evmcc",
					RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
						ledgerDerivedBalanceWrite(from1, 85),
						ledgerDerivedNonceWrite(from1, 1),
						ledgerDerivedBalanceWrite(sink, 15),
					}},
				}},
			}},
		},
		{
			Number:     2,
			Hash:       ledgerDerivedHash(0x02),
			ParentHash: ledgerDerivedHash(0x01),
			Timestamp:  1020,
			Transactions: []blocks.Transaction{
				{
					ID:        "fabric-tx-2",
					Number:    0,
					Valid:     true,
					Status:    0,
					InputArgs: [][]byte{[]byte("invoke"), tx2Bytes},
					Events: ledgerDerivedEvents(t, "fabric-tx-2", []sdkstate.Log{{
						Address: emitter.Bytes(),
						Topics:  [][]byte{ledgerDerivedHash(0xA1)},
						Data:    []byte{0xCA, 0xFE},
					}}),
					NsRWS: []blocks.NsReadWriteSet{{
						Namespace: "evmcc",
						RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
							ledgerDerivedBalanceWrite(from2, 91),
							ledgerDerivedNonceWrite(from2, 1),
							ledgerDerivedBalanceWrite(sink, 24),
						}},
					}},
				},
				{
					ID:        "fabric-tx-3",
					Number:    1,
					Valid:     false,
					Status:    11,
					InputArgs: [][]byte{[]byte("invoke"), tx3Bytes},
					NsRWS: []blocks.NsReadWriteSet{{
						Namespace: "evmcc",
						RWS: blocks.ReadWriteSet{Writes: []blocks.KVWrite{
							ledgerDerivedBalanceWrite(from1, 80),
							ledgerDerivedNonceWrite(from1, 2),
						}},
					}},
				},
			},
		},
	}

	sourceDir := filepath.Join(t.TempDir(), "source")
	source := newLedgerDerivedChain(t, sourceDir)
	for _, block := range history {
		require.NoError(t, source.Handle(t.Context(), block))
	}

	expectedRoot := source.ts.Root()
	expectedSnapshot := captureLedgerDerivedSnapshot(
		t,
		source,
		len(history),
		[][]byte{tx1.Hash().Bytes(), tx2.Hash().Bytes(), tx3.Hash().Bytes()},
	)

	require.NoError(t, source.Close())
	require.NoError(t, os.RemoveAll(sourceDir))

	rebuilt := newLedgerDerivedChain(t, filepath.Join(t.TempDir(), "rebuilt"))
	defer rebuilt.Close()
	for _, block := range history {
		require.NoError(t, rebuilt.Handle(t.Context(), block))
	}

	require.Equal(t, expectedRoot, rebuilt.ts.Root())

	rebuiltSnapshot := captureLedgerDerivedSnapshot(
		t,
		rebuilt,
		len(history),
		[][]byte{tx1.Hash().Bytes(), tx2.Hash().Bytes(), tx3.Hash().Bytes()},
	)
	require.Equal(t, expectedSnapshot, rebuiltSnapshot)
}
