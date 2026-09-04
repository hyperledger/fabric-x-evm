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

// reconcilingGate re-reads the ledger nonce before each admit, so it follows the
// snapshot reverts and primed state that only the test backend makes out of band.
type reconcilingGate struct {
	core.ResyncingSequencer
}

func (g reconcilingGate) Admit(ctx context.Context, tx *types.Transaction) error {
	if err := g.ResyncingSequencer.Resync(ctx, tx); err != nil {
		return err
	}
	return g.ResyncingSequencer.Admit(ctx, tx)
}

// NewReconcilingGate wraps a nonce gate with test-only reconcile-before-admit.
// Pass it to core.WithNonceSequencer.
func NewReconcilingGate(s core.ResyncingSequencer) core.NonceSequencer {
	return reconcilingGate{s}
}
