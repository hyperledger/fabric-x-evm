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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type stubEndorser struct {
	callPayload []byte
	callErr     error
	nonce       uint64
	nonceErr    error
}

func (s *stubEndorser) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error) {
	return nil, nil
}
func (s *stubEndorser) Call(ctx context.Context, msg *ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	return s.callPayload, s.callErr
}
func (s *stubEndorser) BalanceAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (s *stubEndorser) StorageAt(ctx context.Context, _ ethcommon.Address, _ ethcommon.Hash, _ *big.Int) ([]byte, error) {
	return nil, nil
}
func (s *stubEndorser) CodeAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) ([]byte, error) {
	return nil, nil
}
func (s *stubEndorser) NonceAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) (uint64, error) {
	return s.nonce, s.nonceErr
}

func newClient(stub *stubEndorser) *EndorsementClient {
	return &EndorsementClient{endorsers: []api.Service{stub}}
}

func TestCallContract_Status201ReturnsRevertError(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	c := newClient(&stubEndorser{
		callPayload: payload,
		callErr: &common.CallError{
			Status:  common.StatusEVMRevert,
			Message: "execution reverted: out of stock",
			Data:    payload,
		},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var revert *domain.RevertError
	if !errors.As(err, &revert) {
		t.Fatalf("expected *RevertError, got %T (%v)", err, err)
	}
	if revert.Reason != "execution reverted: out of stock" {
		t.Errorf("Reason = %q", revert.Reason)
	}
	if !bytes.Equal(revert.Data, payload) {
		t.Errorf("Data = %x, want %x", revert.Data, payload)
	}
	if !errors.Is(err, domain.ErrExecutionReverted) {
		t.Error("errors.Is(err, ErrExecutionReverted) = false")
	}
}

func TestCallContract_Status500IsGenericError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusServerError, Message: "endorser dead"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var revert *domain.RevertError
	if errors.As(err, &revert) {
		t.Errorf("non-revert error must not be *RevertError, got %v", revert)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	var exec *domain.ExecutionError
	if errors.As(err, &exec) {
		t.Errorf("backend fault must not be *ExecutionError, got %v", exec)
	}
}

func TestCallContract_Status400ReturnsExecutionError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusExecFailure, Message: "out of gas"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var exec *domain.ExecutionError
	if !errors.As(err, &exec) {
		t.Fatalf("expected *ExecutionError, got %T (%v)", err, err)
	}
	if exec.Message != "out of gas" {
		t.Errorf("Message = %q, want %q", exec.Message, "out of gas")
	}
}

func TestCallContract_Status200ReturnsPayload(t *testing.T) {
	want := []byte{0xde, 0xad}
	c := newClient(&stubEndorser{callPayload: want})

	got, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}

// A rejected tx (400) also maps to *ExecutionError, like a failed execution.
func TestCallContract_TxRejectedReturnsExecutionError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusTxRejected, Message: "nonce too low"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var exec *domain.ExecutionError
	if !errors.As(err, &exec) {
		t.Fatalf("expected *ExecutionError, got %T (%v)", err, err)
	}
	if exec.Message != "nonce too low" {
		t.Errorf("Message = %q, want %q", exec.Message, "nonce too low")
	}
}

// A plain (non-CallError) error is a transport failure: it is wrapped, not
// turned into a revert or execution error.
func TestCallContract_TransportErrorIsWrapped(t *testing.T) {
	c := newClient(&stubEndorser{callErr: errors.New("connection refused")})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var revert *domain.RevertError
	if errors.As(err, &revert) {
		t.Errorf("transport error must not be *RevertError, got %v", revert)
	}
	var exec *domain.ExecutionError
	if errors.As(err, &exec) {
		t.Errorf("transport error must not be *ExecutionError, got %v", exec)
	}
	if err.Error() != "process call: connection refused" {
		t.Errorf("error = %q, want %q", err.Error(), "process call: connection refused")
	}
}
