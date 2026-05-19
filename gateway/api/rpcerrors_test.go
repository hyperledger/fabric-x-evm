/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestNilBlockNum(t *testing.T) {
	nonce := uint64(0)
	to := common.HexToAddress("0xcafe")
	value := big.NewInt(1e18)
	gasLimit := uint64(21000)
	maxFeePerGas := big.NewInt(20000000000)
	maxPriorityFeePerGas := big.NewInt(2000000000)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasFeeCap: maxFeePerGas,
		GasTipCap: maxPriorityFeePerGas,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
		Data:      nil,
	})

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("HexToECDSA failed: %s", err)
	}

	rawTx, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %s", err)
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	r := receipt(&domain.Transaction{
		TxHash:      tx.Hash().Bytes(),
		BlockHash:   nil, // nil signals pending to API layer
		BlockNumber: 0,   // 0 signals pending to API layer
		TxIndex:     0,   // Value doesn't matter - API layer checks BlockNumber==0 for pending
		RawTx:       rawTx,
		FromAddress: fromAddress.Bytes(),
		ToAddress:   to.Bytes(),
	})

	if r != nil {
		t.Fatalf("Expected nil receipt, got %v", r)
	}
}

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

func TestClassifyCallError_NilReturnsNil(t *testing.T) {
	if err := classifyCallError(nil); err != nil {
		t.Errorf("classifyCallError(nil) = %v, want nil", err)
	}
}

func TestClassifyCallError_RevertMapsToExecutionReverted(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	got := classifyCallError(&domain.RevertError{
		Reason: "execution reverted: out of stock",
		Data:   payload,
	})

	var rpcErr rpc.Error
	if !errors.As(got, &rpcErr) {
		t.Fatalf("output must satisfy rpc.Error, got %T", got)
	}
	if rpcErr.ErrorCode() != rpcerr.CodeExecutionReverted {
		t.Errorf("code = %d, want %d (ExecutionReverted)", rpcErr.ErrorCode(), rpcerr.CodeExecutionReverted)
	}
	if rpcErr.Error() != "execution reverted: out of stock" {
		t.Errorf("message = %q", rpcErr.Error())
	}

	var dataErr rpc.DataError
	if !errors.As(got, &dataErr) {
		t.Fatalf("revert must satisfy rpc.DataError, got %T", got)
	}
	if dataErr.ErrorData() != "0x08c379a0deadbeef" {
		t.Errorf("ErrorData() = %v, want 0x08c379a0deadbeef", dataErr.ErrorData())
	}
}

func TestClassifyCallError_WrappedRevertStillMatches(t *testing.T) {
	wrapped := fmt.Errorf("call: %w", &domain.RevertError{Reason: "execution reverted"})

	var rpcErr rpc.Error
	if !errors.As(classifyCallError(wrapped), &rpcErr) {
		t.Fatalf("wrapped revert must satisfy rpc.Error")
	}
	if rpcErr.ErrorCode() != rpcerr.CodeExecutionReverted {
		t.Errorf("code = %d, want %d", rpcErr.ErrorCode(), rpcerr.CodeExecutionReverted)
	}
}

func TestClassifyCallError_NonRevertIsInternal(t *testing.T) {
	got := classifyCallError(errors.New("endorser unreachable"))

	var rpcErr rpc.Error
	if !errors.As(got, &rpcErr) {
		t.Fatalf("output must satisfy rpc.Error, got %T", got)
	}
	if rpcErr.ErrorCode() != rpcerr.CodeInternal {
		t.Errorf("code = %d, want %d (Internal)", rpcErr.ErrorCode(), rpcerr.CodeInternal)
	}
}
