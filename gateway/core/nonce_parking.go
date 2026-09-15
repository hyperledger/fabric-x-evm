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

// Memory guardrails for parked transactions and the sender cache.
const (
	defaultMaxParkedPerSender = 64
	defaultParkedTTL          = 3 * time.Minute
	// Cap on parked transactions across all senders. Abandoned gaps cannot
	// grow without bound even when the per-sender cap and TTL have not yet
	// fired.
	defaultMaxParked = 4096
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

// nonceGate enqueues each sender's next expected nonce, parks higher ones until
// the gap fills, and rejects lower ones. The next nonce is cached per sender.
type nonceGate struct {
	mu     sync.RWMutex
	state  stateReader
	signer types.Signer
	queue  enqueuer

	senders *senderCache
	byHash  map[common.Hash]*types.Transaction // parked txs indexed by hash
	// parkedFrom is the subset of the cache that currently holds parked txs,
	// so TTL and global-cap sweeps do not walk the whole sender cache.
	parkedFrom map[common.Address]*senderState

	maxPerSender int
	maxParked    int
	maxSenders   int
	ttl          time.Duration
	now          func() time.Time
}

// expectedNonce is a sender's next admissible nonce, which starts out unknown: an
// entry reserved by Admit carries no nonce until its store read lands, and 0 is a
// real nonce, so no caller may mistake the zero value for one. value withholds it
// until it is set, and raise only ever moves it forwards -- a commit is newer than
// anything a store read already in flight can report.
type expectedNonce struct {
	nonce uint64
	known bool
}

func (e expectedNonce) value() (uint64, bool) { return e.nonce, e.known }

func (e *expectedNonce) raise(n uint64) {
	if !e.known || n > e.nonce {
		e.nonce = n
		e.known = true
	}
}

// senderState is one sender's next expected nonce and its parked transactions.
type senderState struct {
	from     common.Address
	next     expectedNonce
	parked   map[uint64]parkedTx
	lastSeen time.Time
	// refs counts the callers holding this entry. Eviction never drops a held
	// entry: Admit releases g.mu to read the store and must find its entry again.
	refs int
}

type parkedTx struct {
	tx       *types.Transaction
	parkedAt time.Time
}

// senderCache holds the per-sender state, reference counted so a caller can pin
// an entry across an unlock. All of it is guarded by nonceGate.mu.
type senderCache struct {
	entries map[common.Address]*senderState
}

func newSenderCache() *senderCache {
	return &senderCache{entries: make(map[common.Address]*senderState)}
}

// acquire pins the sender's entry, reserving a fresh one -- with no nonce yet --
// if the sender is unknown. Every acquire must be paired with a release.
func (c *senderCache) acquire(from common.Address) *senderState {
	ss := c.entries[from]
	if ss == nil {
		ss = &senderState{from: from, parked: make(map[uint64]parkedTx)}
		c.entries[from] = ss
	}
	ss.refs++
	return ss
}

// release unpins the entry. A reservation that never got a nonce and holds
// nothing is dropped, so a failed seed leaves nothing behind: the next Admit
// reserves and reads again instead of finding a half-built entry.
func (c *senderCache) release(ss *senderState) {
	ss.refs--
	if ss.refs > 0 || ss.next.known || len(ss.parked) > 0 {
		return
	}
	delete(c.entries, ss.from)
}

func (c *senderCache) lookup(from common.Address) (*senderState, bool) {
	ss, ok := c.entries[from]
	return ss, ok
}

func (c *senderCache) size() int { return len(c.entries) }

// evictLRU drops the least-recently-seen senders that evictable admits, while the
// cache is over max. A held entry is never dropped however the policy votes: its
// holder is off the lock reading the store and will come back to it.
func (c *senderCache) evictLRU(max int, evictable func(*senderState) bool) {
	for len(c.entries) > max {
		var oldest *senderState
		for _, ss := range c.entries {
			if ss.refs > 0 || !evictable(ss) {
				continue
			}
			if oldest == nil || ss.lastSeen.Before(oldest.lastSeen) {
				oldest = ss
			}
		}
		if oldest == nil {
			return // nothing evictable; the cap gives way to correctness
		}
		delete(c.entries, oldest.from)
	}
}

func newNonceGate(state stateReader, signer types.Signer, queue enqueuer) *nonceGate {
	return &nonceGate{
		state:        state,
		signer:       signer,
		queue:        queue,
		senders:      newSenderCache(),
		byHash:       make(map[common.Hash]*types.Transaction),
		parkedFrom:   make(map[common.Address]*senderState),
		maxPerSender: defaultMaxParkedPerSender,
		maxParked:    defaultMaxParked,
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
	// Deferred LIFO: evictLRU (registered after the seed) and release run with
	// the lock still held, then the unlock.
	defer g.mu.Unlock()
	ss := g.senders.acquire(from)
	defer g.senders.release(ss)

	// Steady state: the sender is cached with a nonce and we never touch the store.
	// Otherwise read it off the lock -- the reservation keeps an Observe that
	// commits for this sender from evicting the entry while we are away. Concurrent
	// Admits each read for themselves rather than trust a reservation that has no
	// nonce yet, which they cannot read in any case.
	next, seeded := ss.next.value()
	if !seeded {
		g.mu.Unlock()

		seed, err := g.state.NonceAt(ctx, from, nil)

		g.mu.Lock()
		if err != nil {
			// The reservation still has no nonce, so release drops it rather than
			// leaving a zero behind for the next Admit to read as a real one.
			return fmt.Errorf("look up nonce: %w", err)
		}
		// An Observe during the read may already have raised it past our seed, and
		// a commit is newer than anything the store could have told us.
		ss.next.raise(seed)
		next, _ = ss.next.value()
	}

	ss.lastSeen = g.now()
	g.sweepParked()
	defer g.evictLRU() // cannot drop ss: we still hold it

	switch {
	case tx.Nonce() < next:
		return fmt.Errorf("%w: next nonce %d, tx nonce %d", ethcore.ErrNonceTooLow, next, tx.Nonce())
	case tx.Nonce() == next:
		g.queue.Enqueue(tx)
		return nil
	}

	// Future nonce: park until the gap fills.
	_, replacing := ss.parked[tx.Nonce()]
	if !replacing && len(ss.parked) >= g.maxPerSender {
		return errTooManyParked
	}
	if replacing {
		// Overwriting an existing parked tx at this nonce. Ethereum replacement has
		// its own fee-bump rules, tracked in #62; until then we log and overwrite.
		logger.Infof("nonce gate: replacing parked tx for %s at nonce %d", from, tx.Nonce())
	} else {
		g.enforceParkedCap()
	}
	g.park(ss, tx)
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
		// Persist every committed sender, even one never admitted here: a
		// commit-derived nonce is authoritative, so the entry needs no seeding.
		ss := g.senders.acquire(from)
		ss.next.raise(n + 1)
		ss.lastSeen = now
		next, _ := ss.next.value() // set by the raise above

		// Promote even when this block did not move the nonce. A seed that landed
		// while the block was in flight may already have raised it past a parked
		// transaction that is now ready, and this is the only signal that will ever
		// release it: Admit enqueues its own transaction and never promotes another.
		for nonce := range ss.parked {
			if nonce < next {
				g.unpark(ss, nonce)
			}
		}
		if tx := g.unpark(ss, next); tx != nil {
			g.queue.Enqueue(tx)
		}
		g.senders.release(ss)
	}
	// Promote first so a gap that just filled still releases, then drop
	// parked txs whose senders never came back.
	g.sweepParked()
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
	g.parkedFrom[ss.from] = ss
}

// unpark removes any parked tx at nonce and returns it, or nil. Caller holds g.mu.
func (g *nonceGate) unpark(ss *senderState, nonce uint64) *types.Transaction {
	p, ok := ss.parked[nonce]
	if !ok {
		return nil
	}
	delete(ss.parked, nonce)
	delete(g.byHash, p.tx.Hash())
	if len(ss.parked) == 0 {
		delete(g.parkedFrom, ss.from)
	}
	return p.tx
}

// sweepParked drops parked txs whose gap never filled within the TTL, across
// every sender, so an abandoned gap is reclaimed without that sender parking
// again. Caller holds g.mu.
func (g *nonceGate) sweepParked() {
	if len(g.parkedFrom) == 0 {
		return
	}
	now := g.now()
	for _, ss := range g.parkedFrom {
		for nonce, p := range ss.parked {
			if now.Sub(p.parkedAt) > g.ttl {
				g.unpark(ss, nonce)
			}
		}
	}
}

// enforceParkedCap drops the oldest parked txs across senders until the global
// parked count is under the cap. Caller holds g.mu.
func (g *nonceGate) enforceParkedCap() {
	if g.maxParked <= 0 {
		return
	}
	for len(g.byHash) >= g.maxParked {
		ss, nonce, ok := g.oldestParked()
		if !ok {
			return
		}
		g.unpark(ss, nonce)
	}
}

// oldestParked returns the parked tx with the earliest parkedAt. Caller holds g.mu.
func (g *nonceGate) oldestParked() (*senderState, uint64, bool) {
	var (
		oldest *senderState
		nonce  uint64
		at     time.Time
		found  bool
	)
	for _, ss := range g.parkedFrom {
		for n, p := range ss.parked {
			if !found || p.parkedAt.Before(at) {
				oldest, nonce, at, found = ss, n, p.parkedAt, true
			}
		}
	}
	return oldest, nonce, found
}

// evictLRU bounds the sender cache. A sender holding parked txs stays however far
// over the cap we are: dropping it strands them and leaks their byHash entries.
// Caller holds g.mu.
func (g *nonceGate) evictLRU() {
	g.senders.evictLRU(g.maxSenders, func(ss *senderState) bool {
		return len(ss.parked) == 0
	})
}
