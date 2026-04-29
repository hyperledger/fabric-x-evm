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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/require"
)

// stubStore is a Store with overridable lookups; everything else is a no-op.
type stubStore struct {
	getTxFn   func(ctx context.Context, hash []byte) (*domain.Transaction, error)
	getLogsFn func(ctx context.Context, txHash []byte) ([]domain.Log, error)
}

func (s *stubStore) BlockNumber(context.Context) (uint64, error)              { return 0, nil }
func (s *stubStore) LatestBlock(context.Context, bool) (*domain.Block, error) { return nil, nil }
func (s *stubStore) GetBlockByNumber(context.Context, uint64, bool) (*domain.Block, error) {
	return nil, nil
}
func (s *stubStore) GetBlockByHash(context.Context, []byte, bool) (*domain.Block, error) {
	return nil, nil
}
func (s *stubStore) GetBlockTxCountByHash(context.Context, []byte) (int64, error)   { return 0, nil }
func (s *stubStore) GetBlockTxCountByNumber(context.Context, uint64) (int64, error) { return 0, nil }
func (s *stubStore) GetTransactionByHash(ctx context.Context, hash []byte) (*domain.Transaction, error) {
	if s.getTxFn != nil {
		return s.getTxFn(ctx, hash)
	}
	return nil, nil
}
func (s *stubStore) GetTransactionByBlockHashAndIndex(context.Context, []byte, int64) (*domain.Transaction, error) {
	return nil, nil
}
func (s *stubStore) GetTransactionByBlockNumberAndIndex(context.Context, uint64, int64) (*domain.Transaction, error) {
	return nil, nil
}
func (s *stubStore) GetLogs(context.Context, domain.LogFilter) ([]domain.Log, error) { return nil, nil }
func (s *stubStore) GetLogsByTxHash(ctx context.Context, txHash []byte) ([]domain.Log, error) {
	if s.getLogsFn != nil {
		return s.getLogsFn(ctx, txHash)
	}
	return nil, nil
}

func newTestGateway(t *testing.T, store Store) *Gateway {
	t.Helper()
	gw, err := New(nil, nil, store, testChainID, 1)
	require.NoError(t, err)
	return gw
}

func signedTx(t *testing.T, key *ecdsa.PrivateKey, to *common.Address) *types.Transaction {
	t.Helper()
	var inner *types.LegacyTx
	if to != nil {
		inner = &types.LegacyTx{Nonce: 7, To: to, Gas: 21_000, GasPrice: big.NewInt(1), Value: big.NewInt(0)}
	} else {
		inner = &types.LegacyTx{Nonce: 7, Gas: 60_000, GasPrice: big.NewInt(1), Data: []byte{0x60}}
	}
	signed, err := types.SignTx(types.NewTx(inner), types.NewEIP155Signer(big.NewInt(testChainID)), key)
	require.NoError(t, err)
	return signed
}

func TestTransactionByHash_PendingThenCommitted(t *testing.T) {
	store := &stubStore{}
	gw := newTestGateway(t, store)

	key := newKey(t)
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tx := signedTx(t, key, &to)
	from := crypto.PubkeyToAddress(key.PublicKey)

	gw.pendingTxs.Store(tx.Hash(), pendingTx{tx: tx, from: from})

	domTx, isPending, err := gw.TransactionByHash(context.Background(), tx.Hash())
	require.NoError(t, err)
	require.True(t, isPending)
	require.NotNil(t, domTx)
	require.Equal(t, tx.Hash().Bytes(), domTx.TxHash)
	require.Equal(t, from.Bytes(), domTx.FromAddress)
	require.Equal(t, to.Bytes(), domTx.ToAddress)
	require.NotEmpty(t, domTx.RawTx)

	// Simulate commit: store now returns the tx → isPending must flip and the
	// pending entry must be cleaned up.
	committed := &domain.Transaction{
		TxHash:      tx.Hash().Bytes(),
		BlockNumber: 42,
		BlockHash:   []byte("block-hash"),
		Status:      1,
		FromAddress: from.Bytes(),
		ToAddress:   to.Bytes(),
	}
	store.getTxFn = func(_ context.Context, _ []byte) (*domain.Transaction, error) {
		return committed, nil
	}

	domTx, isPending, err = gw.TransactionByHash(context.Background(), tx.Hash())
	require.NoError(t, err)
	require.False(t, isPending)
	require.Equal(t, committed, domTx)

	_, stillPending := gw.pendingTxs.Load(tx.Hash())
	require.False(t, stillPending, "pending entry should be cleared once committed")
}

func TestTransactionByHash_DeployPending(t *testing.T) {
	gw := newTestGateway(t, &stubStore{})

	key := newKey(t)
	tx := signedTx(t, key, nil) // deploy
	from := crypto.PubkeyToAddress(key.PublicKey)

	gw.pendingTxs.Store(tx.Hash(), pendingTx{tx: tx, from: from})

	domTx, isPending, err := gw.TransactionByHash(context.Background(), tx.Hash())
	require.NoError(t, err)
	require.True(t, isPending)
	require.NotNil(t, domTx)
	require.Empty(t, domTx.ToAddress)
	require.Equal(t, crypto.CreateAddress(from, tx.Nonce()).Bytes(), domTx.ContractAddress)
}

func TestTransactionByHash_Unknown(t *testing.T) {
	gw := newTestGateway(t, &stubStore{})

	domTx, isPending, err := gw.TransactionByHash(context.Background(), common.Hash{0xde, 0xad})
	require.NoError(t, err)
	require.Nil(t, domTx)
	require.False(t, isPending)
}
