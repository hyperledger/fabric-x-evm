/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"errors"

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
