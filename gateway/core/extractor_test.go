/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func createTestEthTx(t *testing.T, key *ecdsa.PrivateKey, to common.Address, value *big.Int) *types.Transaction {
	t.Helper()
	tx := types.NewTransaction(0, to, value, 21000, big.NewInt(1000), []byte("test data"))
	signer := types.NewEIP155Signer(big.NewInt(31337))
	signed, err := types.SignTx(tx, signer, key)
	require.NoError(t, err)
	return signed
}

func marshaledEthTx(t *testing.T, key *ecdsa.PrivateKey, to common.Address, value *big.Int) []byte {
	t.Helper()
	b, err := createTestEthTx(t, key, to, value).MarshalBinary()
	require.NoError(t, err)
	return b
}

// --- convertToDomain ---

func TestConvertToDomain_ValidTx(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(100))

	b := blocks.Block{
		Number:     42,
		Hash:       []byte("block-hash"),
		ParentHash: []byte("parent-hash"),
		Timestamp:  12345,
		Transactions: []blocks.Transaction{{
			ID:        "tx-1",
			Number:    0,
			Valid:     true,
			Status:    0,
			InputArgs: [][]byte{nil, ethb},
		}},
	}

	got := convertToDomain(b)

	assert.Equal(t, uint64(42), got.BlockNumber)
	assert.Equal(t, []byte("block-hash"), got.BlockHash)
	assert.Equal(t, []byte("parent-hash"), got.ParentHash)
	assert.Equal(t, int64(12345), got.Timestamp)
	require.Len(t, got.Transactions, 1)
	assert.Equal(t, uint8(1), got.Transactions[0].Status)
	assert.Equal(t, "tx-1", got.Transactions[0].FabricTxID)
}

func TestConvertToDomain_InvalidTxStatus(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(100))

	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{{
			ID:        "tx-bad",
			Valid:     false, // invalid tx
			InputArgs: [][]byte{nil, ethb},
		}},
	}

	got := convertToDomain(b)

	require.Len(t, got.Transactions, 1)
	assert.Equal(t, uint8(0), got.Transactions[0].Status)
}

func TestConvertToDomain_SkipsInsufficientInputArgs(t *testing.T) {
	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			{ID: "tx-no-args", Valid: true, InputArgs: nil},
			{ID: "tx-one-arg", Valid: true, InputArgs: [][]byte{[]byte("only-one")}},
		},
	}

	got := convertToDomain(b)

	assert.Len(t, got.Transactions, 0)
}

func TestConvertToDomain_SkipsInvalidEthBytes(t *testing.T) {
	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{{
			ID:        "tx-bad-bytes",
			Valid:     true,
			InputArgs: [][]byte{nil, []byte("not-an-eth-tx")},
		}},
	}

	got := convertToDomain(b)

	assert.Len(t, got.Transactions, 0)
}

func TestConvertToDomain_EmptyBlock(t *testing.T) {
	b := blocks.Block{Number: 5}
	got := convertToDomain(b)

	assert.Equal(t, uint64(5), got.BlockNumber)
	assert.Len(t, got.Transactions, 0)
}

// --- convertTransaction ---

func TestConvertTransaction_RegularTransfer(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ethTx := createTestEthTx(t, key, to, big.NewInt(100))
	ethb, _ := ethTx.MarshalBinary()

	domainTx, err := convertTransaction(ethb, []byte("block-hash"), 42, 5, "fabric-tx-123", 1, 0, nil)

	require.NoError(t, err)
	assert.Equal(t, ethTx.Hash().Bytes(), domainTx.TxHash)
	assert.Equal(t, []byte("block-hash"), domainTx.BlockHash)
	assert.Equal(t, uint64(42), domainTx.BlockNumber)
	assert.Equal(t, int64(5), domainTx.TxIndex)
	assert.Equal(t, to.Bytes(), domainTx.ToAddress)
	assert.Nil(t, domainTx.ContractAddress)
	assert.Equal(t, "fabric-tx-123", domainTx.FabricTxID)
	assert.Equal(t, uint8(1), domainTx.Status)
	assert.Equal(t, 0, domainTx.FabricTxStatus)
	assert.NotNil(t, domainTx.FromAddress)
	assert.NotNil(t, domainTx.RawTx)
}

func TestConvertTransaction_ContractCreation(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethTx := types.NewContractCreation(0, big.NewInt(0), 1000000, big.NewInt(1000), []byte("contract code"))
	signer := types.NewEIP155Signer(big.NewInt(31337))
	signed, err := types.SignTx(ethTx, signer, key)
	require.NoError(t, err)
	ethb, _ := signed.MarshalBinary()

	domainTx, err := convertTransaction(ethb, []byte("block-hash"), 42, 3, "fabric-tx-456", 1, 0, nil)

	require.NoError(t, err)
	assert.Nil(t, domainTx.ToAddress)
	assert.NotNil(t, domainTx.ContractAddress)
	from := crypto.PubkeyToAddress(key.PublicKey)
	assert.Equal(t, crypto.CreateAddress(from, signed.Nonce()).Bytes(), domainTx.ContractAddress)
}

func TestConvertTransaction_InvalidSignature(t *testing.T) {
	ethTx := types.NewTransaction(0,
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		big.NewInt(100), 21000, big.NewInt(1000), []byte("test"),
	)
	ethb, _ := ethTx.MarshalBinary()

	_, err := convertTransaction(ethb, []byte("block-hash"), 42, 1, "fabric-tx-789", 1, 0, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sender")
}

func TestConvertTransaction_ValidationCodes(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(100))

	tests := []struct {
		name           string
		ethStatus      uint8
		validationCode int
	}{
		{"valid", 1, 0},
		{"mvcc_conflict", 0, 11},
		{"endorsement_failure", 0, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domainTx, err := convertTransaction(ethb, []byte("bh"), 42, 1, "tx", tt.ethStatus, tt.validationCode, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.ethStatus, domainTx.Status)
			assert.Equal(t, tt.validationCode, domainTx.FabricTxStatus)
		})
	}
}
