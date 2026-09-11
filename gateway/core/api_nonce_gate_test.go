/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/stretchr/testify/require"
)

// errHandleQueue is a TxQueue whose Handle always fails, to exercise the
// block-handling error path.
type errHandleQueue struct {
	*TxQueue
	err error
}

func (q *errHandleQueue) Handle(context.Context, *domain.Block) error { return q.err }

func TestNew_InitializesNonceGate(t *testing.T) {
	// workerCount 0 and a nil queue exercise both constructor defaults.
	g, err := New(nil, nil, nil, testChainID, 0, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, g.nonceGate)
	require.NotNil(t, g.TxQueue)
	require.Equal(t, 1, g.workerCount)
}

func gatewayWithGate(t *testing.T) *Gateway {
	t.Helper()
	cfg, signer := chainCtx(t)
	g := &Gateway{
		ChainConfig: cfg,
		Signer:      signer,
		TxQueue:     NewTxQueue(),
		endorsers:   newClient(nonceStub()),
	}
	g.nonceGate = newNonceGate(g, g.Signer, g.TxQueue)
	return g
}

func TestSendTransaction_FutureNonceParked(t *testing.T) {
	key := newKey(t)
	g := gatewayWithGate(t)

	// Committed nonce is 0; a nonce-3 tx must be parked, not enqueued.
	tx := newValidTx(t, key, validTxOpts{nonce: 3})
	require.NoError(t, g.SendTransaction(context.Background(), tx))

	// Not in the worker queue...
	require.Nil(t, g.TxQueue.IsPending(tx.Hash()))

	// ...but reported as pending (parked) via TransactionByHash.
	dtx, err := g.TransactionByHash(context.Background(), tx.Hash())
	require.NoError(t, err)
	require.NotNil(t, dtx)
	require.Equal(t, uint64(0), dtx.BlockNumber) // 0 signals pending to the API layer
}

func TestHandle_EmptyBlockReleasesNothing(t *testing.T) {
	g := gatewayWithGate(t)
	require.NoError(t, g.Handle(context.Background(), blocks.Block{}))
}

func TestHandle_QueueErrorPropagates(t *testing.T) {
	cfg, signer := chainCtx(t)
	boom := errors.New("queue handle failed")
	g := &Gateway{
		ChainConfig: cfg,
		Signer:      signer,
		TxQueue:     &errHandleQueue{TxQueue: NewTxQueue(), err: boom},
		endorsers:   newClient(nonceStub()),
	}
	g.nonceGate = newNonceGate(g, g.Signer, g.TxQueue)

	require.ErrorIs(t, g.Handle(context.Background(), blocks.Block{}), boom)
}

// A resubmission of a parked transaction is rejected by the gate's own index,
// not the queue's, since a parked tx never reaches the queue.
func TestSendTransaction_ParkedResubmissionRejected(t *testing.T) {
	key := newKey(t)
	g := gatewayWithGate(t)

	tx := newValidTx(t, key, validTxOpts{nonce: 3})
	require.NoError(t, g.SendTransaction(context.Background(), tx))
	require.Nil(t, g.TxQueue.IsPending(tx.Hash()), "parked, so not in the queue")

	err := g.SendTransaction(context.Background(), tx)
	require.ErrorIs(t, err, domain.ErrTransactionAlreadyPending)
}

// A supplied sequencer replaces the default gate instead of being wrapped by it.
func TestWithNonceSequencer_ReplacesDefaultGate(t *testing.T) {
	supplied := &countingSequencer{}

	g, err := New(nil, nil, nil, testChainID, 1, nil, nil, WithNonceSequencer(supplied))
	require.NoError(t, err)
	require.Same(t, supplied, g.nonceGate)

	key := newKey(t)
	cfg, signer := chainCtx(t)
	g.ChainConfig, g.Signer = cfg, signer
	require.NoError(t, g.SendTransaction(context.Background(), newValidTx(t, key, validTxOpts{nonce: 9})))
	require.Equal(t, 1, supplied.admits)
}

// countingSequencer records what the gateway asked of it.
type countingSequencer struct {
	admits int
}

func (s *countingSequencer) Admit(context.Context, *types.Transaction) error {
	s.admits++
	return nil
}
func (s *countingSequencer) Observe([]domain.Transaction)             {}
func (s *countingSequencer) IsPending(common.Hash) *types.Transaction { return nil }

// Pre-flight validation runs before the gate, so a rejected transaction never
// reaches the queue or the parked set.
func TestSendTransaction_ValidationRejectsBeforeGate(t *testing.T) {
	key := newKey(t)
	g := gatewayWithGate(t)

	// Zero gas fails the txpool intrinsic-gas check.
	tx := newValidTx(t, key, validTxOpts{nonce: 0, gas: 1})
	require.Error(t, g.SendTransaction(context.Background(), tx))
	require.Nil(t, g.TxQueue.IsPending(tx.Hash()))
	require.Nil(t, g.nonceGate.IsPending(tx.Hash()))
}
