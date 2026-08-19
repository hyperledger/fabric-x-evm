/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"math"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubStore is a minimal Store fake that records the block number it was actually
// queried with, so tests can verify sentinel resolution happens before the store is hit.
type stubStore struct {
	blockNumber     uint64
	txCountByNumber map[uint64]int64
	txCountArg      *uint64
	txByNumberArg   *uint64
}

func (s *stubStore) BlockNumber(ctx context.Context) (uint64, error) { return s.blockNumber, nil }
func (s *stubStore) BlockNumberByHash(ctx context.Context, hash []byte) (*uint64, error) {
	return nil, nil
}
func (s *stubStore) LatestBlock(ctx context.Context, full bool) (*domain.Block, error) {
	return nil, nil
}
func (s *stubStore) GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error) {
	return nil, nil
}
func (s *stubStore) GetBlockByHash(ctx context.Context, hash []byte, full bool) (*domain.Block, error) {
	return nil, nil
}
func (s *stubStore) GetBlockTxCountByHash(ctx context.Context, hash []byte) (int64, error) {
	return 0, nil
}
func (s *stubStore) GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error) {
	s.txCountArg = &num
	return s.txCountByNumber[num], nil
}
func (s *stubStore) GetTransactionByHash(ctx context.Context, hash []byte) (*domain.Transaction, error) {
	return nil, nil
}
func (s *stubStore) GetTransactionByBlockHashAndIndex(ctx context.Context, hash []byte, idx int64) (*domain.Transaction, error) {
	return nil, nil
}
func (s *stubStore) GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error) {
	s.txByNumberArg = &num
	return nil, nil
}
func (s *stubStore) GetLogs(ctx context.Context, filter domain.LogFilter) ([]domain.Log, error) {
	return nil, nil
}
func (s *stubStore) GetLogsByTxHash(ctx context.Context, txHash []byte) ([]domain.Log, error) {
	return nil, nil
}

var _ Store = (*stubStore)(nil)

// TestGetBlockTxCountByNumber_LatestSentinelResolvesToCurrentBlock guards against a bug
// found alongside the eth_getLogs "latest" bug: GetBlockByNumber special-cases
// math.MaxUint64 ("latest") by resolving it to the current block number, but
// GetBlockTxCountByNumber forwarded it to the store unresolved, where int64(MaxUint64)
// wraps to -1 and silently matches no row.
func TestGetBlockTxCountByNumber_LatestSentinelResolvesToCurrentBlock(t *testing.T) {
	store := &stubStore{blockNumber: 42, txCountByNumber: map[uint64]int64{42: 7}}
	g := &Gateway{store: store}

	count, err := g.GetBlockTxCountByNumber(context.Background(), math.MaxUint64)
	require.NoError(t, err)
	assert.Equal(t, int64(7), count)
	require.NotNil(t, store.txCountArg)
	assert.Equal(t, uint64(42), *store.txCountArg)
}

// TestGetTransactionByBlockNumberAndIndex_LatestSentinelResolvesToCurrentBlock is the
// GetTransactionByBlockNumberAndIndex counterpart of
// TestGetBlockTxCountByNumber_LatestSentinelResolvesToCurrentBlock.
func TestGetTransactionByBlockNumberAndIndex_LatestSentinelResolvesToCurrentBlock(t *testing.T) {
	store := &stubStore{blockNumber: 42}
	g := &Gateway{store: store}

	_, err := g.GetTransactionByBlockNumberAndIndex(context.Background(), math.MaxUint64, 0)
	require.NoError(t, err)
	require.NotNil(t, store.txByNumberArg)
	assert.Equal(t, uint64(42), *store.txByNumberArg)
}

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
