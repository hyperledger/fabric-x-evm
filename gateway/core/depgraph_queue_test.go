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
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/stretchr/testify/require"

	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
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
	q := NewDepGraphQueue(nil)
	q.Bind(e)
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

func TestDepGraphQueue_DuplicateEnqueueRejected(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)

	require.NoError(t, q.Enqueue(tx))
	require.ErrorIs(t, q.Enqueue(tx), domain.ErrTransactionAlreadyPending)
	requireReady(t, q, 1)

	require.Equal(t, 1, q.InFlight())
	require.Equal(t, 1, e.calls(), "the duplicate must not be endorsed again")
}

// Batch ids start at 1: the constructor spins on CompareAndSwap(id-1, id)
// against a zero-valued atomic, so a batch numbered 0 is never released. If the
// batcher ever numbered from 0, nothing would come back at all.
func TestDepGraphQueue_BatchesAreReleased(t *testing.T) {
	q, _ := newDepQueue(t)

	q.Enqueue(txWithNonce(1))
	requireReady(t, q, 1)

	q.Enqueue(txWithNonce(2))
	requireReady(t, q, 2)
}

func TestDepGraphQueue_EnqueueAfterCloseIsRejected(t *testing.T) {
	q, _ := newDepQueue(t)
	q.Close()

	require.ErrorIs(t, q.Enqueue(txWithNonce(1)), domain.ErrQueueClosed)
	require.Zero(t, q.InFlight())
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

// Transactions on disjoint keys are not serialised by the scheduler.
func TestDepGraphQueue_DisjointKeys(t *testing.T) {
	q, e := newDepQueue(t)
	for i := range 20 {
		tx := txWithNonce(uint64(i))
		e.touches(tx, tx.Hash().Hex())
		q.Enqueue(tx)
	}
	requireReady(t, q, 20)
}

// Many producers and consumers at once, which is how the gateway actually
// drives the queue: workerCount workers dequeue while the nonce gate admits.
func TestDepGraphQueue_ConcurrentProducersAndConsumers(t *testing.T) {
	q, _ := newDepQueue(t)

	const total = 200
	var wg sync.WaitGroup
	for i := range total {
		wg.Go(func() { q.Enqueue(txWithNonce(uint64(i))) })
	}
	wg.Wait()

	seen := make(map[common.Hash]struct{}, total)
	var mu sync.Mutex
	var consumers sync.WaitGroup
	for range 8 {
		consumers.Go(func() {
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
		})
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

// A full admitted channel is reported to the caller instead of crashing the
// gateway, and the rejected transaction is not left tracked.
func TestDepGraphQueue_AdmittedChannelFullIsRejected(t *testing.T) {
	q := NewDepGraphQueue(nil)
	t.Cleanup(q.Close)
	// Never bound, so nothing drains admitted.
	for i := range defaultChanSize {
		require.NoError(t, q.Enqueue(txWithNonce(uint64(i))))
	}

	overflow := txWithNonce(uint64(defaultChanSize))
	if fxcommon.DebugBuild {
		require.Panics(t, func() { _ = q.Enqueue(overflow) }, "a debug build panics on a full queue")
	} else {
		require.ErrorIs(t, q.Enqueue(overflow), domain.ErrQueueFull)
	}
	require.Nil(t, q.IsPending(overflow.Hash()), "a rejected tx must not stay tracked")
	require.Equal(t, defaultChanSize, q.InFlight())
}

// noRWSetEndorser returns an endorsement with no read-write set in it.
type noRWSetEndorser struct{}

func (noRWSetEndorser) ExecuteTransaction(context.Context, *types.Transaction) (sdk.Endorsement, error) {
	return sdk.Endorsement{}, nil
}

// An endorsement we cannot read drops the transaction instead of crashing the gateway.
func TestDepGraphQueue_UnreadableEndorsementDropsTransaction(t *testing.T) {
	q := NewDepGraphQueue(nil)
	t.Cleanup(q.Close)
	// Not bound, so no endorse worker races the direct call below.
	q.endorser = noRWSetEndorser{}

	tx := txWithNonce(1)
	require.NoError(t, q.Enqueue(tx))

	if fxcommon.DebugBuild {
		require.Panics(t, func() { q.submitToManager(admittedTx{tx: tx}) }, "a debug build panics on an unreadable endorsement")
	} else {
		require.NotPanics(t, func() { q.submitToManager(admittedTx{tx: tx}) })
	}
	require.Nil(t, q.IsPending(tx.Hash()), "an unreadable endorsement must not stay tracked")
	require.Zero(t, len(q.endorsed), "nothing may reach the manager")
}

// releasedNode runs a transaction through the manager and returns its node,
// still held, so the test can hand it to markReady again.
func releasedNode(t *testing.T, q *DepGraphQueue, tx *types.Transaction) dependencygraph.TxNodeBatch {
	t.Helper()
	require.NoError(t, q.Enqueue(tx))
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.mu.RLock()
	defer q.mu.RUnlock()
	return dependencygraph.TxNodeBatch{q.tracked[tx.Hash()].node}
}

// A released node the queue no longer tracks still goes back to the manager,
// or whatever waits on it is held forever.
func TestDepGraphQueue_UntrackedReleasedNodeIsHandedBack(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	nodes := releasedNode(t, q, first)
	q.mu.Lock()
	delete(q.tracked, first.Hash())
	q.mu.Unlock()

	require.NoError(t, q.Enqueue(second))
	require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond)

	q.markReady(nodes)
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())
}

// A released node with a ref this queue never issued is handed back too, and
// a debug build panics on it.
func TestDepGraphQueue_UnrecognisedRefIsHandedBack(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	nodes := releasedNode(t, q, first)
	nodes[0].VerifierTx.Ref.TxId = "not-a-tx-hash"

	require.NoError(t, q.Enqueue(second))
	require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond)

	if fxcommon.DebugBuild {
		require.Panics(t, func() { q.markReady(nodes) }, "a debug build panics on an unrecognised ref")
	} else {
		require.NotPanics(t, func() { q.markReady(nodes) })
	}
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())
}

