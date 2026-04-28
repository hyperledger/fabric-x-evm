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

// stateReader is the subset of ledger state needed to perform stateful
// pre-flight checks on a submitted transaction.
type stateReader interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// errUnprotectedTx mirrors geth's error from internal/ethapi.SubmitTransaction
// when a non-EIP-155 transaction is offered over RPC.
var errUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// txMaxSize matches the limit core/txpool/legacypool uses (4 * txSlotSize, 128KB).
// The constant is unexported in geth, so we redeclare it here.
const txMaxSize = 4 * 32 * 1024

// blockGasLimit is the synthetic block gas cap used for stateless validation.
// 30M is geth mainnet's traditional value and is well above any tx the gateway
// is expected to accept; the per-tx ceiling is enforced separately by
// params.MaxTxGas (Osaka rule, 16.7M) inside txpool.ValidateTransaction.
const blockGasLimit uint64 = 30_000_000

// acceptedTxTypes is the bitmap of transaction types eth_sendRawTransaction
// will accept. We deliberately exclude EIP-4844 blob transactions and EIP-7702
// set-code transactions; see docs/COMPATIBILITY.md for the rationale.
const acceptedTxTypes = (1 << types.LegacyTxType) | (1 << types.AccessListTxType) | (1 << types.DynamicFeeTxType)

// validateTx mirrors the validation that go-ethereum performs in
// internal/ethapi.SubmitTransaction together with core/txpool.ValidateTransaction
// and ValidateTransactionWithState. It calls those exported functions directly
// so the failure modes stay aligned with upstream geth as it evolves.
//
// Deliberate deviations are documented in docs/COMPATIBILITY.md.
func validateTx(
	ctx context.Context,
	tx *types.Transaction,
	chainConfig *params.ChainConfig,
	signer types.Signer,
	state stateReader,
) error {
	// internal/ethapi.SubmitTransaction rejects unprotected (pre-EIP-155)
	// transactions before the txpool sees them. Geth's signer code happily
	// recovers a sender from a Frontier-style signature, so this guard has to
	// live above ValidateTransaction.
	if !tx.Protected() {
		return errUnprotectedTx
	}

	head := &types.Header{
		Number:     new(big.Int),
		Time:       0,
		Difficulty: new(big.Int), // Sign() == 0 ⇒ post-merge, matches our chain config.
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

	from, err := types.Sender(signer, tx) // already validated above; recover for the state lookup
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
		State: sdb,
		// We do not maintain a mempool, so there are no pooled transactions to
		// account for. ExistingExpenditure must be set; ExistingCost may
		// return nil to signal "no replacement at this nonce". Replacement
		// transactions are tracked separately — see docs/COMPATIBILITY.md.
		ExistingExpenditure: func(common.Address) *big.Int { return new(big.Int) },
		ExistingCost:        func(common.Address, uint64) *big.Int { return nil },
	}
	return txpool.ValidateTransactionWithState(tx, signer, stateOpts)
}

// newEphemeralStateDB builds an in-memory *state.StateDB seeded with a single
// account (sender's nonce + balance). It exists so we can pass a real geth
// StateDB to txpool.ValidateTransactionWithState without coupling the gateway
// to the endorser's persistent state store.
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
