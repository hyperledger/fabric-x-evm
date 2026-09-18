/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/stretchr/testify/require"

	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// keyedEndorser endorses each transaction with the keys it was told that
// transaction touches, which is what decides whether two of them clash.
type keyedEndorser struct {
	t    *testing.T
	mu   sync.Mutex
	keys map[common.Hash][]string
	err  error
	n    int
}

func newKeyedEndorser(t *testing.T) *keyedEndorser {
	return &keyedEndorser{t: t, keys: make(map[common.Hash][]string)}
}

func (e *keyedEndorser) touches(tx *types.Transaction, keys ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keys[tx.Hash()] = keys
}

func (e *keyedEndorser) failWith(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *keyedEndorser) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n
}

func (e *keyedEndorser) ExecuteTransaction(_ context.Context, tx *types.Transaction) (sdk.Endorsement, error) {
	e.mu.Lock()
	e.n++
	err, keys := e.err, e.keys[tx.Hash()]
	e.mu.Unlock()

	if err != nil {
		return sdk.Endorsement{}, err
	}
	if len(keys) == 0 {
		keys = []string{tx.Hash().Hex()} // unique, so it clashes with nothing
	}
	return endorsementFor(fabricxPayload(e.t, keys...)), nil
}

func newDepQueue(t *testing.T) (*DepGraphQueue, *keyedEndorser) {
	t.Helper()
	e := newKeyedEndorser(t)
	q := NewDepGraphQueue()
	require.NoError(t, q.Bind(e, cmn.ProtocolFabricX))
	t.Cleanup(q.Close)
	return q, e
}

func txWithNonce(n uint64) *types.Transaction {
	return types.NewTx(&types.LegacyTx{Nonce: n, Gas: 21000, GasPrice: big.NewInt(0)})
}

// readyCount is how many transactions the manager has released but no worker
// has taken, read without consuming one.
func readyCount(q *DepGraphQueue) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.ready)
}

func requireReady(t *testing.T, q *DepGraphQueue, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return readyCount(q) == want }, 2*time.Second, 5*time.Millisecond,
		"expected %d ready, got %d", want, readyCount(q))
}

// blockWith builds a committed block carrying the given transactions.
func blockWith(status uint8, txs ...*types.Transaction) *domain.Block {
	b := &domain.Block{}
	for _, tx := range txs {
		h := tx.Hash()
		b.Transactions = append(b.Transactions, domain.Transaction{TxHash: h.Bytes(), Status: status})
	}
	return b
}

// A transaction clashing with nothing goes straight through.
func TestDepGraphQueue_ClashFreePassesThrough(t *testing.T) {
	q, _ := newDepQueue(t)
	tx := txWithNonce(1)

	q.Enqueue(tx)
	requireReady(t, q, 1)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx.Hash(), got.Hash())
}

// The whole point: a second transaction on the same key waits until the first
// commits, so the two are never in flight together.
func TestDepGraphQueue_ClashingHeldUntilHandle(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(second)
	require.Never(t, func() bool { return readyCount(q) > 0 }, 300*time.Millisecond, 10*time.Millisecond,
		"the clashing transaction must not be released while the first is in flight")

	require.NoError(t, q.Handle(context.Background(), blockWith(1, first)))
	requireReady(t, q, 1)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())
}

// Transactions on different keys do not wait for each other.
func TestDepGraphQueue_NonClashingBothRelease(t *testing.T) {
	q, e := newDepQueue(t)
	a, b := txWithNonce(1), txWithNonce(2)
	e.touches(a, "alice")
	e.touches(b, "bob")

	q.Enqueue(a)
	q.Enqueue(b)
	requireReady(t, q, 2)
}

// A rejected transaction is no longer in flight either, so whatever waited on it
// has to be released. Holding it back would strand the dependent forever.
func TestDepGraphQueue_RejectedBlockerStillReleases(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(second)
	require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond)

	// Status 0: committed but MVCC-invalid.
	require.NoError(t, q.Handle(context.Background(), blockWith(0, first)))
	requireReady(t, q, 1)
}

// A submission that never reaches a block still has to release its dependents,
// which is what Complete does outside the commit path.
func TestDepGraphQueue_CompleteReleasesDependents(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(second)
	require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond)

	q.Complete(first.Hash()) // submission failed, it will never commit
	requireReady(t, q, 1)
}

func TestDepGraphQueue_IsPendingAndInFlight(t *testing.T) {
	q, _ := newDepQueue(t)
	tx := txWithNonce(1)

	require.Nil(t, q.IsPending(tx.Hash()))
	require.Zero(t, q.InFlight())

	q.Enqueue(tx)
	require.NotNil(t, q.IsPending(tx.Hash()))
	require.Equal(t, 1, q.InFlight())

	q.Complete(tx.Hash())
	require.Nil(t, q.IsPending(tx.Hash()))
	require.Zero(t, q.InFlight())
}

