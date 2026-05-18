/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
