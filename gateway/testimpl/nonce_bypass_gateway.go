/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
)

// NonceBypassGateway wraps a Gateway and bypasses nonce validation in SendTransaction.
// This is used for performance testing scenarios like wrap-around replay where the same
// signed transactions are replayed repeatedly, and nonce validation would fail.
// The nonce priming at the endorser level (via BalancePrimingWrapper) ensures the
// Executor's nonce check passes, but we also need to bypass the Gateway's pre-flight
// nonce validation.
type NonceBypassGateway struct {
	*core.Gateway
}

// NewNonceBypassGateway creates a new gateway wrapper that skips nonce validation.
func NewNonceBypassGateway(gw *core.Gateway) *NonceBypassGateway {
	return &NonceBypassGateway{Gateway: gw}
}

// SendTransaction bypasses nonce validation but still uses the transaction queue
// to preserve MVCC retry logic and worker pool control.
func (g *NonceBypassGateway) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	// Skip ValidateTx (which includes nonce validation) and directly enqueue
	g.Gateway.TxQueue.Enqueue(tx)
	return nil
}
