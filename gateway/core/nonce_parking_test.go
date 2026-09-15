/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/require"
)

// stubState returns a per-sender committed nonce and counts its reads, so tests
// can assert seeding happens only once per sender.
type stubState struct {
	mu     sync.Mutex
	nonces map[common.Address]uint64
	err    error
	reads  int
	// onRead, if set, runs at the start of NonceAt so a test can interleave with
	// a seed that is in flight.
	onRead func()
}

func newStubState() *stubState {
	return &stubState{nonces: make(map[common.Address]uint64)}
}

func (s *stubState) NonceAt(_ context.Context, a common.Address, _ *big.Int) (uint64, error) {
	if s.onRead != nil {
		s.onRead()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.err != nil {
		return 0, s.err
	}
	return s.nonces[a], nil
}

func (s *stubState) set(a common.Address, n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[a] = n
}

func (s *stubState) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// enqueueRecorder records the transactions the gate hands to the queue.
type enqueueRecorder struct {
	mu  sync.Mutex
	txs []*types.Transaction
}

func (e *enqueueRecorder) Enqueue(tx *types.Transaction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.txs = append(e.txs, tx)
}

func (e *enqueueRecorder) nonces() []uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]uint64, len(e.txs))
	for i, tx := range e.txs {
		out[i] = tx.Nonce()
	}
	return out
}

func newTestGate(state stateReader) (*nonceGate, *enqueueRecorder) {
	q := &enqueueRecorder{}
	signer := types.LatestSignerForChainID(big.NewInt(testChainID))
	return newNonceGate(state, signer, q), q
}

func senderAddr(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}

// committedBlock builds the committed-transaction slice Observe expects, carrying
// the sender and the raw tx that Observe reads the nonce from.
func committedBlock(t *testing.T, key *ecdsa.PrivateKey, nonces ...uint64) []domain.Transaction {
	t.Helper()
	from := senderAddr(key)
	out := make([]domain.Transaction, len(nonces))
	for i, n := range nonces {
		tx := newValidTx(t, key, validTxOpts{nonce: n})
		raw, err := tx.MarshalBinary()
		require.NoError(t, err)
		out[i] = domain.Transaction{FromAddress: from.Bytes(), RawTx: raw, FabricValid: true}
	}
	return out
}

func TestNonceGate_InOrderAdmits(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())
}

func TestNonceGate_TooLowRejected(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	err := gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 3}))
	require.ErrorIs(t, err, ethcore.ErrNonceTooLow)
	require.Empty(t, q.nonces())
}

func TestNonceGate_FutureParks(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	tx := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), tx))

	require.Empty(t, q.nonces())
	require.Equal(t, tx.Hash(), gate.IsPending(tx.Hash()).Hash())
	require.Nil(t, gate.IsPending(common.Hash{0xde, 0xad}))
}

func TestNonceGate_ReleaseOnCommit(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	future := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), future))
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())

	// Nonce 5 commits; 6 is released.
	gate.Observe(committedBlock(t, key, 5))
	require.Equal(t, []uint64{5, 6}, q.nonces())
	require.Nil(t, gate.IsPending(future.Hash()))
}

func TestNonceGate_ReleasesOnePerCommit(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	tx6 := newValidTx(t, key, validTxOpts{nonce: 6})
	tx7 := newValidTx(t, key, validTxOpts{nonce: 7})
	require.NoError(t, gate.Admit(context.Background(), tx6))
	require.NoError(t, gate.Admit(context.Background(), tx7))
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())

	// Only 6 releases while 7 still has a gap.
	gate.Observe(committedBlock(t, key, 5))
	require.Equal(t, []uint64{5, 6}, q.nonces())
	require.Equal(t, tx7.Hash(), gate.IsPending(tx7.Hash()).Hash())

	// 6 commits, releasing 7.
	gate.Observe(committedBlock(t, key, 6))
	require.Equal(t, []uint64{5, 6, 7}, q.nonces())
	require.Nil(t, gate.IsPending(tx7.Hash()))
}

