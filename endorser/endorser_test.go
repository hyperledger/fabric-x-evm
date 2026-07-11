/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-x-evm/common"
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

func TestResponseStatusEVMExecFailure(t *testing.T) {
	resp := response(nil, vm.ErrOutOfGas)

	if resp.Response.Status != common.StatusEVMExecFailure {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusEVMExecFailure)
	}
}

func TestResponseStatusError(t *testing.T) {
	resp := response(nil, errors.New("backend unavailable"))

	if resp.Response.Status != common.StatusError {
		t.Fatalf("status = %d, want %d", resp.Response.Status, common.StatusError)
	}
}
