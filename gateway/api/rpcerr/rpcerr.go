/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package rpcerr provides typed JSON-RPC errors that implement
// go-ethereum's rpc.Error and rpc.DataError interfaces, so the
// gateway can return Ethereum-compatible error codes instead of
// the default -32603 Internal Error fallback.
//
// Standard JSON-RPC codes used here:
//
//	-32602 Invalid params         (malformed inputs from the caller)
//	-32603 Internal error         (unexpected backend failures)
//	-32003 Transaction rejected   (txpool rules: nonce, gas, funds, etc.)
//	-32000 Generic server error   (used for execution reverted; carries data)
package rpcerr

import "fmt"

// Standard error codes. The -32000 range is server-defined; geth uses
// -32003 for "transaction rejected" and -32000 for execution reverted.
const (
	CodeInvalidParams     = -32602
	CodeInternal          = -32603
	CodeTxRejected        = -32003
	CodeExecutionReverted = -32000
)

// Error is a JSON-RPC error with a code, satisfying rpc.Error.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string  { return e.Message }
func (e *Error) ErrorCode() int { return e.Code }

// DataError carries a code, message, and a payload (e.g. EVM revert
// data). It satisfies both rpc.Error and rpc.DataError.
type DataError struct {
	Code    int
	Message string
	Data    any
}

func (e *DataError) Error() string  { return e.Message }
func (e *DataError) ErrorCode() int { return e.Code }
func (e *DataError) ErrorData() any { return e.Data }

// InvalidParams returns -32602 for malformed caller input
// (bad hex, unparseable args, invalid raw tx, etc.).
func InvalidParams(format string, args ...any) error {
	return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf(format, args...)}
}

// Internal returns -32603 for unexpected backend failures.
// Use only when no more specific code applies.
func Internal(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}

// TxRejected returns -32003 for transactions rejected by validation
// rules (nonce too low, intrinsic gas, insufficient funds,
// unsupported tx type, init code too large, invalid sender,
// unprotected tx, etc.).
func TxRejected(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: CodeTxRejected, Message: err.Error()}
}

// ExecutionReverted returns -32000 with the EVM revert data attached
// as ErrorData(). reason is the decoded human-readable message (or
// "execution reverted" if the data does not match the Error(string)
// selector). data is the raw 0x-prefixed revert payload — callers
// should match what eth_call clients expect to read back.
func ExecutionReverted(reason string, data string) error {
	return &DataError{
		Code:    CodeExecutionReverted,
		Message: reason,
		Data:    data,
	}
}
