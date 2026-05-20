/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package domain

import "errors"

// ErrUnprotectedTx signals a transaction without EIP-155 replay protection.
var ErrUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// ErrNonceLookup wraps a backend failure to fetch the sender's nonce, so the
// API layer can distinguish backend faults from tx-rejection causes.
var ErrNonceLookup = errors.New("look up nonce")

// ErrExecutionReverted is the sentinel for EVM reverts surfaced by eth_call.
var ErrExecutionReverted = errors.New("execution reverted")

// RevertError carries the formatted reason and the raw revert payload bytes.
// The API layer maps it to JSON-RPC -32000 with the payload as ErrorData.
type RevertError struct {
	Reason string
	Data   []byte
}

func (e *RevertError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return ErrExecutionReverted.Error()
}

func (e *RevertError) Is(target error) bool {
	return target == ErrExecutionReverted
}
