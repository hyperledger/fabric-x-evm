/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// Memory guardrails per sender.
const (
	defaultMaxParkedPerSender = 64
	defaultParkedTTL          = 3 * time.Minute
	// Cap on cached senders before LRU eviction.
	defaultMaxSenders = 1 << 20
)

var errTooManyParked = errors.New("too many queued (future-nonce) transactions for sender")

// enqueuer receives a ready transaction.
type enqueuer interface {
	Enqueue(tx *types.Transaction)
}

// NonceSequencer gates a sender's transactions by nonce.
type NonceSequencer interface {
	Admit(ctx context.Context, tx *types.Transaction) error
	Observe(committed []domain.Transaction)
	IsPending(hash common.Hash) *types.Transaction
}

// ResyncingSequencer is a NonceSequencer that can re-read a sender's committed
// nonce from state. The test backend wraps it to follow out-of-band changes.
type ResyncingSequencer interface {
	NonceSequencer
	Resync(ctx context.Context, tx *types.Transaction) error
}

// nonceGate enqueues each sender's next expected nonce, parks higher ones until
// the gap fills, and rejects lower ones. The next nonce is cached per sender.
type nonceGate struct {
	mu     sync.RWMutex
	state  stateReader
	signer types.Signer
	queue  enqueuer

	senders map[common.Address]*senderState
	byHash  map[common.Hash]*types.Transaction // parked txs indexed by hash

	maxPerSender int
	maxSenders   int
	ttl          time.Duration
	now          func() time.Time
}

// senderState is one sender's next expected nonce and its parked transactions.
type senderState struct {
	next     uint64 // next nonce eligible to admit
	parked   map[uint64]parkedTx
	lastSeen time.Time
}

type parkedTx struct {
	tx       *types.Transaction
	parkedAt time.Time
}

func newNonceGate(state stateReader, signer types.Signer, queue enqueuer) *nonceGate {
	return &nonceGate{
		state:        state,
		signer:       signer,
		queue:        queue,
		senders:      make(map[common.Address]*senderState),
		byHash:       make(map[common.Hash]*types.Transaction),
		maxPerSender: defaultMaxParkedPerSender,
		maxSenders:   defaultMaxSenders,
		ttl:          defaultParkedTTL,
		now:          time.Now,
	}
}

// Admit enqueues tx at its expected nonce, parks a higher one, rejects a lower one.
func (g *nonceGate) Admit(ctx context.Context, tx *types.Transaction) error {
	from, err := types.Sender(g.signer, tx)
	if err != nil {
		return fmt.Errorf("recover sender: %w", err)
	}

	g.mu.Lock()
	// Steady state: the sender is cached and we never touch the store. On a miss,
	// release the lock for the slow seed, then re-acquire and recheck - an Observe
	// may have created the sender while we were unlocked.
	ss := g.senders[from]
	if ss == nil {
		g.mu.Unlock()
		seed, err := g.state.NonceAt(ctx, from, nil)
		if err != nil {
			return fmt.Errorf("look up nonce: %w", err)
		}
		g.mu.Lock()
		if ss = g.senders[from]; ss == nil {
			g.evictLRU() // evict before insert so we never drop the new entry
			ss = &senderState{next: seed, parked: make(map[uint64]parkedTx)}
			g.senders[from] = ss
		}
	}
	defer g.mu.Unlock()

	ss.lastSeen = g.now()

	switch {
	case tx.Nonce() < ss.next:
		return fmt.Errorf("%w: next nonce %d, tx nonce %d", ethcore.ErrNonceTooLow, ss.next, tx.Nonce())
	case tx.Nonce() == ss.next:
		g.queue.Enqueue(tx)
		return nil
	}

	// Future nonce: park until the gap fills.
	g.evictExpiredParked(ss)
	_, replacing := ss.parked[tx.Nonce()]
	if !replacing && len(ss.parked) >= g.maxPerSender {
		return errTooManyParked
	}
	if replacing {
		// Overwriting an existing parked tx at this nonce. Ethereum replacement has
		// its own fee-bump rules, tracked in #62; until then we log and overwrite.
		logger.Infof("nonce gate: replacing parked tx for %s at nonce %d", from, tx.Nonce())
	}
	g.park(ss, tx)
	return nil
}

