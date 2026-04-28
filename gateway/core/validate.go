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
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

type stateReader interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

var errUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// txMaxSize redeclares the unexported core/txpool/legacypool constant (4 * 32 KiB).
const txMaxSize = 4 * 32 * 1024

const blockGasLimit uint64 = 30_000_000

const acceptedTxTypes = (1 << types.LegacyTxType) | (1 << types.AccessListTxType) | (1 << types.DynamicFeeTxType)

// validateTx delegates to geth's exported txpool helpers so the failure model
// stays aligned with upstream. Deviations are documented in docs/COMPATIBILITY.md.
func validateTx(
	ctx context.Context,
	tx *types.Transaction,
	chainConfig *params.ChainConfig,
	signer types.Signer,
	state stateReader,
) error {
	// Geth rejects this in internal/ethapi.SubmitTransaction, above the txpool —
	// the txpool's signer recovery accepts Frontier-style signatures.
	if !tx.Protected() {
		return errUnprotectedTx
	}

	head := &types.Header{
		Number:     new(big.Int),
		Time:       0,
		Difficulty: new(big.Int), // Sign() == 0 ⇒ post-merge.
		GasLimit:   blockGasLimit,
	}
	opts := &txpool.ValidationOptions{
		Config:       chainConfig,
		Accept:       acceptedTxTypes,
		MaxSize:      txMaxSize,
		MaxBlobCount: 0,
		MinTip:       new(big.Int),
	}
	if err := txpool.ValidateTransaction(tx, head, signer, opts); err != nil {
		return err
	}

	from, err := types.Sender(signer, tx)
	if err != nil {
		return fmt.Errorf("%w: %v", txpool.ErrInvalidSender, err)
	}

	nonce, err := state.NonceAt(ctx, from, nil)
	if err != nil {
		return fmt.Errorf("look up nonce: %w", err)
	}
	balance, err := state.BalanceAt(ctx, from, nil)
	if err != nil {
		return fmt.Errorf("look up balance: %w", err)
	}

	sdb, err := newEphemeralStateDB(from, nonce, balance)
	if err != nil {
		return fmt.Errorf("build state for validation: %w", err)
	}

	stateOpts := &txpool.ValidationOptionsWithState{
		State:               sdb,
		ExistingExpenditure: func(common.Address) *big.Int { return new(big.Int) },
		ExistingCost:        func(common.Address, uint64) *big.Int { return nil },
	}
	return txpool.ValidateTransactionWithState(tx, signer, stateOpts)
}

// newEphemeralStateDB lets us hand a real *state.StateDB to
// txpool.ValidateTransactionWithState without sharing the endorser's store.
func newEphemeralStateDB(addr common.Address, nonce uint64, balance *big.Int) (*state.StateDB, error) {
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, nil))
	if err != nil {
		return nil, err
	}
	sdb.SetNonce(addr, nonce, tracing.NonceChangeUnspecified)
	if balance == nil {
		balance = new(big.Int)
	}
	bal, overflow := uint256.FromBig(balance)
	if overflow {
		return nil, fmt.Errorf("balance %s exceeds 256 bits", balance)
	}
	sdb.SetBalance(addr, bal, tracing.BalanceChangeUnspecified)
	return sdb, nil
}