// The id the manager echoes back has to convert to the hash we tracked, or a
// released transaction is never found again.
func TestDepGraphQueue_TxRefIDRoundTrips(t *testing.T) {
	hash := txWithNonce(7).Hash()
	got, ok := hashFromTxRefID(txRefID(hash))
	require.True(t, ok)
	require.Equal(t, hash, got)

	// common.HexToHash would return a hash for each of these rather than fail.
	for _, bad := range []string{"", "0x", hash.Hex()[:40], "tx-id-1", hash.Hex() + "00"} {
		_, ok := hashFromTxRefID(bad)
		require.False(t, ok, "must reject %q", bad)
	}
}

// Every config field has a way of taking the queue down when it is not positive,
// so the constructor substitutes the default instead of passing the value on.
func TestDepGraphQueue_NonPositiveConfigFallsBackToDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.DepGraphQueue
	}{
		// A zero ChanSize leaves admitted unbuffered and a zero BatchTimeout
		// panics in time.NewTicker; the negative cases reach make() and NewTicker
		// with a negative argument, which panics outright.
		{"zero workers", &config.DepGraphQueue{EndorseWorkers: 0}},
		{"zero chan size", &config.DepGraphQueue{ChanSize: 0}},
		{"zero waiting limit", &config.DepGraphQueue{WaitingTxsLimit: 0}},
		{"zero batch threshold", &config.DepGraphQueue{BatchThreshold: 0}},
		{"zero batch timeout", &config.DepGraphQueue{BatchTimeout: 0}},
		{"negative workers", &config.DepGraphQueue{EndorseWorkers: -1}},
		{"negative chan size", &config.DepGraphQueue{ChanSize: -1}},
		{"negative waiting limit", &config.DepGraphQueue{WaitingTxsLimit: -1}},
		{"negative batch threshold", &config.DepGraphQueue{BatchThreshold: -1}},
		{"negative batch timeout", &config.DepGraphQueue{BatchTimeout: -time.Millisecond}},
		{"zero mvcc retries", &config.DepGraphQueue{MaxMVCCRetries: 0}},
		{"negative mvcc retries", &config.DepGraphQueue{MaxMVCCRetries: -1}},
		{"all at once", &config.DepGraphQueue{
			EndorseWorkers: -1, ChanSize: 0, WaitingTxsLimit: 0,
			BatchThreshold: -1, BatchTimeout: -time.Millisecond, MaxMVCCRetries: -1,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := NewDepGraphQueue(tc.cfg)
			t.Cleanup(q.Close)

			require.Equal(t, defaultEndorseWorkers, q.endorseWorkers)
			require.Equal(t, defaultBatchThreshold, q.batchThreshold)
			require.Equal(t, defaultBatchTimeout, q.batchTimeout)
			require.Equal(t, defaultChanSize, cap(q.admitted))
			require.Equal(t, defaultMaxMVCCRetries, q.maxMVCCRetries)

			// A defaulted WaitingTxsLimit is the one that cannot be read back off
			// the struct, so drive a transaction through to prove Acquire is not
			// parked on an empty slot pool.
			e := newKeyedEndorser(t)
			q.Bind(e)
			tx := txWithNonce(1)
			q.Enqueue(tx)
			requireReady(t, q, 1)
			got, ok := q.Dequeue()
			require.True(t, ok)
			require.Equal(t, tx.Hash(), got.Hash())
		})
	}
}

