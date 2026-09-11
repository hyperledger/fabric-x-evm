/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

type stateReader interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
}

var errUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// txMaxSize redeclares the unexported core/txpool/legacypool constant (4 * 32 KiB).
const txMaxSize = 4 * 32 * 1024

const blockGasLimit uint64 = math.MaxUint64

const acceptedTxTypes = (1 << types.LegacyTxType) | (1 << types.AccessListTxType) | (1 << types.DynamicFeeTxType)

// ValidateTx delegates stateless checks to geth's txpool.ValidateTransaction so
// the failure model tracks upstream. Nonce sequencing is stateful and lives in
// the nonce gate, not here. Deviations are documented in docs/COMPATIBILITY.md.
func ValidateTx(
	tx *types.Transaction,
	chainConfig *params.ChainConfig,
	signer types.Signer,
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

	if _, err := types.Sender(signer, tx); err != nil {
		return fmt.Errorf("%w: %v", txpool.ErrInvalidSender, err)
	}
	return nil
}
