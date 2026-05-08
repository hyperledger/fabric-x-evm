/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"errors"
	"fmt"
	"testing"

	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestClassifyValidationError_NilReturnsNil(t *testing.T) {
	if err := classifyValidationError(nil); err != nil {
		t.Errorf("classifyValidationError(nil) = %v, want nil", err)
	}
}

func TestClassifyValidationError_NonceLookupIsInternal(t *testing.T) {
	underlying := errors.New("ledger unavailable")
	wrapped := fmt.Errorf("%w: %w", domain.ErrNonceLookup, underlying)

	got := classifyValidationError(wrapped)

	var rpcErr rpc.Error
	if !errors.As(got, &rpcErr) {
		t.Fatalf("classifier output must satisfy rpc.Error, got %T", got)
	}
	if rpcErr.ErrorCode() != rpcerr.CodeInternal {
		t.Errorf("code = %d, want %d (Internal)", rpcErr.ErrorCode(), rpcerr.CodeInternal)
	}
}

func TestClassifyValidationError_TxRejectionsMapToTxRejected(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"unprotected", domain.ErrUnprotectedTx},
		{"nonce too low", fmt.Errorf("%w: next nonce 5, tx nonce 1", ethcore.ErrNonceTooLow)},
		{"intrinsic gas", ethcore.ErrIntrinsicGas},
		{"insufficient funds", ethcore.ErrInsufficientFunds},
		{"unsupported tx type", ethcore.ErrTxTypeNotSupported},
		{"init code too large", ethcore.ErrMaxInitCodeSizeExceeded},
		{"invalid sender", fmt.Errorf("%w: bad sig", txpool.ErrInvalidSender)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyValidationError(c.err)

			var rpcErr rpc.Error
			if !errors.As(got, &rpcErr) {
				t.Fatalf("output must satisfy rpc.Error, got %T", got)
			}
			if rpcErr.ErrorCode() != rpcerr.CodeTxRejected {
				t.Errorf("code = %d, want %d (TxRejected)", rpcErr.ErrorCode(), rpcerr.CodeTxRejected)
			}
		})
	}
}

func TestClassifyValidationError_PreservesMessage(t *testing.T) {
	err := errors.New("nonce too low: next nonce 5, tx nonce 1")
	got := classifyValidationError(err)
	if got.Error() != err.Error() {
		t.Errorf("message = %q, want %q", got.Error(), err.Error())
	}
}