func TestNonceGate_TwoSendersIndependent(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	state.set(senderAddr(keyB), 2)
	gate, q := newTestGate(state)

	txA := newValidTx(t, keyA, validTxOpts{nonce: 6}) // future -> parked
	txB := newValidTx(t, keyB, validTxOpts{nonce: 2}) // in order -> admitted
	require.NoError(t, gate.Admit(context.Background(), txA))
	require.NoError(t, gate.Admit(context.Background(), txB))

	require.Equal(t, []uint64{2}, q.nonces())
	require.Equal(t, txA.Hash(), gate.IsPending(txA.Hash()).Hash())
}

func TestNonceGate_PerSenderCap(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, _ := newTestGate(state)
	gate.maxPerSender = 2

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6})))
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 7})))
	require.ErrorIs(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 8})), errTooManyParked)
}

func TestNonceGate_InOrderAdmitsSkipStateReads(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, _ := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5}))) // seeds
	gate.Observe(committedBlock(t, key, 5))                                                         // next -> 6
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6}))) // in order, no read
	require.Equal(t, 1, state.readCount())
}

func TestNonceGate_TTLEviction(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, _ := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	stale := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), stale))

	// Past the TTL, the next park sweeps the expired entry.
	now = now.Add(defaultParkedTTL + time.Second)
	fresh := newValidTx(t, key, validTxOpts{nonce: 7})
	require.NoError(t, gate.Admit(context.Background(), fresh))

	require.Nil(t, gate.IsPending(stale.Hash()))
	require.Equal(t, fresh.Hash(), gate.IsPending(fresh.Hash()).Hash())
}

func TestNonceGate_SeedErrorPropagates(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.err = errors.New("ledger unavailable")
	gate, _ := newTestGate(state)

	err := gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5}))
	require.ErrorIs(t, err, state.err)
}

func TestNonceGate_ObservePersistsUnknownSender(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	gate, q := newTestGate(state)

	// A commit arrives for a sender the gate never admitted.
	gate.Observe(committedBlock(t, key, 5))

	// The next in-order tx is admitted from the cache, with no state read.
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6})))
	require.Equal(t, []uint64{6}, q.nonces())
	require.Equal(t, 0, state.readCount())
}

func TestNonceGate_ObserveSkipsInvalidTx(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	gate, q := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	tx6 := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), tx6))
	require.Equal(t, []uint64{5}, q.nonces())

	// An invalidated commit must not advance the sender's nonce.
	block := committedBlock(t, key, 5)
	block[0].FabricValid = false
	gate.Observe(block)

	require.Equal(t, []uint64{5}, q.nonces())
	require.Equal(t, tx6.Hash(), gate.IsPending(tx6.Hash()).Hash())
}

func TestNonceGate_ObserveAdvancesOnRevert(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	gate, q := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	tx6 := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), tx6))
	require.Equal(t, []uint64{5}, q.nonces())

	// A revert consumes the nonce, so it must advance and release the next tx,
	// even though its EVM status is 0.
	block := committedBlock(t, key, 5)
	block[0].Status = 0
	gate.Observe(block)

	require.Equal(t, []uint64{5, 6}, q.nonces())
	require.Nil(t, gate.IsPending(tx6.Hash()))
}

// An Observe landing while a seed is in flight must not be undone by the stale
// value the store returns, and eviction must not drop the reservation.
func TestNonceGate_SeedRaceWithObserveAndEvict(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	gate, q := newTestGate(state)
	gate.maxSenders = 0 // every eviction pass tries to drop this sender

	seeding := make(chan struct{})
	release := make(chan struct{})
	state.onRead = func() {
		close(seeding)
		<-release
	}

	admitted := make(chan error, 1)
	go func() {
		admitted <- gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6}))
	}()

	<-seeding
	// Nonce 5 commits and Observe evicts, both while the seed is off the lock.
	gate.Observe(committedBlock(t, key, 5))
	close(release)

	require.NoError(t, <-admitted)
	// The commit won: nonce 6 is next, so it is queued rather than parked
	// waiting for a nonce that has already committed.
	require.Equal(t, []uint64{6}, q.nonces())

	// The reservation also survived the eviction pass, so the advanced nonce is
	// still cached for the next transaction from this sender.
	gate.mu.RLock()
	ss, ok := gate.senders.lookup(from)
	gate.mu.RUnlock()
	require.True(t, ok, "an entry held by a seeding Admit must not be evicted")
	next, seeded := ss.next.value()
	require.True(t, seeded)
	require.Equal(t, uint64(6), next)
}

