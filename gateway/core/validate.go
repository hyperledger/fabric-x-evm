/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// stateReader is the subset of ledger state needed to perform stateful
// pre-flight checks on a submitted transaction.
type stateReader interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// errUnprotectedTx mirrors geth's error from internal/ethapi.SubmitTransaction
// when a non-EIP-155 transaction is offered over RPC.
var errUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// validateTx mirrors the validation that go-ethereum performs in
// internal/ethapi.SubmitTransaction together with core/txpool.ValidateTransaction
// and ValidateTransactionWithState, so that eth_sendRawTransaction returns the
// same class of errors that an Ethereum client expects. Stateful checks
// (nonce, balance) are evaluated against the latest committed state.
func validateTx(
	ctx context.Context,
	tx *types.Transaction,
	chainID *big.Int,
	chainConfig *params.ChainConfig,
	signer types.Signer,
	state stateReader,
) error {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType:
	default:
		return fmt.Errorf("%w: tx type %d", ethcore.ErrTxTypeNotSupported, tx.Type())
	}

	if !tx.Protected() {
		return errUnprotectedTx
	}

	if tx.ChainId().Cmp(chainID) != 0 {
		return fmt.Errorf("invalid chain id: have %s, want %s", tx.ChainId(), chainID)
	}

	if tx.Value().Sign() < 0 {
		return txpool.ErrNegativeValue
	}

	if tx.GasFeeCap().BitLen() > 256 {
		return ethcore.ErrFeeCapVeryHigh
	}
	if tx.GasTipCap().BitLen() > 256 {
		return ethcore.ErrTipVeryHigh
	}
	if tx.GasFeeCapIntCmp(tx.GasTipCap()) < 0 {
		return ethcore.ErrTipAboveFeeCap
	}

	from, err := types.Sender(signer, tx)
	if err != nil {
		return fmt.Errorf("%w: %v", txpool.ErrInvalidSender, err)
	}

	if tx.Nonce()+1 < tx.Nonce() {
		return ethcore.ErrNonceMax
	}

	// All forks in our chain config activate at genesis, so any (block, time)
	// argument yields the same rule set.
	rules := chainConfig.Rules(common.Big0, true, 0)

	if rules.IsShanghai && tx.To() == nil && len(tx.Data()) > params.MaxInitCodeSize {
		return fmt.Errorf("%w: code size %d, limit %d", ethcore.ErrMaxInitCodeSizeExceeded, len(tx.Data()), params.MaxInitCodeSize)
	}

	intrGas, err := ethcore.IntrinsicGas(tx.Data(), tx.AccessList(), tx.SetCodeAuthorizations(), tx.To() == nil, true, rules.IsIstanbul, rules.IsShanghai)
	if err != nil {
		return err
	}
	if tx.Gas() < intrGas {
		return fmt.Errorf("%w: gas %d, minimum needed %d", ethcore.ErrIntrinsicGas, tx.Gas(), intrGas)
	}

	nonce, err := state.NonceAt(ctx, from, nil)
	if err != nil {
		return fmt.Errorf("look up nonce: %w", err)
	}
	if nonce > tx.Nonce() {
		return fmt.Errorf("%w: next nonce %d, tx nonce %d", ethcore.ErrNonceTooLow, nonce, tx.Nonce())
	}

	balance, err := state.BalanceAt(ctx, from, nil)
	if err != nil {
		return fmt.Errorf("look up balance: %w", err)
	}
	cost := tx.Cost()
	if balance.Cmp(cost) < 0 {
		return fmt.Errorf("%w: balance %s, tx cost %s, overshot %s", ethcore.ErrInsufficientFunds, balance, cost, new(big.Int).Sub(cost, balance))
	}

	return nil
}
