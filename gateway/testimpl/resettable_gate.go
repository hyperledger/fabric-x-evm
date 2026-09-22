/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// ResettableGate is testnode's nonce sequencer: the real gate, which evm_revert can discard
// because a revert rewinds ledger nonces below whatever the gate cached.
type ResettableGate struct {
	mu    sync.RWMutex
	inner core.NonceSequencer
	fresh func() core.NonceSequencer
}

// NewResettableGate returns a gate that must be Bound before its first transaction.
func NewResettableGate() *ResettableGate { return &ResettableGate{} }

// Bind sets how to build the real gate and installs one. It runs once the gateway exists, since the gate reads nonces from it.
func (g *ResettableGate) Bind(fresh func() core.NonceSequencer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fresh = fresh
	g.inner = fresh()
}

// ResetNonces replaces the gate with a fresh one, dropping every cached nonce and parked transaction.
func (g *ResettableGate) ResetNonces() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inner = g.fresh()
}

func (g *ResettableGate) Admit(ctx context.Context, tx *types.Transaction) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.inner.Admit(ctx, tx)
}

func (g *ResettableGate) Observe(committed []domain.Transaction) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	g.inner.Observe(committed)
}

func (g *ResettableGate) IsPending(hash common.Hash) *types.Transaction {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.inner.IsPending(hash)
}