// A failed seed must not leave a half-built sender behind for the next Admit to
// read as an authoritative zero.
func TestNonceGate_SeedFailureLeavesNoSender(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.err = errors.New("store down")
	gate, _ := newTestGate(state)

	require.Error(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))

	gate.mu.RLock()
	defer gate.mu.RUnlock()
	require.Zero(t, gate.senders.size())
}

// blockFirstRead holds the first NonceAt until release is called, and lets every
// later read -- including the ones concurrent Admits issue for themselves -- run
// straight through.
func blockFirstRead(state *stubState) (seeding <-chan struct{}, release func()) {
	started, gate := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	state.onRead = func() {
		if reads.Add(1) == 1 {
			close(started)
			<-gate
		}
	}
	return started, func() { close(gate) }
}

// A second Admit arriving while the first is seeding must not read the reserved
// entry's absent nonce as an authoritative zero, which would send a stale
// transaction for execution.
func TestNonceGate_ConcurrentAdmitDuringSeedRejectsStale(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	seeding, release := blockFirstRead(state)
	gate, q := newTestGate(state)

	admitted := make(chan error, 1)
	go func() {
		admitted <- gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 9}))
	}()
	<-seeding

	err := gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 0}))
	require.ErrorIs(t, err, ethcore.ErrNonceTooLow)

	release()
	require.NoError(t, <-admitted)
	require.Empty(t, q.nonces())
	require.Equal(t, 2, state.readCount(), "each Admit seeds for itself")
}

// A second Admit arriving while the first is seeding, carrying the nonce that is
// genuinely next, must be enqueued. Parking it strands it: nothing of the
// sender's is in flight, so no commit will ever arrive to release it.
func TestNonceGate_ConcurrentAdmitDuringSeedEnqueuesReady(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	seeding, release := blockFirstRead(state)
	gate, q := newTestGate(state)

	future := newValidTx(t, key, validTxOpts{nonce: 9})
	admitted := make(chan error, 1)
	go func() {
		admitted <- gate.Admit(context.Background(), future)
	}()
	<-seeding

	ready := newValidTx(t, key, validTxOpts{nonce: 5})
	require.NoError(t, gate.Admit(context.Background(), ready))

	release()
	require.NoError(t, <-admitted)

	require.Equal(t, []uint64{5}, q.nonces())
	require.Nil(t, gate.IsPending(ready.Hash()))
	require.Equal(t, future.Hash(), gate.IsPending(future.Hash()).Hash())
}

// A seed that fails while a second Admit is parked on the same sender must still
// leave no entry behind, or the sender is stuck at nonce zero for good.
func TestNonceGate_SeedFailureWithConcurrentAdmitLeavesNoSender(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.err = errors.New("store down")
	seeding, release := blockFirstRead(state)
	gate, q := newTestGate(state)

	admitted := make(chan error, 1)
	go func() {
		admitted <- gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 9}))
	}()
	<-seeding

	require.Error(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	release()
	require.Error(t, <-admitted)

	gate.mu.RLock()
	size := gate.senders.size()
	gate.mu.RUnlock()
	require.Zero(t, size, "a failed seed must leave no entry to read as nonce zero")

	// Once the store recovers, the sender is seeded from scratch.
	state.err = nil
	state.set(senderAddr(key), 5)
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())
}

// Senders holding parked transactions are never evicted, however far over the
// cap the cache is.
func TestNonceGate_EvictLRUKeepsParkedSenders(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	gate, q := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 7})))
	gate.maxSenders = 0
	gate.evictLRU()

	gate.mu.RLock()
	_, kept := gate.senders.lookup(from)
	gate.mu.RUnlock()
	require.True(t, kept, "a sender with parked transactions must survive eviction")

	// It is still parked, so the gap filling still releases it.
	gate.Observe(committedBlock(t, key, 5, 6))
	require.Equal(t, []uint64{7}, q.nonces())
}

