/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"crypto/ecdsa"
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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type replaySnapshot struct {
	Blocks []domain.Block
	Txs    []domain.Transaction
	Logs   []domain.Log
}

func replayHash(prefix byte) []byte {
	hash := make([]byte, 32)
	hash[0] = prefix
	return hash
}

func replayBalanceWrite(addr common.Address, bal int64) blocks.KVWrite {
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":bal",
		Value: big.NewInt(bal).Bytes(),
	}
}

func replayNonceWrite(addr common.Address, nonce uint64) blocks.KVWrite {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, nonce)
	return blocks.KVWrite{
		Key:   "acc:" + addr.Hex() + ":nonce",
		Value: buf,
	}
}

func replayStorageWrite(addr common.Address, slot, value common.Hash) blocks.KVWrite {
	return blocks.KVWrite{
		Key:   "str:" + addr.Hex() + ":" + slot.Hex(),
		Value: []byte(value.Hex()),
	}
}

func replayEvents(t *testing.T, txID string, logs []sdkstate.Log) []byte {
	t.Helper()

	payload, err := json.Marshal(logs)
	require.NoError(t, err)

	event, err := fc.MarshalLogs(payload, "evmcc", txID)
	require.NoError(t, err)
	return event
}

func newReplayChain(t *testing.T, dir string) *Chain {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	chain, err := NewChain(filepath.Join(dir, "gateway.db"), filepath.Join(dir, "trie"))
	require.NoError(t, err)
	return chain
}

func buildReplayTx(
	t *testing.T,
	key *ecdsa.PrivateKey,
	nonce uint64,
	to common.Address,
	value int64,
	fabricID string,
	valid bool,
	fabricStatus int,
	writes []blocks.KVWrite,
	logs []sdkstate.Log,
) (blocks.Transaction, []byte) {
	t.Helper()

	tx := types.NewTransaction(nonce, to, big.NewInt(value), 21000, big.NewInt(1000), []byte(fabricID))
	signer := types.NewEIP155Signer(big.NewInt(31337))
	signed, err := types.SignTx(tx, signer, key)
	require.NoError(t, err)

	txBytes, err := signed.MarshalBinary()
	require.NoError(t, err)

	blockTx := blocks.Transaction{
		ID:        fabricID,
		Valid:     valid,
		Status:    fabricStatus,
		InputArgs: [][]byte{[]byte("invoke"), txBytes},
		NsRWS: []blocks.NsReadWriteSet{{
			Namespace: "evmcc",
			RWS:       blocks.ReadWriteSet{Writes: writes},
		}},
	}
	if len(logs) > 0 {
		blockTx.Events = replayEvents(t, fabricID, logs)
	}

	return blockTx, signed.Hash().Bytes()
}

func captureReplaySnapshot(t *testing.T, chain *Chain, blockCount int, txHashes [][]byte) replaySnapshot {
	t.Helper()

	snapshot := replaySnapshot{
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

func TestReplayFromGenesisMatchesPersistedContinuousGatewayState(t *testing.T) {
	key1, err := crypto.GenerateKey()
	require.NoError(t, err)
	key2, err := crypto.GenerateKey()
	require.NoError(t, err)

	from1 := crypto.PubkeyToAddress(key1.PublicKey)
	from2 := crypto.PubkeyToAddress(key2.PublicKey)
	sink := common.HexToAddress("0x2222222222222222222222222222222222222222")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	slot := common.HexToHash("0x01")
	value := common.HexToHash("0xdeadbeef")

	tx1, txHash1 := buildReplayTx(
		t,
		key1,
		0,
		sink,
		10,
		"fabric-tx-1",
		true,
		0,
		[]blocks.KVWrite{
			replayBalanceWrite(from1, 90),
			replayNonceWrite(from1, 1),
			replayBalanceWrite(sink, 10),
		},
		nil,
	)
	tx1.Number = 0

	tx2, txHash2 := buildReplayTx(
		t,
		key1,
		1,
		sink,
		20,
		"fabric-tx-2",
		true,
		0,
		[]blocks.KVWrite{
			replayBalanceWrite(from1, 70),
			replayNonceWrite(from1, 2),
			replayBalanceWrite(sink, 30),
			replayStorageWrite(contract, slot, value),
		},
		[]sdkstate.Log{{
			Address: contract.Bytes(),
			Topics:  [][]byte{replayHash(0xA0), replayHash(0xB0)},
			Data:    []byte{0xCA, 0xFE},
		}},
	)
	tx2.Number = 0

	tx3, txHash3 := buildReplayTx(
		t,
		key2,
		0,
		sink,
		99,
		"fabric-tx-3",
		false,
		11,
		[]blocks.KVWrite{
			replayBalanceWrite(from2, 1),
			replayNonceWrite(from2, 1),
		},
		nil,
	)
	tx3.Number = 1

	tx4, txHash4 := buildReplayTx(
		t,
		key1,
		2,
		sink,
		10,
		"fabric-tx-4",
		true,
		0,
		[]blocks.KVWrite{
			replayBalanceWrite(from1, 60),
			replayNonceWrite(from1, 3),
			replayBalanceWrite(sink, 40),
		},
		nil,
	)
	tx4.Number = 0

	history := []blocks.Block{
		{
			Number:       1,
			Hash:         replayHash(0x01),
			ParentHash:   replayHash(0x00),
			Timestamp:    1001,
			Transactions: []blocks.Transaction{tx1},
		},
		{
			Number:       2,
			Hash:         replayHash(0x02),
			ParentHash:   replayHash(0x01),
			Timestamp:    1002,
			Transactions: []blocks.Transaction{tx2, tx3},
		},
		{
			Number:       3,
			Hash:         replayHash(0x03),
			ParentHash:   replayHash(0x02),
			Timestamp:    1003,
			Transactions: []blocks.Transaction{tx4},
		},
	}

	continuousDir := filepath.Join(t.TempDir(), "continuous")
	continuous := newReplayChain(t, continuousDir)
	for _, block := range history {
		require.NoError(t, continuous.Handle(t.Context(), block))
	}

	continuousRoot := continuous.ts.Root()
	continuousSnapshot := captureReplaySnapshot(t, continuous, len(history), [][]byte{txHash1, txHash2, txHash3, txHash4})
	continuousHeight, err := continuous.BlockNumber(t.Context())
	require.NoError(t, err)
	require.NoError(t, continuous.Close())

	reopened := newReplayChain(t, continuousDir)
	defer reopened.Close()

	require.Equal(t, continuousRoot, reopened.ts.Root())
	reopenedHeight, err := reopened.BlockNumber(t.Context())
	require.NoError(t, err)
	require.Equal(t, continuousHeight, reopenedHeight)

	reopenedSnapshot := captureReplaySnapshot(t, reopened, len(history), [][]byte{txHash1, txHash2, txHash3, txHash4})
	require.Equal(t, continuousSnapshot, reopenedSnapshot)

	rebuilt := newReplayChain(t, filepath.Join(t.TempDir(), "rebuilt"))
	defer rebuilt.Close()
	for _, block := range history {
		require.NoError(t, rebuilt.Handle(t.Context(), block))
	}

	require.Equal(t, continuousRoot, rebuilt.ts.Root())

	rebuiltSnapshot := captureReplaySnapshot(t, rebuilt, len(history), [][]byte{txHash1, txHash2, txHash3, txHash4})
	require.Equal(t, reopenedSnapshot, rebuiltSnapshot)

	rebuiltHeight, err := rebuilt.BlockNumber(t.Context())
	require.NoError(t, err)
	require.Equal(t, reopenedHeight, rebuiltHeight)
}
