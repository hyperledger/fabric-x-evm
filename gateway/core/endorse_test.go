/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type stubEndorser struct {
	callResp *peer.ProposalResponse
	callErr  error
}

func (s *stubEndorser) ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	return nil, nil
}
func (s *stubEndorser) ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	return s.callResp, s.callErr
}
func (s *stubEndorser) ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error) {
	return nil, nil
}

func newClient(stub *stubEndorser) *EndorsementClient {
	return &EndorsementClient{endorsers: []Endorser{stub}}
}

func TestCallContract_Status201ReturnsRevertError(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{
			Status:  201,
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
		Response: &peer.Response{Status: 500, Message: "endorser dead"},
	}})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var revert *domain.RevertError
	if errors.As(err, &revert) {
		t.Errorf("non-revert error must not be *RevertError, got %v", revert)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallContract_Status200ReturnsPayload(t *testing.T) {
	want := []byte{0xde, 0xad}
	c := newClient(&stubEndorser{callResp: &peer.ProposalResponse{
		Response: &peer.Response{Status: 200, Payload: want},
	}})

	got, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}