// A transaction whose sender cannot be recovered is rejected before it reaches
// any per-sender state.
func TestNonceGate_UnrecoverableSenderRejected(t *testing.T) {
	state := newStubState()
	gate, q := newTestGate(state)

	to := common.HexToAddress("0x1")
	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(testChainID),
		Nonce:     5,
		Gas:       21000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		To:        &to,
	})

	require.Error(t, gate.Admit(context.Background(), unsigned))
	require.Empty(t, q.nonces())
	require.Equal(t, 0, state.readCount(), "a bad signature must not reach the store")
}

// A second transaction at an already-parked nonce replaces the first, and the
// by-hash index follows it so the replaced one stops reporting as pending.
func TestNonceGate_ParkedReplacement(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	first := newValidTx(t, key, validTxOpts{nonce: 7, gas: 21_000})
	second := newValidTx(t, key, validTxOpts{nonce: 7, gas: 22_000})
	require.NoError(t, gate.Admit(context.Background(), first))
	require.NoError(t, gate.Admit(context.Background(), second))

	require.Nil(t, gate.IsPending(first.Hash()), "the replaced tx is no longer parked")
	require.NotNil(t, gate.IsPending(second.Hash()))

	// Filling the gap releases the replacement, not the original.
	gate.Observe(committedBlock(t, key, 5, 6))
	require.Equal(t, []uint64{7}, q.nonces())
	require.Equal(t, second.Hash(), q.txs[0].Hash())
}

// Parked transactions that a commit leaves below the sender's next nonce are
// dropped rather than queued.
func TestNonceGate_ObserveDropsStaleParked(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	stale := newValidTx(t, key, validTxOpts{nonce: 7})
	require.NoError(t, gate.Admit(context.Background(), stale))

	// Nonce 8 commits, so 7 can never be next again.
	gate.Observe(committedBlock(t, key, 8))

	require.Empty(t, q.nonces(), "a nonce below next must not be queued")
	require.Nil(t, gate.IsPending(stale.Hash()), "and must leave the by-hash index")
}

// A committed entry whose raw transaction cannot be decoded is skipped instead
// of advancing anything.
func TestNonceGate_ObserveSkipsUndecodableTx(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	gate, q := newTestGate(state)

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6})))

	gate.Observe([]domain.Transaction{{
		FromAddress: from.Bytes(),
		RawTx:       []byte("not a transaction"),
		FabricValid: true,
	}})

	require.Empty(t, q.nonces(), "an undecodable commit advances no nonce")
}

func parkedCount(g *nonceGate) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.byHash)
}

func parkedSenderCount(g *nonceGate) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.parkedFrom)
}

func cachedSenderCount(g *nonceGate) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.senders.size()
}

// A sender that parks once and never returns still loses the parked tx once
// the TTL elapses and any other Admit runs — not only when that sender parks
// again.
func TestNonceGate_AbandonedSenderTTLOnOtherAdmit(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	state.set(senderAddr(keyB), 2)
	gate, q := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	stale := newValidTx(t, keyA, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), stale))
	require.Equal(t, 1, parkedCount(gate))

	now = now.Add(defaultParkedTTL + time.Second)
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, keyB, validTxOpts{nonce: 2})))

	require.Equal(t, []uint64{2}, q.nonces())
	require.Nil(t, gate.IsPending(stale.Hash()), "abandoned parked tx must expire without that sender returning")
	require.Equal(t, 0, parkedCount(gate))
	require.Equal(t, 0, parkedSenderCount(gate))
}

// Blocks keep arriving even when the abandoned sender does not. Observe must
// sweep expired parked txs for senders that are not in the block.
func TestNonceGate_AbandonedSenderTTLOnObserve(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	gate, _ := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	stale := newValidTx(t, keyA, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), stale))

	now = now.Add(defaultParkedTTL + time.Second)
	gate.Observe(committedBlock(t, keyB, 0))

	require.Nil(t, gate.IsPending(stale.Hash()))
	require.Equal(t, 0, parkedCount(gate))
	require.Equal(t, 0, parkedSenderCount(gate))
}