// Explicit positive values are honoured; only the broken ones are replaced.
func TestDepGraphQueue_PositiveConfigIsHonoured(t *testing.T) {
	q := NewDepGraphQueue(&config.DepGraphQueue{
		EndorseWorkers: 2, ChanSize: 16,
		WaitingTxsLimit: 32, BatchThreshold: 4, BatchTimeout: 5 * time.Millisecond,
		MaxMVCCRetries: 7,
	})
	t.Cleanup(q.Close)

	require.Equal(t, 2, q.endorseWorkers)
	require.Equal(t, 4, q.batchThreshold)
	require.Equal(t, 5*time.Millisecond, q.batchTimeout)
	require.Equal(t, 16, cap(q.admitted))
	require.Equal(t, 7, q.maxMVCCRetries)
}

// fabricCommittedBlock builds a block whose transactions all committed on Fabric.
func fabricCommittedBlock(txs ...*types.Transaction) *domain.Block {
	b := blockWith(1, txs...)
	for i := range b.Transactions {
		b.Transactions[i].FabricTxStatus = blocks.StatusCommitted
	}
	return b
}

// A transaction nothing overwrote keeps the endorsement it was scheduled with,
// so processTx need not execute it again. It is handed over once.
func TestDepGraphQueue_ReusesCurrentEndorsement(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)

	q.Enqueue(tx)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	_, ok = q.ReusableEndorsement(tx.Hash())
	require.True(t, ok)
	_, ok = q.ReusableEndorsement(tx.Hash())
	require.False(t, ok, "an endorsement is handed over once")
	require.Equal(t, 1, e.calls())
}

// A transaction that executed before a clashing one committed read the old
// value, so its endorsement is stale and it must execute again.
func TestDepGraphQueue_StaleEndorsementIsNotReused(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(second)
	require.Eventually(t, func() bool {
		q.mu.RLock()
		defer q.mu.RUnlock()
		tracked, ok := q.tracked[second.Hash()]
		return ok && tracked.endorsed
	}, 2*time.Second, 5*time.Millisecond, "second's endorsement must be kept before first commits")

	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(first)))
	requireReady(t, q, 1)
	_, ok = q.Dequeue()
	require.True(t, ok)

	_, ok = q.ReusableEndorsement(second.Hash())
	require.False(t, ok, "second read balance before first's write committed")
}

