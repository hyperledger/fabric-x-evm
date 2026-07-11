/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"errors"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// classifyValidationError maps a Backend.SendTransaction error to a typed
// JSON-RPC error. Internal lookup faults surface as -32603; everything else
// (geth txpool sentinels, domain.ErrUnprotectedTx) is a tx rejection (-32003).
func classifyValidationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrNonceLookup) {
		return rpcerr.Internal(err)
	}
	return rpcerr.TxRejected(err)
}

// classifyCallError maps a Backend.CallContract error to a typed JSON-RPC error:
// reverts (*domain.RevertError, with payload) and non-revert execution failures
// (*domain.ExecutionError) surface as -32000; everything else is -32603.
func classifyCallError(err error) error {
	if err == nil {
		return nil
	}
	var revert *domain.RevertError
	if errors.As(err, &revert) {
		return rpcerr.ExecutionReverted(revert.Reason, hexutil.Encode(revert.Data))
	}
	var exec *domain.ExecutionError
	if errors.As(err, &exec) {
		return rpcerr.ExecutionError(exec.Message)
	}
	return rpcerr.Internal(err)
}