// An empty block is still a clock tick: expired parked txs must go even when
// Observe has nothing to promote.
func TestNonceGate_EmptyObserveSweepsAbandoned(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, _ := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	stale := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), stale))

	now = now.Add(defaultParkedTTL + time.Second)
	gate.Observe(nil)

	require.Nil(t, gate.IsPending(stale.Hash()))
	require.Equal(t, 0, parkedCount(gate))
}

// Traffic from another sender before the TTL must not drop a live parked tx.
func TestNonceGate_TTLDoesNotDropEarly(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	state.set(senderAddr(keyB), 2)
	gate, _ := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	live := newValidTx(t, keyA, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), live))

	now = now.Add(defaultParkedTTL)
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, keyB, validTxOpts{nonce: 2})))

	require.Equal(t, live.Hash(), gate.IsPending(live.Hash()).Hash())
	require.Equal(t, 1, parkedCount(gate))
}

// A commit that fills the gap still releases the parked tx even if it has
// sat past the TTL: that gap is no longer abandoned.
func TestNonceGate_ObserveReleasesExpiredWhenGapFills(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	future := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), future))
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))

	now = now.Add(defaultParkedTTL + time.Second)
	gate.Observe(committedBlock(t, key, 5))

	require.Equal(t, []uint64{5, 6}, q.nonces())
	require.Nil(t, gate.IsPending(future.Hash()))
}

// Once TTL has already dropped a parked tx, a later gap-fill must not enqueue it.
func TestNonceGate_ExpiredParkedNotReleasedAfterSweep(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	stale := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), stale))

	now = now.Add(defaultParkedTTL + time.Second)
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Nil(t, gate.IsPending(stale.Hash()))

	gate.Observe(committedBlock(t, key, 5))
	require.Equal(t, []uint64{5}, q.nonces(), "a swept parked tx must not come back on commit")
}

// A global parked cap drops the oldest parked tx across senders so many
// abandoned gaps cannot exhaust memory inside the TTL window.
func TestNonceGate_GlobalParkedCapEvictsOldest(t *testing.T) {
	keyA, keyB, keyC := newKey(t), newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	state.set(senderAddr(keyB), 5)
	state.set(senderAddr(keyC), 5)
	gate, _ := newTestGate(state)
	gate.maxParked = 2

	now := time.Now()
	gate.now = func() time.Time { return now }

	txA := newValidTx(t, keyA, validTxOpts{nonce: 6})
	txB := newValidTx(t, keyB, validTxOpts{nonce: 6})
	txC := newValidTx(t, keyC, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), txA))
	now = now.Add(time.Second)
	require.NoError(t, gate.Admit(context.Background(), txB))
	now = now.Add(time.Second)
	require.NoError(t, gate.Admit(context.Background(), txC))

	require.Nil(t, gate.IsPending(txA.Hash()), "oldest parked tx is dropped at the global cap")
	require.Equal(t, txB.Hash(), gate.IsPending(txB.Hash()).Hash())
	require.Equal(t, txC.Hash(), gate.IsPending(txC.Hash()).Hash())
	require.Equal(t, 2, parkedCount(gate))
	require.Equal(t, 2, parkedSenderCount(gate))
}

// Replacing a parked nonce does not consume an extra global slot or evict others.
func TestNonceGate_GlobalCapSkipsReplacement(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	state := newStubState()
	state.set(senderAddr(keyA), 5)
	state.set(senderAddr(keyB), 5)
	gate, _ := newTestGate(state)
	gate.maxParked = 1

	first := newValidTx(t, keyA, validTxOpts{nonce: 6, gas: 21_000})
	second := newValidTx(t, keyA, validTxOpts{nonce: 6, gas: 22_000})
	other := newValidTx(t, keyB, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), first))
	require.NoError(t, gate.Admit(context.Background(), second))

	require.Nil(t, gate.IsPending(first.Hash()))
	require.NotNil(t, gate.IsPending(second.Hash()))
	require.Equal(t, 1, parkedCount(gate))

	require.NoError(t, gate.Admit(context.Background(), other))
	require.Nil(t, gate.IsPending(second.Hash()), "the replaced sender is the oldest slot")
	require.NotNil(t, gate.IsPending(other.Hash()))
	require.Equal(t, 1, parkedCount(gate))
}