// An endorsement older than the reuse window is not handed back, even when
// nothing it read was written.
func TestDepGraphQueue_OldEndorsementIsNotReused(t *testing.T) {
	q, _ := newDepQueue(t)
	tx := txWithNonce(1)

	q.Enqueue(tx)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	for i := range uint64(reuseWindow) {
		require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(txWithNonce(100+i))))
	}
	_, ok = q.ReusableEndorsement(tx.Hash())
	require.False(t, ok)
}

// A write by a transaction Fabric rejected never happened, so it does not
// stale the endorsement of one that read the key.
func TestDepGraphQueue_InvalidWriteDoesNotStale(t *testing.T) {
	q, e := newDepQueue(t)
	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	q.Enqueue(second)
	require.Eventually(t, func() bool {
		q.mu.RLock()
		defer q.mu.RUnlock()
		tracked, ok := q.tracked[second.Hash()]
		return ok && tracked.endorsed
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, q.Handle(context.Background(), blockWith(1, first))) // FabricTxStatus left invalid
	requireReady(t, q, 1)
	_, ok = q.Dequeue()
	require.True(t, ok)

	_, ok = q.ReusableEndorsement(second.Hash())
	require.True(t, ok)
}

// processTx submits the queue's endorsement when it is current, and executes
// again only when the queue has none to give.
func TestProcessTx_ReusesQueueEndorsement(t *testing.T) {
	q, e := newDepQueue(t)
	reused, fresh := txWithNonce(1), txWithNonce(2)
	pResp := &peer.ProposalResponse{Response: &peer.Response{Status: fxcommon.StatusOK}}
	stub := &stubEndorser{execResp: pResp}
	g := &Gateway{TxQueue: q, endorsers: signingClient(stub), endorsementChan: make(chan EndorsedTx, 2)}

	q.Enqueue(reused)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	require.NoError(t, g.processTx(context.Background(), reused))
	require.Equal(t, reused.Hash(), (<-g.endorsementChan).Hash)
	require.Equal(t, 1, e.calls(), "the queue's endorsement is reused")
	require.True(t, stub.lastTS.IsZero(), "processTx must not execute again")

	require.NoError(t, g.processTx(context.Background(), fresh))
	require.Equal(t, fresh.Hash(), (<-g.endorsementChan).Hash)
	require.False(t, stub.lastTS.IsZero(), "an untracked tx is executed")
}

// mvccConflictBlockWithRaw is mvccConflictBlock with RawTx populated, as a real
// block always has it. Without the raw bytes a transaction the queue does not
// track cannot be reconstructed, so adoption needs them.
func mvccConflictBlockWithRaw(t *testing.T, txs ...*types.Transaction) *domain.Block {
	t.Helper()
	b := mvccConflictBlock(txs...)
	for i, tx := range txs {
		raw, err := tx.MarshalBinary()
		require.NoError(t, err)
		b.Transactions[i].RawTx = raw
	}
	return b
}

// mvccConflictBlock builds a committed block whose transactions the committer
// rejected with an MVCC conflict.
func mvccConflictBlock(txs ...*types.Transaction) *domain.Block {
	b := blockWith(0, txs...)
	for i := range b.Transactions {
		b.Transactions[i].FabricTxStatus = blocks.StatusMVCCConflict
	}
	return b
}

// fabricStatusBlock builds a committed block with an arbitrary Fabric status, for
// the failure modes that must not be retried.
func fabricStatusBlock(status blocks.Status, txs ...*types.Transaction) *domain.Block {
	b := blockWith(0, txs...)
	for i := range b.Transactions {
		b.Transactions[i].FabricTxStatus = status
	}
	return b
}

// An MVCC conflict is not the transaction's fault, so it is executed again and
// offered back to a worker rather than reported as failed.
func TestDepGraphQueue_MVCCConflictIsRetried(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)
	e.touches(tx, "balance")

	q.Enqueue(tx)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	require.NoError(t, q.Handle(context.Background(), mvccConflictBlock(tx)))

	// Offered to a worker again rather than reported as failed.
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx.Hash(), got.Hash())

	// Still tracked across the retry, so IsPending keeps answering for it and the
	// client never sees the transaction disappear.
	require.NotNil(t, q.IsPending(tx.Hash()))

	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(tx)))
	require.Nil(t, q.IsPending(tx.Hash()))
}

