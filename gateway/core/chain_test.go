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
	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	co "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	sdkstate "github.com/hyperledger/fabric-x-sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func createTestEthTx(t *testing.T, key *ecdsa.PrivateKey, to common.Address, value *big.Int) *types.Transaction {
	t.Helper()
	tx := types.NewTransaction(0, to, value, 21000, big.NewInt(1000), []byte("test data"))
	signer := types.NewEIP155Signer(big.NewInt(4011))
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

func execPayload(t *testing.T, status endorsementpb.ExecutionStatus, gasUsed uint64) []byte {
	t.Helper()
	b, err := co.MarshalExecutionMetadata(&endorsementpb.ExecutionMetadata{Status: status, GasUsed: gasUsed})
	require.NoError(t, err)
	return b
}

func succeededPayload(t *testing.T) []byte {
	return execPayload(t, endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, 21000)
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
			Status:    blocks.StatusCommitted,
			EventName: co.ProposalTypeEVMTx,
			InputArgs: [][]byte{ethb},
			Payload:   succeededPayload(t),
		}},
	}

	got := ConvertToDomain(b)

	assert.Equal(t, uint64(42), got.BlockNumber)
	assert.Equal(t, []byte("block-hash"), got.BlockHash)
	assert.Equal(t, []byte("parent-hash"), got.ParentHash)
	assert.Equal(t, int64(12345), got.Timestamp)
	require.Len(t, got.Transactions, 1)
	assert.Equal(t, uint8(1), got.Transactions[0].Status)
	assert.Equal(t, "tx-1", got.Transactions[0].FabricTxID)
	assert.Equal(t, blocks.StatusCommitted, got.Transactions[0].FabricTxStatus)
}

func TestConvertToDomain_InvalidTxStatus(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(100))

	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{{
			ID:        "tx-bad",
			Status:    blocks.StatusMVCCConflict, // invalid tx
			EventName: co.ProposalTypeEVMTx,
			InputArgs: [][]byte{ethb},
			Payload:   succeededPayload(t),
		}},
	}

	got := ConvertToDomain(b)

	require.Len(t, got.Transactions, 1)
	assert.Equal(t, uint8(0), got.Transactions[0].Status)
	assert.Equal(t, blocks.StatusMVCCConflict, got.Transactions[0].FabricTxStatus)
}

// A Fabric-invalid tx can still carry events recorded during endorsement-time
// simulation, before the later conflict was detected. Since those effects
// were never committed, their logs must not be surfaced either. Examples: MVCC
// conflict or endorsement policy error. Same for an invalid ethereum transaction.
func TestConvertToDomain_InvalidTxDropsLogs(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	ethb := marshaledEthTx(t, key, to, big.NewInt(100))

	events := resilienceEvents(t, "tx-bad", []sdkstate.Log{{
		Address: to.Bytes(),
		Topics:  [][]byte{resilienceHash(0xAA)},
		Data:    []byte{0x01},
	}})

	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{{
			ID:        "tx-bad",
			Status:    blocks.StatusMVCCConflict, // invalid tx, but events survive from simulation
			Event:     events,
			EventName: co.ProposalTypeEVMTx,
			InputArgs: [][]byte{ethb},
			Payload:   succeededPayload(t),
		}},
	}

	got := ConvertToDomain(b)

	require.Len(t, got.Transactions, 1)
	assert.Equal(t, uint8(0), got.Transactions[0].Status)
	assert.Empty(t, got.Transactions[0].Logs)
}

func TestConvertToDomain_SkipsNonEVMTx(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(100))

	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			{ID: "tx-no-args", Status: blocks.StatusCommitted, EventName: co.ProposalTypeEVMTx, InputArgs: nil},
			{ID: "tx-no-marker", Status: blocks.StatusCommitted, InputArgs: [][]byte{ethb}},
			{ID: "tx-other-event", Status: blocks.StatusCommitted, EventName: "event", InputArgs: [][]byte{ethb}},
			{ID: "tx-old-layout", Status: blocks.StatusCommitted, EventName: co.ProposalTypeEVMTx, InputArgs: [][]byte{{0xfb}, ethb}},
		},
	}

	got := ConvertToDomain(b)

	assert.Len(t, got.Transactions, 0)
}

