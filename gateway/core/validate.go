/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

type stateReader interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
}

// txMaxSize redeclares the unexported core/txpool/legacypool constant (4 * 32 KiB).
const txMaxSize = 4 * 32 * 1024

const blockGasLimit uint64 = math.MaxUint64

const acceptedTxTypes = (1 << types.LegacyTxType) | (1 << types.AccessListTxType) | (1 << types.DynamicFeeTxType)

// ValidateTx delegates stateless checks to geth's txpool.ValidateTransaction so
// the failure model tracks upstream. The only stateful check is nonce-too-low,
// inlined from txpool.ValidateTransactionWithState to avoid building a per-tx
// StateDB. Deviations are documented in docs/COMPATIBILITY.md.
func ValidateTx(
	ctx context.Context,
	tx *types.Transaction,
	chainConfig *params.ChainConfig,
	signer types.Signer,
	state stateReader,
) error {
	// Geth rejects this in internal/ethapi.SubmitTransaction, above the txpool —
	// the txpool's signer recovery accepts Frontier-style signatures.
	if !tx.Protected() {
		return domain.ErrUnprotectedTx
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
		return fmt.Errorf("%w: %w", domain.ErrNonceLookup, err)
	}
	if nonce > tx.Nonce() {
		return fmt.Errorf("%w: next nonce %d, tx nonce %d", ethcore.ErrNonceTooLow, nonce, tx.Nonce())
	}
	return nil
}
