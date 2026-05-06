/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
