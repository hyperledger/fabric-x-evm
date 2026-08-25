/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"context"
	"fmt"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
)

// claimSeeder seeds claimed and dispatched on the HybridSynchronizer before
// either delivery or notification starts.
type claimSeeder interface {
	// Seed blocks until it has determined the first block delivery will receive,
	// then sets claimed and dispatched to firstBlock-1 on h.
	Seed(ctx context.Context) error
}

// throwawaySeeder implements claimSeeder for the real (non-test) path.  It
// builds a throwaway synchronizer — identical to the real one but wired to a
// one-shot handler — runs it until the first block arrives, and uses that
// block number to seed h.
type throwawaySeeder struct {
	db      network.BlockHeightReader
	channel string
	conf    network.PeerConf
	signer  sdk.Signer
	h       *HybridSynchronizer
}

func (s *throwawaySeeder) Seed(ctx context.Context) error {
	// expected is what the delivery synchronizer will request from the peer:
	// the block after the last one in our local store.  BlockHeight returns
	// localBlockNumber+1, and the synchronizer uses start=0 when that value
	// is ≤1 (empty store / block-0 tip).
	expected, err := s.h.delivery.BlockHeight(ctx)
	if err != nil {
		return fmt.Errorf("hybridx: seed claim: read local height: %w", err)
	}
	if expected <= 1 {
		expected = 0
	}

	got := make(chan uint64, 1)
	peekCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	peekShim := &firstBlockShim{got: got, cancel: cancel}
	throwaway, err := nfabx.NewSynchronizer(s.db, s.channel, s.conf, s.signer, sdk.NoOpLogger{}, peekShim)
	if err != nil {
		return fmt.Errorf("hybridx: seed claim: create throwaway synchronizer: %w", err)
	}
	go func() {
		// Error is expected: peekCtx is cancelled as soon as the first block arrives.
		_ = throwaway.Start(peekCtx)
	}()

	select {
	case first := <-got:
		switch {
		case first == expected:
			// Normal case: peer delivered exactly the block we expected.
		case expected == 0 && first == 1:
			// fabrictest quirk: the in-process test network numbers its first
			// block as 1 rather than 0.  This is benign — the seed is based on
			// what the peer actually delivers, so the protocol still works.
			s.h.logger.Infof("hybridx: peer delivered block 1 as its first block (expected 0); using block 1 as seed anchor (in-process test network)")
		default:
			panic(fmt.Errorf(
				"hybridx: peer delivered block %d as first block but we expected block %d — "+
					"blocks %d..%d are missing and cannot be recovered without a restart",
				first, expected, expected, first-1))
		}
		s.h.applySeed(first)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// firstBlockShim is a blocks.BlockHandler used by throwawaySeeder.  On the
// first block it sends the block number on got and cancels the context.
type firstBlockShim struct {
	got    chan<- uint64
	cancel context.CancelFunc
}

func (s *firstBlockShim) Handle(_ context.Context, b blocks.Block) error {
	s.got <- b.Number
	s.cancel()
	return nil
}

// noopSeeder is the claimSeeder used by newWithDeps.  Tests seed via seedClaimAt.
type noopSeeder struct{}

func (noopSeeder) Seed(context.Context) error { return nil }

// applySeed stores firstBlock-1 into both claimed and dispatched.
func (h *HybridSynchronizer) applySeed(first uint64) {
	seed := int64(first) - 1
	h.claimed.Store(seed)
	h.dispatched.Store(seed)
	h.logger.Infof("hybridx: seeded claim at %d (first delivered block is %d)", seed, first)
}

// seedClaimAt seeds claimed and dispatched directly from a known first block
// number.  Used by tests that build HybridSynchronizer via newWithDeps and
// control delivery via fakeDelivery.deliver.
func (h *HybridSynchronizer) seedClaimAt(first uint64) {
	h.applySeed(first)
}