// Status and gas come from the Payload; cumulative gas adds up in block order.
func TestConvertToDomain_StatusAndGasFromPayload(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := func(id string, nonce uint64, status blocks.Status, payload []byte) blocks.Transaction {
		signed, err := types.SignTx(types.NewTransaction(nonce, to, big.NewInt(1), 21000, big.NewInt(0), nil), types.HomesteadSigner{}, key)
		require.NoError(t, err)
		raw, err := signed.MarshalBinary()
		require.NoError(t, err)
		return blocks.Transaction{ID: id, Status: status, EventName: co.ProposalTypeEVMTx, InputArgs: [][]byte{raw}, Payload: payload}
	}

	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{
			tx("ok", 0, blocks.StatusCommitted, execPayload(t, endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, 21000)),
			tx("revert", 1, blocks.StatusCommitted, execPayload(t, endorsementpb.ExecutionStatus_EXECUTION_STATUS_REVERTED, 30000)),
			tx("execfail", 2, blocks.StatusCommitted, execPayload(t, endorsementpb.ExecutionStatus_EXECUTION_STATUS_EXEC_FAILED, 50000)),
			tx("mvcc", 3, blocks.StatusMVCCConflict, execPayload(t, endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, 40000)),
			tx("no-payload", 4, blocks.StatusCommitted, nil),
			tx("bad-payload", 5, blocks.StatusCommitted, []byte{0xff, 0xff}),
		},
	}

	got := ConvertToDomain(b)

	require.Len(t, got.Transactions, 6)
	want := []struct {
		status          uint8
		gas, cumulative uint64
	}{
		{1, 21000, 21000},
		{0, 30000, 51000},
		{0, 50000, 101000},
		{0, 0, 101000}, // never ran on-chain
		{0, 0, 101000},
		{0, 0, 101000},
	}
	for i, w := range want {
		etx := got.Transactions[i]
		assert.Equal(t, w.status, etx.Status, "%s status", etx.FabricTxID)
		assert.Equal(t, w.gas, etx.GasUsed, "%s gas", etx.FabricTxID)
		assert.Equal(t, w.cumulative, etx.CumulativeGasUsed, "%s cumulative gas", etx.FabricTxID)
	}
}

func TestConvertToDomain_SkipsInvalidEthBytes(t *testing.T) {
	b := blocks.Block{
		Number: 1,
		Transactions: []blocks.Transaction{{
			ID:        "tx-bad-bytes",
			Status:    blocks.StatusCommitted,
			InputArgs: [][]byte{[]byte("not-an-eth-tx")},
		}},
	}

	got := ConvertToDomain(b)

	assert.Len(t, got.Transactions, 0)
}

func TestConvertToDomain_EmptyBlock(t *testing.T) {
	b := blocks.Block{Number: 5}
	got := ConvertToDomain(b)

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

	logIndex := int64(0)
	domainTx, err := convertTransaction(ethb, []byte("block-hash"), 42, 5, "fabric-tx-123", 1, blocks.StatusCommitted, nil, &logIndex)

	require.NoError(t, err)
	assert.Equal(t, ethTx.Hash().Bytes(), domainTx.TxHash)
	assert.Equal(t, []byte("block-hash"), domainTx.BlockHash)
	assert.Equal(t, uint64(42), domainTx.BlockNumber)
	assert.Equal(t, int64(5), domainTx.TxIndex)
	assert.Equal(t, to.Bytes(), domainTx.ToAddress)
	assert.Nil(t, domainTx.ContractAddress)
	assert.Equal(t, "fabric-tx-123", domainTx.FabricTxID)
	assert.Equal(t, uint8(1), domainTx.Status)
	assert.Equal(t, blocks.StatusCommitted, domainTx.FabricTxStatus)
	assert.NotNil(t, domainTx.FromAddress)
	assert.NotNil(t, domainTx.RawTx)
}

func TestConvertTransaction_ContractCreation(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethTx := types.NewContractCreation(0, big.NewInt(0), 1000000, big.NewInt(1000), []byte("contract code"))
	signer := types.NewEIP155Signer(big.NewInt(4011))
	signed, err := types.SignTx(ethTx, signer, key)
	require.NoError(t, err)
	ethb, _ := signed.MarshalBinary()

	logIndex := int64(0)
	domainTx, err := convertTransaction(ethb, []byte("block-hash"), 42, 3, "fabric-tx-456", 1, blocks.StatusCommitted, nil, &logIndex)

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

	logIndex := int64(0)
	_, err := convertTransaction(ethb, []byte("block-hash"), 42, 1, "fabric-tx-789", 1, blocks.StatusCommitted, nil, &logIndex)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sender")
}

// TestConvertTransaction_FabricStatuses checks that the commit status is carried
// through to the domain row independently of the EVM receipt status, which the two
// valid cases below distinguish: a revert commits on Fabric while reporting an EVM
// status of 0. The statuses are blocks.Status values, the SDK's protocol-neutral enum
// shared by both delivery paths, rather than ledger-specific validation codes.
func TestConvertTransaction_FabricStatuses(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	ethb := marshaledEthTx(t, key, common.HexToAddress("0x1234567890123456789012345678901234567890"), big.NewInt(100))

	tests := []struct {
		name         string
		ethStatus    uint8
		fabricStatus blocks.Status
		wantValid    bool
	}{
		{"committed", 1, blocks.StatusCommitted, true},
		// A revert is Fabric-valid even though its EVM status is 0.
		{"revert", 0, blocks.StatusCommitted, true},
		{"mvcc_conflict", 0, blocks.StatusMVCCConflict, false},
		{"endorsement_failure", 0, blocks.StatusEndorsementPolicyFailure, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logIndex := int64(0)
			domainTx, err := convertTransaction(ethb, []byte("bh"), 42, 1, "tx", tt.ethStatus, tt.fabricStatus, nil, &logIndex)
			require.NoError(t, err)
			assert.Equal(t, tt.ethStatus, domainTx.Status)
			assert.Equal(t, tt.fabricStatus, domainTx.FabricTxStatus)
			// The status is the only record of a Fabric-valid commit: no separate
			// boolean rides alongside it that could disagree.
			assert.Equal(t, tt.wantValid, domainTx.FabricTxStatus.Valid())
		})
	}
}