func TestDepGraphQueue_CompleteUnknownHashIsNoop(t *testing.T) {
	q, _ := newDepQueue(t)
	require.NotPanics(t, func() { q.Complete(common.HexToHash("0xdeadbeef")) })
}

// A block can carry transactions this gateway never submitted.
func TestDepGraphQueue_HandleIgnoresUnknownTransactions(t *testing.T) {
	q, _ := newDepQueue(t)
	require.NoError(t, q.Handle(context.Background(), blockWith(1, txWithNonce(99))))

	total, _, _, _ := q.Stats()
	require.Equal(t, 1, total, "an unknown transaction still counts as processed")
}

func TestDepGraphQueue_HandleEmptyBlock(t *testing.T) {
	q, _ := newDepQueue(t)
	require.NoError(t, q.Handle(context.Background(), &domain.Block{}))
}

func TestDepGraphQueue_CloseUnblocksDequeue(t *testing.T) {
	q, _ := newDepQueue(t)

	done := make(chan bool, 1)
	go func() { _, ok := q.Dequeue(); done <- ok }()

	time.Sleep(50 * time.Millisecond) // let it block
	q.Close()

	select {
	case ok := <-done:
		require.False(t, ok, "Dequeue must report the queue closed")
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wake Dequeue")
	}
}

// Enqueue has no error return, so a failed endorsement is dropped rather than
// reported. It must not stay tracked, or InFlight never reaches zero.
func TestDepGraphQueue_EndorsementFailureDropsTransaction(t *testing.T) {
	q, e := newDepQueue(t)
	e.failWith(errors.New("endorser down"))

	tx := txWithNonce(1)
	q.Enqueue(tx)

	require.Eventually(t, func() bool { return q.InFlight() == 0 }, 2*time.Second, 5*time.Millisecond,
		"a transaction that could not be endorsed must not stay tracked")
	require.Zero(t, readyCount(q))
}

func TestDepGraphQueue_DuplicateEnqueueIgnored(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)

	q.Enqueue(tx)
	q.Enqueue(tx)
	requireReady(t, q, 1)

	require.Equal(t, 1, q.InFlight())
	require.Equal(t, 1, e.calls(), "the duplicate must not be endorsed again")
}

// Batch IDs start at 1: the constructor spins on CompareAndSwap(id-1, id)
// against a zero-valued atomic, so a batch with ID 0 is never released.
func TestDepGraphQueue_BatchIDsStartAtOne(t *testing.T) {
	q, _ := newDepQueue(t)

	q.Enqueue(txWithNonce(1))
	requireReady(t, q, 1)
	require.Equal(t, uint64(1), q.batchID.Load())

	q.Enqueue(txWithNonce(2))
	requireReady(t, q, 2)
	require.Equal(t, uint64(2), q.batchID.Load())
}

func TestDepGraphQueue_EnqueueAfterCloseIsIgnored(t *testing.T) {
	q, _ := newDepQueue(t)
	q.Close()

	require.NotPanics(t, func() { q.Enqueue(txWithNonce(1)) })
	require.Zero(t, q.InFlight())
}

// Fabric-X only: a classic Fabric endorsement carries no namespaces to schedule
// on, so this fails at wiring time rather than dropping every transaction.
func TestDepGraphQueue_BindRejectsClassicFabric(t *testing.T) {
	q := NewDepGraphQueue()
	t.Cleanup(q.Close)

	err := q.Bind(newKeyedEndorser(t), cmn.ProtocolFabric)
	require.Error(t, err)
	require.Contains(t, err.Error(), cmn.ProtocolFabricX)
}

func TestDepGraphQueue_BindRejectsUnknownProtocol(t *testing.T) {
	q := NewDepGraphQueue()
	t.Cleanup(q.Close)
	require.Error(t, q.Bind(newKeyedEndorser(t), "bogus"))
}

// An empty protocol means fabric-x, as everywhere else in the gateway.
func TestDepGraphQueue_BindAcceptsDefaultProtocol(t *testing.T) {
	q := NewDepGraphQueue()
	t.Cleanup(q.Close)
	require.NoError(t, q.Bind(newKeyedEndorser(t), ""))
}

