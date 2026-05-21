/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTx(nonce uint64) *types.Transaction {
	return types.NewTransaction(
		nonce,
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		big.NewInt(1),
		21000,
		big.NewInt(1),
		nil,
	)
}

func signedTestTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64) *types.Transaction {
	t.Helper()

	tx := types.NewTransaction(
		nonce,
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		big.NewInt(1),
		21000,
		big.NewInt(1),
		nil,
	)

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(tx.ChainId()), key)
	require.NoError(t, err)
	return signedTx
}

func signedERC20TransferTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, token common.Address, recipient common.Address) *types.Transaction {
	t.Helper()

	data := make([]byte, 4+32+32)
	copy(data[:4], common.Hex2Bytes("a9059cbb"))
	copy(data[4+12:4+32], recipient.Bytes())
	copy(data[4+32:], common.LeftPadBytes(big.NewInt(1).Bytes(), 32))

	tx := types.NewTransaction(
		nonce,
		token,
		big.NewInt(0),
		100000,
		big.NewInt(1),
		data,
	)

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(tx.ChainId()), key)
	require.NoError(t, err)
	return signedTx
}

func TestNewTxQueue_InitializesPendingAndInProgress(t *testing.T) {
	q := NewTxQueue()

	require.NotNil(t, q.cond)
	require.NotNil(t, q.pendingQueue)
	require.NotNil(t, q.inProgressMap)
	assert.Len(t, q.pendingQueue, 0)
	assert.Len(t, q.inProgressMap, 0)
	assert.False(t, q.done)
}

func TestTxQueue_EnqueueAddsToPendingQueue(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)

	q.Enqueue(tx)

	require.Len(t, q.pendingQueue, 1)
	assert.Equal(t, tx, q.pendingQueue[0])
	assert.Len(t, q.inProgressMap, 0)
}

func TestTxQueue_DequeueMovesTxToInProgressMap(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, tx.Hash(), got.Hash())
	assert.Len(t, q.pendingQueue, 0)

	inProgressTx, exists := q.inProgressMap[tx.Hash()]
	require.True(t, exists)
	assert.Equal(t, tx.Hash(), inProgressTx.Hash())
}

func TestTxQueue_IsPending_FindsInPendingQueue(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)

	result := q.IsPending(tx.Hash())
	require.NotNil(t, result)
	assert.Equal(t, tx.Hash(), result.Hash())
}

func TestTxQueue_IsPending_FindsInProgressMap(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)
	q.Dequeue() // Moves to inProgressMap

	result := q.IsPending(tx.Hash())
	require.NotNil(t, result)
	assert.Equal(t, tx.Hash(), result.Hash())
}

func TestTxQueue_IsPending_ReturnsNilWhenNotFound(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)

	result := q.IsPending(tx.Hash())
	assert.Nil(t, result)
}

func TestTxQueue_IsPending_ReturnsNilAfterComplete(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)
	q.Dequeue() // Moves to inProgressMap
	q.Complete(tx.Hash())

	result := q.IsPending(tx.Hash())
	assert.Nil(t, result)
}

func TestTxQueue_Complete_RemovesFromInProgressMap(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)
	q.Dequeue() // Moves to inProgressMap

	q.Complete(tx.Hash())

	_, exists := q.inProgressMap[tx.Hash()]
	assert.False(t, exists)
}

func TestTxQueue_Complete_IsIdempotent(t *testing.T) {
	q := NewTxQueue()
	tx := testTx(1)
	q.Enqueue(tx)
	q.Dequeue()

	// Call Complete multiple times
	q.Complete(tx.Hash())
	q.Complete(tx.Hash())
	q.Complete(tx.Hash())

	// Should not panic and map should be empty
	assert.Len(t, q.inProgressMap, 0)
}

func TestTxQueue_Handle_MarksTransactionsComplete(t *testing.T) {
	q := NewTxQueue()
	tx1 := testTx(1)
	tx2 := testTx(2)

	// Enqueue and dequeue to move to inProgressMap
	q.Enqueue(tx1)
	q.Enqueue(tx2)
	q.Dequeue()
	q.Dequeue()

	// Verify both are in progress
	assert.NotNil(t, q.IsPending(tx1.Hash()))
	assert.NotNil(t, q.IsPending(tx2.Hash()))

	// Create a block with these transactions
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx1.Hash().Bytes()},
			{TxHash: tx2.Hash().Bytes()},
		},
	}

	// Handle the block
	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	// Verify both are now complete (not pending)
	assert.Nil(t, q.IsPending(tx1.Hash()))
	assert.Nil(t, q.IsPending(tx2.Hash()))
}

func TestTxQueue_Handle_EmptyBlock(t *testing.T) {
	q := NewTxQueue()

	block := &domain.Block{
		BlockNumber:  1,
		Transactions: []domain.Transaction{},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)
}