// The endorsement that conflicted must not be handed to the retry: it read state
// that has since been written, which is what the conflict means. With nothing to
// reuse, processTx falls through to executing the transaction again.
func TestDepGraphQueue_ConflictDiscardsStaleEndorsement(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)
	e.touches(tx, "balance")

	q.Enqueue(tx)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	// The first attempt had one to reuse.
	_, reusable := q.ReusableEndorsement(tx.Hash())
	require.True(t, reusable)

	require.NoError(t, q.Handle(context.Background(), mvccConflictBlock(tx)))
	requireReady(t, q, 1)
	_, ok = q.Dequeue()
	require.True(t, ok)

	_, reusable = q.ReusableEndorsement(tx.Hash())
	require.False(t, reusable, "the retry must execute again rather than reuse the endorsement that conflicted")
}

// A retry does not go back through the dependency manager. Re-entering it would
// build a second node whose keys collide with the first — which the manager
// still holds, since the queue keeps that node to protect the dependents — so
// the retry would wait on a node that is deliberately never fed back.
func TestDepGraphQueue_RetryDoesNotReEnterTheManager(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)
	e.touches(tx, "balance")

	q.Enqueue(tx)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)
	endorsements := e.calls()

	require.NoError(t, q.Handle(context.Background(), mvccConflictBlock(tx)))
	requireReady(t, q, 1)

	require.Equal(t, endorsements, e.calls(),
		"a tracked retry must not be endorsed by the queue again; the worker re-executes it")
}

// The point of the whole design: a transaction being retried has not written
// yet, so its dependents must keep waiting. Releasing its node would tell the
// manager otherwise, and the dependent would endorse against state missing this
// transaction's write and conflict in turn.
func TestDepGraphQueue_RetryKeepsHoldingDependents(t *testing.T) {
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

	require.NoError(t, q.Handle(context.Background(), mvccConflictBlock(first)))

	// The retry is served; the dependent is not. Exactly one transaction is
	// ready, and it is the one being retried.
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, first.Hash(), got.Hash(), "the retry is served, not the dependent")
	require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond,
		"the dependent must stay held while the transaction ahead of it is retried")

	// Only once the retry reaches a final outcome does the dependent go.
	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(first)))
	requireReady(t, q, 1)
	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())

	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(second)))
	require.Zero(t, q.InFlight())
}

// Exhausting the budget should be unreachable: a retry re-executes while the
// graph still holds every key it touches, so under the single-gateway assumption
// nothing else can be writing them and the second attempt should commit. Getting
// here means conflicts come from outside the graph, and releasing the node then
// admits the dependents into a system that just failed this transaction
// repeatedly. A debug build says so; a release build takes the lesser evil and
// releases them rather than stranding them for good.
func TestDepGraphQueue_ExhaustedRetryIsFatalInDebug(t *testing.T) {
	e := newKeyedEndorser(t)
	q := NewDepGraphQueue(&config.DepGraphQueue{MaxMVCCRetries: 1})
	q.Bind(e)
	t.Cleanup(q.Close)

	first, second := txWithNonce(1), txWithNonce(2)
	e.touches(first, "balance")
	e.touches(second, "balance")

	q.Enqueue(first)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)
	q.Enqueue(second)

	// One retry, which is allowed and must not panic.
	require.NoError(t, q.Handle(context.Background(), mvccConflictBlock(first)))
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, first.Hash(), got.Hash())

	// The next conflict is past the budget.
	exhaust := func() { _ = q.Handle(context.Background(), mvccConflictBlock(first)) }
	if fxcommon.DebugBuild {
		require.Panics(t, exhaust, "a debug build must not quietly release the dependents of a tx it gave up on")
		return
	}
	exhaust()
	require.Never(t, func() bool { return q.IsPending(first.Hash()) != nil }, 100*time.Millisecond, 10*time.Millisecond,
		"a transaction past its budget is untracked, not retried again")

	// Release build: the dependent is let go rather than stranded, and is
	// expected to conflict in turn — which is exactly why debug builds panic.
	requireReady(t, q, 1)
	got, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, second.Hash(), got.Hash())
	require.Nil(t, q.IsPending(first.Hash()))
}

