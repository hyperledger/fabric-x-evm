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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type stubEndorser struct {
	callResp  *peer.ProposalResponse
	callErr   error
	queryResp *peer.ProposalResponse
	queryErr  error
}

func (s *stubEndorser) ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error) {
	return nil, nil
}
func (s *stubEndorser) ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, _ *big.Int) (*peer.ProposalResponse, error) {
	return s.callResp, s.callErr
}
func (s *stubEndorser) ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error) {
	return s.queryResp, s.queryErr
}

func newClient(stub *stubEndorser) *EndorsementClient {
	return &EndorsementClient{endorsers: []api.Service{stub}}
}

func TestCallContract_Status201ReturnsRevertError(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{
			Status:  common.StatusEVMRevert,
			Message: "execution reverted: out of stock",
			Payload: payload,
		},
	}})

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
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{Status: common.StatusServerError, Message: "endorser dead"},
	}})

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
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{Status: common.StatusExecFailure, Message: "out of gas"},
	}})

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
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{Status: common.StatusOK, Payload: want},
	}})

	got, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}