func TestTxQueue_DequeueSkipsPendingTxFromSenderWithOutstandingTx(t *testing.T) {
	q := NewTxQueue()

	keyA, err := crypto.GenerateKey()
	require.NoError(t, err)
	keyB, err := crypto.GenerateKey()
	require.NoError(t, err)

	txA1 := signedTestTx(t, keyA, 1)
	txA2 := signedTestTx(t, keyA, 2)
	txB1 := signedTestTx(t, keyB, 1)

	q.Enqueue(txA1)
	q.Enqueue(txA2)
	q.Enqueue(txB1)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txA1.Hash(), got.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txB1.Hash(), got.Hash())

	q.Complete(txA1.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txA2.Hash(), got.Hash())
}

func TestTxQueue_Complete_RemovesSenderTrackingAndUnblocksWaitingSender(t *testing.T) {
	q := NewTxQueue()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	tx1 := signedTestTx(t, key, 1)
	tx2 := signedTestTx(t, key, 2)

	q.Enqueue(tx1)
	q.Enqueue(tx2)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx1.Hash(), got.Hash())

	dequeued := make(chan *types.Transaction, 1)
	go func() {
		tx, ok := q.Dequeue()
		if !ok {
			dequeued <- nil
			return
		}
		dequeued <- tx
	}()

	select {
	case tx := <-dequeued:
		t.Fatalf("unexpected dequeue before complete: %v", tx.Hash())
	case <-time.After(50 * time.Millisecond):
	}

	q.Complete(tx1.Hash())

	select {
	case tx := <-dequeued:
		require.NotNil(t, tx)
		require.Equal(t, tx2.Hash(), tx.Hash())
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dequeue after complete")
	}

	sender, err := types.Sender(types.LatestSignerForChainID(tx1.ChainId()), tx1)
	require.NoError(t, err)
	assert.Equal(t, 1, q.inProgressParticipants[sender])
	_, tracked := q.txParticipants[tx1.Hash()]
	assert.False(t, tracked)

	q.Complete(tx2.Hash())
	_, blocked := q.inProgressParticipants[sender]
	assert.False(t, blocked)
}

func TestTxQueue_Complete_OutOfOrderSameSenderCompletionStillClearsTracking(t *testing.T) {
	q := NewTxQueue()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	tx1 := signedTestTx(t, key, 1)
	tx2 := signedTestTx(t, key, 2)

	q.Enqueue(tx1)
	q.Enqueue(tx2)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx1.Hash(), got.Hash())

	q.Complete(tx1.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx2.Hash(), got.Hash())

	sender, err := types.Sender(types.LatestSignerForChainID(tx1.ChainId()), tx1)
	require.NoError(t, err)
	assert.Equal(t, 1, q.inProgressParticipants[sender])

	q.Complete(tx1.Hash())
	assert.Equal(t, 1, q.inProgressParticipants[sender])

	q.Complete(tx2.Hash())
	_, blocked := q.inProgressParticipants[sender]
	assert.False(t, blocked)
	_, tracked1 := q.txParticipants[tx1.Hash()]
	assert.False(t, tracked1)
	_, tracked2 := q.txParticipants[tx2.Hash()]
	assert.False(t, tracked2)
}

func TestTxQueue_DequeueSkipsPendingTxWithOutstandingRecipient(t *testing.T) {
	q := NewTxQueue()

	keyA, err := crypto.GenerateKey()
	require.NoError(t, err)
	keyB, err := crypto.GenerateKey()
	require.NoError(t, err)
	keyC, err := crypto.GenerateKey()
	require.NoError(t, err)

	token := common.HexToAddress("0x1111111111111111111111111111111111111111")
	recipient := crypto.PubkeyToAddress(keyC.PublicKey)

	txA := signedERC20TransferTx(t, keyA, 1, token, recipient)
	txB := signedERC20TransferTx(t, keyB, 1, token, recipient)
	otherRecipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	txC := signedERC20TransferTx(t, keyB, 2, token, otherRecipient)

	q.Enqueue(txA)
	q.Enqueue(txB)
	q.Enqueue(txC)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txA.Hash(), got.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txC.Hash(), got.Hash())

	// Must complete both txA and txC before txB can be dequeued
	// txB shares sender (keyB) with txC, so it's blocked until txC completes
	q.Complete(txA.Hash())
	q.Complete(txC.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, txB.Hash(), got.Hash())
}

func TestTxQueue_ContractCreationHasNoRecipientParticipant(t *testing.T) {
	q := NewTxQueue()

	keyA, err := crypto.GenerateKey()
	require.NoError(t, err)
	keyB, err := crypto.GenerateKey()
	require.NoError(t, err)

	creationTx := types.NewContractCreation(1, big.NewInt(0), 100000, big.NewInt(1), []byte{1, 2, 3, 4})
	creationTx, err = types.SignTx(creationTx, types.LatestSignerForChainID(creationTx.ChainId()), keyA)
	require.NoError(t, err)

	token := common.HexToAddress("0x1111111111111111111111111111111111111111")
	recipient := common.HexToAddress("0x3333333333333333333333333333333333333333")
	transferTx := signedERC20TransferTx(t, keyB, 1, token, recipient)

	q.Enqueue(creationTx)
	q.Enqueue(transferTx)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, creationTx.Hash(), got.Hash())

	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, transferTx.Hash(), got.Hash())
}
