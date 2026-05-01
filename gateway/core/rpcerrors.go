/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"errors"

	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
)

// classifyValidationError maps a ValidateTx error to a typed JSON-RPC
// error suitable for return through eth_sendRawTransaction.
//
// All ValidateTx failures are tx rejections (-32003) except internal
// state-lookup failures, which are surfaced as -32603. Geth's own
// txpool sentinels (ErrNonceTooLow, ErrIntrinsicGas, ErrInsufficientFunds,
// ErrTxTypeNotSupported, ErrMaxInitCodeSizeExceeded, ErrInvalidSender)
// and the unprotected-tx sentinel all flow through TxRejected because
// they describe a tx the node refuses to admit, not a server fault.
func classifyValidationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNonceLookup) {
		return rpcerr.Internal(err)
	}
	return rpcerr.TxRejected(err)
}