// Only an MVCC conflict is retried. The other failures are deterministic, so
// another attempt would fail the same way and only delay the client's answer.
func TestDepGraphQueue_DeterministicFailuresAreNotRetried(t *testing.T) {
	for _, status := range []blocks.Status{
		blocks.StatusInvalidSignature,
		blocks.StatusMalformed,
		blocks.StatusDuplicateTxID,
	} {
		t.Run(status.String(), func(t *testing.T) {
			q, _ := newDepQueue(t)
			tx := txWithNonce(1)

			q.Enqueue(tx)
			requireReady(t, q, 1)
			_, ok := q.Dequeue()
			require.True(t, ok)

			require.NoError(t, q.Handle(context.Background(), fabricStatusBlock(status, tx)))
			require.Never(t, func() bool { return readyCount(q) > 0 }, 200*time.Millisecond, 10*time.Millisecond)
			require.Nil(t, q.IsPending(tx.Hash()))
		})
	}
}

// The crash case: a transaction that was in flight when the process died comes
// back as an MVCC conflict with nothing tracking it. That is exactly what the
// retry is for, so the queue adopts it rather than dropping it.
func TestDepGraphQueue_UntrackedConflictIsAdopted(t *testing.T) {
	q, e := newDepQueue(t)
	tx := txWithNonce(1)
	e.touches(tx, "balance")

	// No Enqueue: as far as this queue is concerned the transaction is unknown,
	// which is how a pre-crash submission looks after a restart.
	require.NoError(t, q.Handle(context.Background(), mvccConflictBlockWithRaw(t, tx)))

	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx.Hash(), got.Hash())
	require.NotNil(t, q.IsPending(tx.Hash()), "the adopted transaction is tracked like any other")

	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(tx)))
	require.Zero(t, q.InFlight())
}

// An adopted transaction gets a budget too, so a key that is permanently
// contended cannot make it cycle forever.
func TestDepGraphQueue_AdoptedConflictHasABudget(t *testing.T) {
	e := newKeyedEndorser(t)
	q := NewDepGraphQueue(&config.DepGraphQueue{MaxMVCCRetries: 1})
	q.Bind(e)
	t.Cleanup(q.Close)

	tx := txWithNonce(1)
	e.touches(tx, "balance")

	// Adoption consumes the first attempt, so with a budget of one the next
	// conflict is the end of it.
	require.NoError(t, q.Handle(context.Background(), mvccConflictBlockWithRaw(t, tx)))
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	exhaust := func() { _ = q.Handle(context.Background(), mvccConflictBlockWithRaw(t, tx)) }
	if fxcommon.DebugBuild {
		require.Panics(t, exhaust, "an adopted transaction past its budget is as fatal as any other")
		return
	}
	exhaust()
	require.Never(t, func() bool { return readyCount(q) > 0 }, 300*time.Millisecond, 10*time.Millisecond,
		"an adopted transaction past its budget must not be re-admitted")
	require.Zero(t, q.InFlight())
}

// A conflict whose raw bytes will not decode cannot be retried at all: there is
// nothing to execute. A committed block should never carry one, so a debug build
// surfaces it; a release build drops it and stays up.
func TestDepGraphQueue_UndecodableUntrackedConflictIsDropped(t *testing.T) {
	q, _ := newDepQueue(t)
	b := mvccConflictBlock(txWithNonce(99)) // blockWith leaves RawTx empty

	if fxcommon.DebugBuild {
		require.Panics(t, func() { _ = q.Handle(context.Background(), b) },
			"a debug build surfaces a conflict it cannot reconstruct")
	} else {
		require.NoError(t, q.Handle(context.Background(), b))
	}
	require.Zero(t, q.InFlight())
}

