/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	"github.com/stretchr/testify/require"
)

func newTx(nonce uint64) *types.Transaction {
	to := common.HexToAddress("0x1")
	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		Gas:       21000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		To:        &to,
	})
}

// Out-of-order nonces all reach the queue: the test backend parks nothing, so a
// primed or reverted ledger nonce can never leave it stale.
func TestPassthroughGate_AdmitsEveryNonce(t *testing.T) {
	q := core.NewTxQueue()
	gate := testimpl.NewPassthroughGate(q)

	for _, nonce := range []uint64{7, 5, 6} {
		require.NoError(t, gate.Admit(context.Background(), newTx(nonce)))
	}

	require.Equal(t, 3, q.InFlight())
	for _, want := range []uint64{7, 5, 6} {
		tx, ok := q.Dequeue()
		require.True(t, ok)
		require.Equal(t, want, tx.Nonce())
	}
}

// Nothing is ever parked, so the queue alone answers IsPending and Observe has
// no work to do.
func TestPassthroughGate_NothingParked(t *testing.T) {
	q := core.NewTxQueue()
	gate := testimpl.NewPassthroughGate(q)
	tx := newTx(5)

	require.NoError(t, gate.Admit(context.Background(), tx))
	require.Nil(t, gate.IsPending(tx.Hash()))

	gate.Observe([]domain.Transaction{})
	require.Equal(t, 1, q.InFlight())
}
