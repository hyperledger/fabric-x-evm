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
}

func newStubState() *stubState {
	return &stubState{nonces: make(map[common.Address]uint64)}
}

func (s *stubState) NonceAt(_ context.Context, a common.Address, _ *big.Int) (uint64, error) {
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

// reconcileAdmit mimics the test backend's reconciling gate: resync then admit.
func reconcileAdmit(g *nonceGate, tx *types.Transaction) error {
	if err := g.Resync(context.Background(), tx); err != nil {
		return err
	}
	return g.Admit(context.Background(), tx)
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

func TestNonceGate_ReconcilesStaleCache(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	plain, q := newTestGate(state)

	require.NoError(t, reconcileAdmit(plain, newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())

	// The ledger advances by a path the gate did not observe.
	state.set(from, 8)

	// A nonce ahead of the stale cache still admits: the re-read picks up the
	// real committed nonce.
	require.NoError(t, reconcileAdmit(plain, newValidTx(t, key, validTxOpts{nonce: 8})))
	require.Equal(t, []uint64{5, 8}, q.nonces())
}

func TestNonceGate_ReconcilesAfterRevert(t *testing.T) {
	key := newKey(t)
	from := senderAddr(key)
	state := newStubState()
	state.set(from, 5)
	plain, q := newTestGate(state)

	require.NoError(t, reconcileAdmit(plain, newValidTx(t, key, validTxOpts{nonce: 5})))
	require.Equal(t, []uint64{5}, q.nonces())

	// A snapshot revert moves the ledger nonce back below the cache.
	state.set(from, 2)

	// A nonce below the stale cache still admits: the re-read follows the revert
	// down instead of rejecting it as too low.
	require.NoError(t, reconcileAdmit(plain, newValidTx(t, key, validTxOpts{nonce: 2})))
	require.Equal(t, []uint64{5, 2}, q.nonces())
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
