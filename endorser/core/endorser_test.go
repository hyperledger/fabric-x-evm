/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

func TestResponseStatusOK(t *testing.T) {
	resp := response([]byte{0xde, 0xad}, nil)

	if resp.Response.Status != common.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusOK)
	}
}

func TestResponseStatusEVMRevert(t *testing.T) {
	resp := response(nil, vm.ErrExecutionReverted)

	if resp.Response.Status != common.StatusEVMRevert {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusEVMRevert)
	}
}

func TestResponseStatusExecFailure(t *testing.T) {
	// A valid tx whose execution failed is tagged *execution.ExecFailure.
	resp := response(nil, execution.NewExecFailure(vm.ErrOutOfGas))

	if resp.Response.Status != common.StatusExecFailure {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusExecFailure)
	}
}

func TestResponseStatusTxRejected(t *testing.T) {
	// An invalid tx rejected before execution is tagged *execution.TxRejected.
	resp := response(nil, execution.NewTxRejected(gethcore.ErrNonceTooLow))

	if resp.Response.Status != common.StatusTxRejected {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusTxRejected)
	}
}

func TestResponseStatusServerError(t *testing.T) {
	resp := response(nil, errors.New("backend unavailable"))

	if resp.Response.Status != common.StatusServerError {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusServerError)
	}
}

// stubEngine is an EVMEngineInterface whose Execute returns a fixed error, so we
// can drive ProcessEVMTransaction's failure classification.
type stubEngine struct {
	execErr error
}

func (s *stubEngine) Execute(context.Context, *types.Transaction) (endorsement.ExecutionResult, error) {
	return endorsement.ExecutionResult{}, s.execErr
}
func (s *stubEngine) Call(ethereum.CallMsg, *big.Int) ([]byte, error) { return nil, nil }
func (s *stubEngine) BalanceAt(context.Context, ethcommon.Address, *big.Int) (*big.Int, error) {
	return nil, nil
}
func (s *stubEngine) StorageAt(context.Context, ethcommon.Address, ethcommon.Hash, *big.Int) ([]byte, error) {
	return nil, nil
}
func (s *stubEngine) CodeAt(context.Context, ethcommon.Address, *big.Int) ([]byte, error) {
	return nil, nil
}
func (s *stubEngine) NonceAt(context.Context, ethcommon.Address, *big.Int) (uint64, error) {
	return 0, nil
}

func processEVMTxWithEngineErr(t *testing.T, execErr error) *peer.ProposalResponse {
	t.Helper()

	// The stub engine ignores the tx, so it need not be signed.
	f := &Endorser{Engine: &stubEngine{execErr: execErr}}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	resp, err := f.ProcessEVMTransaction(context.Background(), endorsement.Invocation{}, tx)
	if err != nil {
		t.Fatalf("ProcessEVMTransaction must encode the failure in the response, got Go error: %v", err)
	}
	return resp
}

// A valid tx whose execution failed surfaces as StatusExecFailure (endorsable).
func TestProcessEVMTransaction_ExecFailure(t *testing.T) {
	resp := processEVMTxWithEngineErr(t, execution.NewExecFailure(vm.ErrOutOfGas))

	if resp.Response.Status != common.StatusExecFailure {
		t.Fatalf("status = %d, want %d (StatusExecFailure)", resp.Response.Status, common.StatusExecFailure)
	}
}

// An invalid tx surfaces as StatusTxRejected so the caller is told to fix it.
func TestProcessEVMTransaction_TxRejected(t *testing.T) {
	resp := processEVMTxWithEngineErr(t, execution.NewTxRejected(gethcore.ErrNonceTooLow))

	if resp.Response.Status != common.StatusTxRejected {
		t.Fatalf("status = %d, want %d (StatusTxRejected)", resp.Response.Status, common.StatusTxRejected)
	}
}

// An untagged error is an infrastructure failure on our side and must surface as
// StatusServerError (500); CreateSignedTx then refuses to package it.
func TestProcessEVMTransaction_InfraErrorIs500(t *testing.T) {
	resp := processEVMTxWithEngineErr(t, errors.New("open snapshot: db unavailable"))

	if resp.Response.Status != common.StatusServerError {
		t.Fatalf("status = %d, want %d (StatusServerError)", resp.Response.Status, common.StatusServerError)
	}
}
