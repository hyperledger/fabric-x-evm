/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/stretchr/testify/require"
)

const testChainID int64 = 4011

type fakeState struct {
	nonce    uint64
	nonceErr error
}

func (f *fakeState) NonceAt(_ context.Context, _ common.Address, _ *big.Int) (uint64, error) {
	return f.nonce, f.nonceErr
}

type validTxOpts struct {
	nonce uint64
	gas   uint64
	value *big.Int
	to    *common.Address
	data  []byte
}

func newValidTx(t *testing.T, key *ecdsa.PrivateKey, opts validTxOpts) *types.Transaction {
	t.Helper()
	if opts.gas == 0 {
		opts.gas = 21_000
	}
	if opts.value == nil {
		opts.value = big.NewInt(0)
	}
	to := opts.to
	if to == nil {
		addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
		to = &addr
	}
	tx := types.NewTransaction(opts.nonce, *to, opts.value, opts.gas, big.NewInt(1), opts.data)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(testChainID)), key)
	require.NoError(t, err)
	return signed
}

func chainCtx(t *testing.T) (*params.ChainConfig, types.Signer) {
	t.Helper()
	return cmn.BuildChainConfig(testChainID), types.LatestSignerForChainID(big.NewInt(testChainID))
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	return key
}

func TestValidateTx_Valid(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	tx := newValidTx(t, key, validTxOpts{nonce: 5})
	state := &fakeState{nonce: 5}

	require.NoError(t, ValidateTx(context.Background(), tx, cfg, signer, state))
}

func TestValidateTx_UnprotectedRejected(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	raw := types.NewTransaction(0, to, big.NewInt(0), 21_000, big.NewInt(1), nil)
	tx, err := types.SignTx(raw, types.HomesteadSigner{}, key) // pre-EIP-155 → unprotected
	require.NoError(t, err)

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, errUnprotectedTx)
}

func TestValidateTx_ChainIDMismatch(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	raw := types.NewTransaction(0, to, big.NewInt(0), 21_000, big.NewInt(1), nil)
	tx, err := types.SignTx(raw, types.NewEIP155Signer(big.NewInt(testChainID+1)), key)
	require.NoError(t, err)

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, txpool.ErrInvalidSender)
}

func TestValidateTx_TipAboveFeeCap(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	raw := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(testChainID),
		Nonce:     0,
		GasTipCap: big.NewInt(10),
		GasFeeCap: big.NewInt(5),
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	tx, err := types.SignTx(raw, signer, key)
	require.NoError(t, err)

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, ethcore.ErrTipAboveFeeCap)
}

func TestValidateTx_IntrinsicGasTooLow(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	tx := newValidTx(t, key, validTxOpts{gas: 20_000}) // below 21_000
	err := ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, ethcore.ErrIntrinsicGas)
}

func TestValidateTx_NonceTooLow(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	tx := newValidTx(t, key, validTxOpts{nonce: 3})
	state := &fakeState{nonce: 7}

	err := ValidateTx(context.Background(), tx, cfg, signer, state)
	require.ErrorIs(t, err, ethcore.ErrNonceTooLow)
}

func TestValidateTx_InitCodeTooLarge(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	oversized := make([]byte, params.MaxInitCodeSize+1)
	raw := types.NewContractCreation(0, big.NewInt(0), 30_000_000, big.NewInt(1), oversized)
	tx, err := types.SignTx(raw, types.NewEIP155Signer(big.NewInt(testChainID)), key)
	require.NoError(t, err)

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{nonce: 0})
	require.ErrorIs(t, err, ethcore.ErrMaxInitCodeSizeExceeded)
}

func TestValidateTx_BlobTxTypeRejected(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	raw := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(big.NewInt(testChainID)),
		Nonce:      0,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(1),
		Gas:        21_000,
		To:         to,
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: []common.Hash{{}},
	})
	tx, err := types.SignTx(raw, signer, key)
	require.NoError(t, err)

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, ethcore.ErrTxTypeNotSupported)
}

func TestValidateTx_NegativeValue(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	raw := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(testChainID),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(-1),
	})
	tx, err := types.SignTx(raw, signer, key)
	if err != nil {
		t.Skip("signer rejects negative value at sign time:", err)
	}

	err = ValidateTx(context.Background(), tx, cfg, signer, &fakeState{})
	require.ErrorIs(t, err, txpool.ErrNegativeValue)
}

func TestValidateTx_StateLookupErrorPropagates(t *testing.T) {
	key := newKey(t)
	cfg, signer := chainCtx(t)

	tx := newValidTx(t, key, validTxOpts{nonce: 0})
	boom := errors.New("ledger unavailable")
	state := &fakeState{nonceErr: boom}

	err := ValidateTx(context.Background(), tx, cfg, signer, state)
	require.ErrorIs(t, err, boom)
}