// The per-sender cap still rejects before the global cap evicts anything.
func TestNonceGate_PerSenderCapStillApplies(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, _ := newTestGate(state)
	gate.maxPerSender = 2
	gate.maxParked = 100

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6})))
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 7})))
	require.ErrorIs(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 8})), errTooManyParked)
	require.Equal(t, 2, parkedCount(gate))
}

// After TTL clears parked txs, LRU can finally enforce maxSenders.
func TestNonceGate_TTLThenLRUEnforcesMaxSenders(t *testing.T) {
	keys := []*ecdsa.PrivateKey{newKey(t), newKey(t), newKey(t), newKey(t), newKey(t)}
	traffic := newKey(t)
	state := newStubState()
	for _, key := range keys {
		state.set(senderAddr(key), 5)
	}
	state.set(senderAddr(traffic), 1)
	gate, _ := newTestGate(state)
	gate.maxSenders = 2

	now := time.Now()
	gate.now = func() time.Time { return now }

	for _, key := range keys {
		require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 6})))
	}
	require.Equal(t, 5, cachedSenderCount(gate), "parked senders must survive LRU until they expire")

	now = now.Add(defaultParkedTTL + time.Second)
	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, traffic, validTxOpts{nonce: 1})))

	require.Equal(t, 0, parkedCount(gate))
	require.LessOrEqual(t, cachedSenderCount(gate), 2)
}

// An invalidated commit must not advance the nonce, but the parked dependent
// still expires on the next sweep instead of living forever.
func TestNonceGate_InvalidCommitThenTTLDropsDependent(t *testing.T) {
	key := newKey(t)
	state := newStubState()
	state.set(senderAddr(key), 5)
	gate, q := newTestGate(state)

	now := time.Now()
	gate.now = func() time.Time { return now }

	require.NoError(t, gate.Admit(context.Background(), newValidTx(t, key, validTxOpts{nonce: 5})))
	tx6 := newValidTx(t, key, validTxOpts{nonce: 6})
	require.NoError(t, gate.Admit(context.Background(), tx6))

	block := committedBlock(t, key, 5)
	block[0].FabricValid = false
	gate.Observe(block)
	require.Equal(t, tx6.Hash(), gate.IsPending(tx6.Hash()).Hash())

	now = now.Add(defaultParkedTTL + time.Second)
	gate.Observe(nil)

	require.Equal(t, []uint64{5}, q.nonces())
	require.Nil(t, gate.IsPending(tx6.Hash()))
	require.Equal(t, 0, parkedCount(gate))
}

// Concurrent parks serialize on the gate lock, so the global cap is a hard
// ceiling rather than a best-effort bound.
func TestNonceGate_ConcurrentParkRespectsGlobalCap(t *testing.T) {
	const n = 50
	keys := make([]*ecdsa.PrivateKey, n)
	txs := make([]*types.Transaction, n)
	state := newStubState()
	for i := range n {
		keys[i] = newKey(t)
		state.set(senderAddr(keys[i]), 0)
		txs[i] = newValidTx(t, keys[i], validTxOpts{nonce: 1})
	}
	gate, _ := newTestGate(state)
	gate.maxParked = 8

	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(tx *types.Transaction) {
			defer wg.Done()
			errCh <- gate.Admit(context.Background(), tx)
		}(txs[i])
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(t, 8, parkedCount(gate))
	require.Equal(t, 8, parkedSenderCount(gate))
	pending := 0
	for _, tx := range txs {
		if gate.IsPending(tx.Hash()) != nil {
			pending++
		}
	}
	require.Equal(t, 8, pending)
}
