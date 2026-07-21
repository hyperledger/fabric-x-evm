/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonceStub returns a stubEndorser whose NonceAt yields nonce 0.
func nonceStub() *stubEndorser {
	return &stubEndorser{}
}

func TestSendTransaction_DuplicateRejected(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	g := &Gateway{
		ChainConfig: cfg,
		Signer:      signer,
		TxQueue:     NewTxQueue(),
		endorsers:   newClient(nonceStub()),
	}

	tx := newValidTx(t, key, validTxOpts{nonce: 0})

	require.NoError(t, g.SendTransaction(context.Background(), tx))

	err := g.SendTransaction(context.Background(), tx)
	require.ErrorIs(t, err, domain.ErrTransactionAlreadyPending)

	assert.NotNil(t, g.TxQueue.IsPending(tx.Hash()))
}

// The gateway's state readers forward straight to the endorsers.
func TestGateway_StateReadersDelegate(t *testing.T) {
	stub := &stubEndorser{
		balance: big.NewInt(123),
		storage: []byte{0x11, 0x22},
		code:    []byte{0x33, 0x44},
	}
	g := &Gateway{endorsers: newClient(stub)}
	ctx := context.Background()
	addr := ethcommon.Address{}

	bal, err := g.BalanceAt(ctx, addr, nil)
	require.NoError(t, err)
	assert.Equal(t, stub.balance, bal)

	stor, err := g.StorageAt(ctx, addr, ethcommon.Hash{}, nil)
	require.NoError(t, err)
	assert.Equal(t, stub.storage, stor)

	code, err := g.CodeAt(ctx, addr, nil)
	require.NoError(t, err)
	assert.Equal(t, stub.code, code)
}
