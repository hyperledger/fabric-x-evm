/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"testing"

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
