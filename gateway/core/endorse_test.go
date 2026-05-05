/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestClassifyCallRevert_NilResponse(t *testing.T) {
	if got := classifyCallRevert(nil); got != nil {
		t.Errorf("classifyCallRevert(nil) = %v, want nil", got)
	}
}

func TestClassifyCallRevert_NonRevertMessage(t *testing.T) {
	resp := &peer.Response{
		Status:  500,
		Message: "out of gas",
	}
	if got := classifyCallRevert(resp); got != nil {
		t.Errorf("non-revert message must classify to nil, got %v", got)
	}
}

func TestClassifyCallRevert_BareRevertNoReason(t *testing.T) {
	resp := &peer.Response{
		Status:  500,
		Message: "execution reverted",
	}
	got := classifyCallRevert(resp)
	if got == nil {
		t.Fatalf("expected *RevertError, got nil")
	}
	if !errors.Is(got, domain.ErrExecutionReverted) {
		t.Errorf("errors.Is(err, ErrExecutionReverted) = false")
	}
	if got.Reason != "execution reverted" {
		t.Errorf("Reason = %q, want %q", got.Reason, "execution reverted")
	}
	if len(got.Data) != 0 {
		t.Errorf("Data = %x, want empty", got.Data)
	}
}

func TestClassifyCallRevert_DecodedReasonAndPayload(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	resp := &peer.Response{
		Status:  500,
		Message: "execution reverted: out of stock",
		Payload: payload,
	}
	got := classifyCallRevert(resp)
	if got == nil {
		t.Fatalf("expected *RevertError, got nil")
	}
	if got.Reason != "execution reverted: out of stock" {
		t.Errorf("Reason = %q", got.Reason)
	}
	if !bytes.Equal(got.Data, payload) {
		t.Errorf("Data = %x, want %x", got.Data, payload)
	}
}