// A committed transaction with nothing tracking it is just old news: it was
// already completed, or it predates a restart. Nothing to do.
func TestDepGraphQueue_UntrackedCommitIsIgnored(t *testing.T) {
	q, _ := newDepQueue(t)
	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(txWithNonce(99))))
	require.Zero(t, q.InFlight())
}

// Complete and Handle read an untracked hash differently, and must keep doing
// so. In a committed block it means the entry was lost across a restart, which
// is worth adopting; from Complete it means a submission failed for a
// transaction that was already finished, which is worth nothing. Routing
// Complete through the adoption path would have it resurrect transactions that
// never reached the orderer.
func TestDepGraphQueue_CompleteNeverAdopts(t *testing.T) {
	e := newKeyedEndorser(t)
	q := NewDepGraphQueue(nil)
	q.Bind(e)
	t.Cleanup(q.Close)

	tx := txWithNonce(1)
	q.Complete(tx.Hash())

	require.Zero(t, q.InFlight(), "Complete must not create tracking for an unknown hash")
	require.Nil(t, q.IsPending(tx.Hash()))
	require.Never(t, func() bool { return readyCount(q) > 0 }, 300*time.Millisecond, 10*time.Millisecond,
		"Complete must never schedule work")
	require.Zero(t, e.calls(), "Complete must not cause an endorsement")
}

// An adopted transaction enters the manager as a new node, unlike a tracked
// retry, so it has to be serialised against whatever currently holds its keys.
// Post-restart that matters: the live transaction holding the key is exactly
// what the adopted one conflicted with.
func TestDepGraphQueue_AdoptedConflictWaitsForTheKeyHolder(t *testing.T) {
	q, e := newDepQueue(t)
	live, lost := txWithNonce(1), txWithNonce(2)
	e.touches(live, "balance")
	e.touches(lost, "balance")

	// live is in flight and holding "balance".
	q.Enqueue(live)
	requireReady(t, q, 1)
	_, ok := q.Dequeue()
	require.True(t, ok)

	// lost arrives as an untracked conflict (post-restart) on the same key.
	require.NoError(t, q.Handle(context.Background(), mvccConflictBlockWithRaw(t, lost)))

	// Must NOT be served while live still holds the key.
	require.Never(t, func() bool { return readyCount(q) > 0 }, 400*time.Millisecond, 10*time.Millisecond,
		"adopted tx must wait for the live tx holding the same key")
	// Once live commits, the adopted one runs.
	require.NoError(t, q.Handle(context.Background(), fabricCommittedBlock(live)))
	requireReady(t, q, 1)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, lost.Hash(), got.Hash())
}

// The retry path hits the same full-channel condition Enqueue does, so it gets
// the same treatment. The consequence is worse here — Enqueue can report
// ErrQueueFull to a caller that will retry, while an adopted transaction has no
// caller left — so a debug build surfaces it rather than losing it quietly.
func TestDepGraphQueue_FullChannelOnAdoptDebugPanics(t *testing.T) {
	q := NewDepGraphQueue(nil)
	t.Cleanup(q.Close)
	// Never bound, so nothing drains admitted; fill it to the brim.
	for i := range defaultChanSize {
		require.NoError(t, q.Enqueue(txWithNonce(uint64(i))))
	}

	// An untracked conflict would be adopted, but there is no room to admit it.
	lost := txWithNonce(uint64(defaultChanSize) + 1)
	block := mvccConflictBlockWithRaw(t, lost)

	if fxcommon.DebugBuild {
		require.Panics(t, func() { _ = q.Handle(context.Background(), block) },
			"a debug build panics when a retry cannot be admitted")
	} else {
		require.NoError(t, q.Handle(context.Background(), block))
	}
	require.Nil(t, q.IsPending(lost.Hash()), "a dropped retry must not stay tracked")
}
