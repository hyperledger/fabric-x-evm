/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// passthroughGate is the nonce sequencer for harnesses that replay signed
// transactions or move ledger state out of band without telling the gateway. It
// keeps no per-sender nonce, so nothing can leave it stale. testnode uses
// ResettableGate instead.
type passthroughGate struct {
	queue core.TxQueueInterface
}

// Admit enqueues every transaction, parking none.
func (g passthroughGate) Admit(_ context.Context, tx *types.Transaction) error {
	g.queue.Enqueue(tx)
	return nil
}

// Observe has nothing to release, since nothing is ever parked.
func (g passthroughGate) Observe([]domain.Transaction) {}

// IsPending reports nothing parked, so the queue alone answers the question.
func (g passthroughGate) IsPending(common.Hash) *types.Transaction { return nil }

// NewPassthroughGate returns a nonce sequencer that admits everything straight
// to the queue. Pass it to core.WithNonceSequencer.
func NewPassthroughGate(queue core.TxQueueInterface) core.NonceSequencer {
	return passthroughGate{queue: queue}
}