// Three transactions on one key come out one at a time, each waiting for the
// one before it to commit. Which of the contenders goes first is not fixed:
// endorse workers run in parallel, so batch order follows endorsement, not
// Enqueue. The nonce gate already serialises a single sender, so only
// cross-sender transactions reach here together and their order is free.
func TestDepGraphQueue_ChainOfClashesReleasesOneAtATime(t *testing.T) {
	q, e := newDepQueue(t)
	txs := []*types.Transaction{txWithNonce(1), txWithNonce(2), txWithNonce(3)}
	for _, tx := range txs {
		e.touches(tx, "balance")
		q.Enqueue(tx)
	}

	seen := make(map[common.Hash]struct{}, len(txs))
	for range txs {
		requireReady(t, q, 1)
		require.Never(t, func() bool { return readyCount(q) > 1 }, 150*time.Millisecond, 10*time.Millisecond,
			"only one transaction on a contended key may be in flight")

		got, ok := q.Dequeue()
		require.True(t, ok)
		seen[got.Hash()] = struct{}{}
		require.NoError(t, q.Handle(context.Background(), blockWith(1, got)))
	}

	require.Len(t, seen, len(txs), "every contender must eventually be released")
}

// conflictEnq is how the prototype reports contention: the two that waited are
// counted, the one that went straight through is not.
func TestDepGraphQueue_CountsConflicts(t *testing.T) {
	q, e := newDepQueue(t)
	txs := []*types.Transaction{txWithNonce(1), txWithNonce(2), txWithNonce(3)}
	for _, tx := range txs {
		e.touches(tx, "balance")
	}

	q.Enqueue(txs[0])
	requireReady(t, q, 1)
	first, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(txs[1])
	q.Enqueue(txs[2])

	require.NoError(t, q.Handle(context.Background(), blockWith(1, first)))
	requireReady(t, q, 1)
	second, ok := q.Dequeue()
	require.True(t, ok)
	require.NoError(t, q.Handle(context.Background(), blockWith(1, second)))
	requireReady(t, q, 1)

	_, _, totalEnq, conflicts := q.Stats()
	require.Equal(t, 3, totalEnq)
	require.Equal(t, 2, conflicts, "the two that waited are counted, the first is not")
}

// Transactions on disjoint keys are not serialised by the scheduler.
func TestDepGraphQueue_DisjointKeysReportNoConflicts(t *testing.T) {
	q, e := newDepQueue(t)
	for i := range 20 {
		tx := txWithNonce(uint64(i))
		e.touches(tx, tx.Hash().Hex())
		q.Enqueue(tx)
	}
	requireReady(t, q, 20)

	_, _, totalEnq, conflicts := q.Stats()
	require.Equal(t, 20, totalEnq)
	require.Zero(t, conflicts, "disjoint keys must not be reported as contended")
}

// A dropped transaction is invisible to the caller, so the count is the only
// way to see it. Enqueue cannot report the failure itself (#379).
func TestDepGraphQueue_DroppedCounted(t *testing.T) {
	q, e := newDepQueue(t)
	e.failWith(errors.New("endorser down"))

	q.Enqueue(txWithNonce(1))
	require.Eventually(t, func() bool { return q.Dropped() == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Zero(t, q.InFlight())
}

// Many producers and consumers at once, which is how the gateway actually
// drives the queue: workerCount workers dequeue while the nonce gate admits.
func TestDepGraphQueue_ConcurrentProducersAndConsumers(t *testing.T) {
	q, _ := newDepQueue(t)

	const total = 200
	var wg sync.WaitGroup
	for i := range total {
		wg.Add(1)
		go func() { defer wg.Done(); q.Enqueue(txWithNonce(uint64(i))) }()
	}
	wg.Wait()

	seen := make(map[common.Hash]struct{}, total)
	var mu sync.Mutex
	var consumers sync.WaitGroup
	for range 8 {
		consumers.Add(1)
		go func() {
			defer consumers.Done()
			for {
				tx, ok := q.Dequeue()
				if !ok {
					return
				}
				mu.Lock()
				seen[tx.Hash()] = struct{}{}
				done := len(seen) == total
				mu.Unlock()
				if done {
					return
				}
			}
		}()
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == total
	}, 10*time.Second, 10*time.Millisecond, "every enqueued transaction must come back exactly once")

	q.Close()
	consumers.Wait()
}

// Close twice must not panic: the gateway closes the queue on shutdown and a
// test cleanup may close it again.
func TestDepGraphQueue_CloseIsIdempotent(t *testing.T) {
	q, _ := newDepQueue(t)
	require.NotPanics(t, func() { q.Close(); q.Close() })
}

// Complete twice must not feed the same node back twice, which would corrupt
// the manager's view of what is still in flight.
func TestDepGraphQueue_CompleteTwiceIsSafe(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, _ = q.Dequeue()
	q.Enqueue(second)

	q.Complete(first.Hash())
	q.Complete(first.Hash())
	requireReady(t, q, 1)

	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())
}
