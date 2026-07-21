/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"bytes"
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

// stubEngine is an EVMEngineInterface whose methods return fixed values, so we
// can drive the endorser's classification and state-reader delegation.
type stubEngine struct {
	execErr     error
	callPayload []byte
	callErr     error
	balance     *big.Int
	storage     []byte
	code        []byte
	nonce       uint64
}

func (s *stubEngine) Execute(context.Context, *types.Transaction) (endorsement.ExecutionResult, error) {
	return endorsement.ExecutionResult{}, s.execErr
}
func (s *stubEngine) Call(ethereum.CallMsg, *big.Int) ([]byte, error) {
	return s.callPayload, s.callErr
}
func (s *stubEngine) BalanceAt(context.Context, ethcommon.Address, *big.Int) (*big.Int, error) {
	return s.balance, nil
}
func (s *stubEngine) StorageAt(context.Context, ethcommon.Address, ethcommon.Hash, *big.Int) ([]byte, error) {
	return s.storage, nil
}
func (s *stubEngine) CodeAt(context.Context, ethcommon.Address, *big.Int) ([]byte, error) {
	return s.code, nil
}
func (s *stubEngine) NonceAt(context.Context, ethcommon.Address, *big.Int) (uint64, error) {
	return s.nonce, nil
}

// stubBuilder is an endorsement.Builder returning a fixed response, so we can
// drive Execute's success and endorse-failure paths.
type stubBuilder struct {
	resp *peer.ProposalResponse
	err  error
}

func (b *stubBuilder) Endorse(endorsement.Invocation, endorsement.ExecutionResult) (*peer.ProposalResponse, error) {
	return b.resp, b.err
}

func processEVMTxWithEngineErr(t *testing.T, execErr error) *peer.ProposalResponse {
	t.Helper()

	// The stub engine ignores the tx, so it need not be signed.
	f := &Endorser{Engine: &stubEngine{execErr: execErr}}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	resp, err := f.Execute(context.Background(), endorsement.Invocation{}, tx)
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

// Call surfaces an EVM revert as a *common.CallError carrying the revert status
// and the returned payload.
func TestCall_Revert(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0}
	f := &Endorser{Engine: &stubEngine{callPayload: payload, callErr: vm.ErrExecutionReverted}}

	got, err := f.Call(context.Background(), &ethereum.CallMsg{}, nil)

	var callErr *common.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("expected *common.CallError, got %T (%v)", err, err)
	}
	if callErr.Status != common.StatusEVMRevert {
		t.Errorf("Status = %d, want %d", callErr.Status, common.StatusEVMRevert)
	}
	if !bytes.Equal(callErr.Data, payload) || !bytes.Equal(got, payload) {
		t.Errorf("payload: CallError.Data = %x, returned = %x, want %x", callErr.Data, got, payload)
	}
}

// A valid call whose execution failed is classified StatusExecFailure.
func TestCall_ExecFailure(t *testing.T) {
	f := &Endorser{Engine: &stubEngine{callErr: execution.NewExecFailure(vm.ErrOutOfGas)}}

	_, err := f.Call(context.Background(), &ethereum.CallMsg{}, nil)

	var callErr *common.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("expected *common.CallError, got %T (%v)", err, err)
	}
	if callErr.Status != common.StatusExecFailure {
		t.Errorf("Status = %d, want %d", callErr.Status, common.StatusExecFailure)
	}
}

// A successful call returns the payload and no error.
func TestCall_Success(t *testing.T) {
	want := []byte{0xde, 0xad}
	f := &Endorser{Engine: &stubEngine{callPayload: want}}

	got, err := f.Call(context.Background(), &ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}

// On a successful execution, Execute returns the builder's signed response.
func TestExecute_Success(t *testing.T) {
	want := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}
	f := &Endorser{Engine: &stubEngine{}, builder: &stubBuilder{resp: want}}

	got, err := f.Execute(context.Background(), endorsement.Invocation{}, types.NewTx(&types.LegacyTx{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("resp = %v, want %v", got, want)
	}
}

// A signing/endorse failure rides in the response as a 500, not a Go error.
func TestExecute_EndorseFailureIs500(t *testing.T) {
	f := &Endorser{Engine: &stubEngine{}, builder: &stubBuilder{err: errors.New("sign: hsm down")}}

	resp, err := f.Execute(context.Background(), endorsement.Invocation{}, types.NewTx(&types.LegacyTx{}))
	if err != nil {
		t.Fatalf("endorse failure must ride in the response, got Go error: %v", err)
	}
	if resp.Response.Status != common.StatusServerError {
		t.Errorf("status = %d, want %d", resp.Response.Status, common.StatusServerError)
	}
}

// The state readers forward straight to the engine.
func TestStateReadersDelegateToEngine(t *testing.T) {
	eng := &stubEngine{
		balance: big.NewInt(42),
		storage: []byte{0x01, 0x02},
		code:    []byte{0xfe, 0xed},
		nonce:   7,
	}
	f := &Endorser{Engine: eng}
	ctx := context.Background()
	addr := ethcommon.Address{}

	if bal, _ := f.BalanceAt(ctx, addr, nil); bal.Cmp(eng.balance) != 0 {
		t.Errorf("BalanceAt = %v, want %v", bal, eng.balance)
	}
	if got, _ := f.StorageAt(ctx, addr, ethcommon.Hash{}, nil); !bytes.Equal(got, eng.storage) {
		t.Errorf("StorageAt = %x, want %x", got, eng.storage)
	}
	if got, _ := f.CodeAt(ctx, addr, nil); !bytes.Equal(got, eng.code) {
		t.Errorf("CodeAt = %x, want %x", got, eng.code)
	}
	if got, _ := f.NonceAt(ctx, addr, nil); got != eng.nonce {
		t.Errorf("NonceAt = %d, want %d", got, eng.nonce)
	}
}