// Resync updates the cache to the sender's committed nonce and drops any parked
// tx that is now too low.
func (g *nonceGate) Resync(ctx context.Context, tx *types.Transaction) error {
	from, err := types.Sender(g.signer, tx)
	if err != nil {
		return fmt.Errorf("recover sender: %w", err)
	}
	committed, err := g.state.NonceAt(ctx, from, nil)
	if err != nil {
		return fmt.Errorf("look up nonce: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	ss := g.senders[from]
	if ss == nil {
		ss = &senderState{parked: make(map[uint64]parkedTx)}
		g.senders[from] = ss
	}
	ss.next = committed
	ss.lastSeen = g.now()
	for nonce := range ss.parked {
		if nonce < ss.next {
			g.unpark(ss, nonce)
		}
	}
	return nil
}

// Observe advances each sender's cached nonce from the block and releases any
// now-ready parked transaction. Only Fabric-valid commits advance a nonce.
func (g *nonceGate) Observe(committed []domain.Transaction) {
	// Highest valid nonce per sender.
	highest := make(map[common.Address]uint64)
	for i := range committed {
		if !committed[i].FabricValid {
			continue // invalidated: nonce not consumed
		}
		tx := committed[i].ToEthTx()
		if tx == nil {
			continue
		}
		from := common.BytesToAddress(committed[i].FromAddress)
		if cur, ok := highest[from]; !ok || tx.Nonce() > cur {
			highest[from] = tx.Nonce()
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	for from, n := range highest {
		ss := g.senders[from]
		if ss == nil {
			// Persist every committed sender, even one never admitted here.
			ss = &senderState{parked: make(map[uint64]parkedTx)}
			g.senders[from] = ss
		}
		if n+1 > ss.next {
			ss.next = n + 1
		}
		ss.lastSeen = now
		for nonce := range ss.parked {
			if nonce < ss.next {
				g.unpark(ss, nonce)
			}
		}
		if tx := g.unpark(ss, ss.next); tx != nil {
			g.queue.Enqueue(tx)
		}
	}
	g.evictLRU()
}

// IsPending returns a parked transaction by hash, or nil if it is not parked.
func (g *nonceGate) IsPending(hash common.Hash) *types.Transaction {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.byHash[hash]
}

// park adds tx to the sender's parked set and the by-hash index. Caller holds g.mu.
func (g *nonceGate) park(ss *senderState, tx *types.Transaction) {
	if old, ok := ss.parked[tx.Nonce()]; ok {
		delete(g.byHash, old.tx.Hash())
	}
	ss.parked[tx.Nonce()] = parkedTx{tx: tx, parkedAt: g.now()}
	g.byHash[tx.Hash()] = tx
}

// unpark removes any parked tx at nonce and returns it, or nil. Caller holds g.mu.
func (g *nonceGate) unpark(ss *senderState, nonce uint64) *types.Transaction {
	p, ok := ss.parked[nonce]
	if !ok {
		return nil
	}
	delete(ss.parked, nonce)
	delete(g.byHash, p.tx.Hash())
	return p.tx
}

// evictExpiredParked drops parked txs whose gap never filled within the TTL. Caller holds g.mu.
func (g *nonceGate) evictExpiredParked(ss *senderState) {
	now := g.now()
	for nonce, p := range ss.parked {
		if now.Sub(p.parkedAt) > g.ttl {
			delete(ss.parked, nonce)
			delete(g.byHash, p.tx.Hash())
		}
	}
}

// evictLRU drops least-recently-seen senders with no parked txs while over the cap.
func (g *nonceGate) evictLRU() {
	for len(g.senders) > g.maxSenders {
		var oldest common.Address
		var oldestSeen time.Time
		found := false
		for from, ss := range g.senders {
			if len(ss.parked) > 0 {
				continue
			}
			if !found || ss.lastSeen.Before(oldestSeen) {
				oldest, oldestSeen, found = from, ss.lastSeen, true
			}
		}
		if !found {
			return
		}
		delete(g.senders, oldest)
	}
}
