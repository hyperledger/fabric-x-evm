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

// passthroughGate is the test backend's nonce sequencer. It keeps no per-sender
// nonce, so the snapshot reverts and primed state that only the test backend
// makes out of band can never leave it stale.
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
