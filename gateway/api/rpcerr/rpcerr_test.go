/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package rpcerr

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

func TestInvalidParamsCodeAndMessage(t *testing.T) {
	err := InvalidParams("bad hex %q", "0xnope")

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("InvalidParams must satisfy rpc.Error, got %T", err)
	}
	if rpcErr.ErrorCode() != CodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.ErrorCode(), CodeInvalidParams)
	}
	if rpcErr.Error() != `bad hex "0xnope"` {
		t.Errorf("message = %q, want %q", rpcErr.Error(), `bad hex "0xnope"`)
	}
}

func TestInternalCodeAndMessage(t *testing.T) {
	err := Internal(errors.New("db connection lost"))

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("Internal must satisfy rpc.Error")
	}
	if rpcErr.ErrorCode() != CodeInternal {
		t.Errorf("code = %d, want %d", rpcErr.ErrorCode(), CodeInternal)
	}
	if rpcErr.Error() != "db connection lost" {
		t.Errorf("message = %q, want %q", rpcErr.Error(), "db connection lost")
	}
}

func TestInternalNilReturnsNil(t *testing.T) {
	if err := Internal(nil); err != nil {
		t.Errorf("Internal(nil) = %v, want nil", err)
	}
}

func TestTxRejectedCodeAndMessage(t *testing.T) {
	err := TxRejected(errors.New("nonce too low"))

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("TxRejected must satisfy rpc.Error")
	}
	if rpcErr.ErrorCode() != CodeTxRejected {
		t.Errorf("code = %d, want %d", rpcErr.ErrorCode(), CodeTxRejected)
	}
	if rpcErr.Error() != "nonce too low" {
		t.Errorf("message = %q, want %q", rpcErr.Error(), "nonce too low")
	}
}

func TestTxRejectedNilReturnsNil(t *testing.T) {
	if err := TxRejected(nil); err != nil {
		t.Errorf("TxRejected(nil) = %v, want nil", err)
	}
}

func TestExecutionRevertedSatisfiesDataError(t *testing.T) {
	const data = "0x08c379a0deadbeef"
	err := ExecutionReverted("execution reverted: out of stock", data)

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("ExecutionReverted must satisfy rpc.Error")
	}
	if rpcErr.ErrorCode() != CodeExecutionReverted {
		t.Errorf("code = %d, want %d", rpcErr.ErrorCode(), CodeExecutionReverted)
	}

	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		t.Fatalf("ExecutionReverted must satisfy rpc.DataError")
	}
	if dataErr.ErrorData() != data {
		t.Errorf("ErrorData() = %v, want %s", dataErr.ErrorData(), data)
	}
	if dataErr.Error() != "execution reverted: out of stock" {
		t.Errorf("message = %q", dataErr.Error())
	}
}

func TestStandardCodesMatchSpec(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"InvalidParams", CodeInvalidParams, -32602},
		{"Internal", CodeInternal, -32603},
		{"TxRejected", CodeTxRejected, -32003},
		{"ExecutionReverted", CodeExecutionReverted, -32000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
			}
		})
	}
}
