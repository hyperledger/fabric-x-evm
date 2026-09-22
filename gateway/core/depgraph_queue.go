/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"

	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

var depGraphLogger = flogging.MustGetLogger("gateway.core.depgraph_queue")

const (
	// defaultEndorseWorkers drain the admitted channel. Enqueue runs under the
	// nonce gate's write lock, so the endorsement cannot happen there.
	defaultEndorseWorkers = 8
	// defaultChanSize buffers each hop. The manager sizes an internal channel
	// from cap(IncomingTxs), so an unbuffered one would starve it.
	defaultChanSize = 1024
	// defaultWaitingTxsLimit caps what the graph holds. It must exceed
	// defaultBatchThreshold or Acquire blocks forever.
	defaultWaitingTxsLimit = 1 << 16
	// defaultBatchThreshold and defaultBatchTimeout size the batches handed to
	// the manager: send at the threshold, or on the timeout if traffic is thin.
	// Both are guesses pending guidance from the committer side.
	defaultBatchThreshold = 64
	defaultBatchTimeout   = 10 * time.Millisecond
)

// txRefID is the id the manager knows a transaction by: its Ethereum hash, in
// hex. hashFromTxRefID reverses it. The manager echoes the id back on release
// and that is how markReady finds the transaction again, so the two must stay
// in step - there is no Fabric transaction id involved anywhere here.
func txRefID(hash common.Hash) string       { return hash.Hex() }
func hashFromTxRefID(id string) common.Hash { return common.HexToHash(id) }

// txEndorser produces the read-write set the manager schedules on.
type txEndorser interface {
	ExecuteTransaction(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error)
}

// trackedTx is one transaction between Enqueue and Complete.
//
// node is nil until the manager releases it. Feeding a node back is what
// unblocks its dependents, so it is kept for the whole lifetime rather than
// dropped at Dequeue, and cleared once fed back so it cannot be sent twice.
type trackedTx struct {
	tx   *types.Transaction
	node *dependencygraph.TransactionNode
}

// DepGraphQueue is a TxQueueInterface backed by the committer's dependency
// manager, so two transactions touching the same key are never in flight at
// once.
//
// Enqueue endorses for a read-write set and hands it to the manager, Dequeue
// serves whatever the manager says is clash free, and Handle tells the manager
// what committed, which is what releases the rest.
//
// Fabric-X only. The manager schedules on applicationpb namespaces, which is
// what a fabric-x endorsement carries; a classic Fabric endorsement carries a
// different message entirely. Bind rejects that rather than letting it fail
// per transaction at runtime.
//
// Persistence: in-memory only, like the queues it replaces.
type DepGraphQueue struct {
	incoming  chan *dependencygraph.TransactionBatch
	outgoing  chan dependencygraph.TxNodeBatch
	validated chan dependencygraph.TxNodeBatch
	admitted  chan *types.Transaction
	endorsed  chan *servicepb.TxWithRef

	// batchID is owned by the batcher goroutine alone. The manager needs
	// batches to arrive in strictly increasing order, which one sender gives
	// for free and concurrent senders cannot: numbering and sending are not
	// atomic together, so a worker descheduled between the two lets a later
	// batch overtake an earlier one and the constructor spins forever.
	batchID uint64

	// metrics is the manager's own registry. The package already counts the
	// transactions its graph is holding, so contention is read from here
	// rather than guessed at from the outside.
	metrics     *monitoring.Provider
	depWaitPeak atomic.Int64

	mu      sync.RWMutex
	cond    *sync.Cond
	ready   []*types.Transaction
	tracked map[common.Hash]*trackedTx
	done    bool

	// endorser is written once by Bind, which then starts the workers that read
	// it. Starting them after the write is the happens-before, so no lock.
	endorser txEndorser

	total    int
	invalid  int
	totalEnq int
	dropped  int

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce func()
}

// NewDepGraphQueue starts the dependency manager and the goroutines feeding it.
// Bind must be called before the first Enqueue.
func NewDepGraphQueue() *DepGraphQueue {
	q := &DepGraphQueue{
		incoming:  make(chan *dependencygraph.TransactionBatch, defaultChanSize),
		outgoing:  make(chan dependencygraph.TxNodeBatch, defaultChanSize),
		validated: make(chan dependencygraph.TxNodeBatch, defaultChanSize),
		admitted:  make(chan *types.Transaction, defaultChanSize),
		endorsed:  make(chan *servicepb.TxWithRef, defaultChanSize),
		tracked:   make(map[common.Hash]*trackedTx),
	}
	q.cond = sync.NewCond(&q.mu)
	q.ctx, q.cancel = context.WithCancel(context.Background())
	q.closeOnce = sync.OnceFunc(q.shutdown)

	// Required, not optional: newPerformanceMetrics calls straight through to
	// the provider, so a nil one panics on construction.
	q.metrics = monitoring.NewProvider()

	mgr := dependencygraph.NewManager(&dependencygraph.Parameters{
		IncomingTxs:               q.incoming,
		OutgoingDepFreeTxsNode:    q.outgoing,
		IncomingValidatedTxsNode:  q.validated,
		NumOfLocalDepConstructors: 1,
		WaitingTxsLimit:           defaultWaitingTxsLimit,
		PrometheusMetricsProvider: q.metrics,
	})

	q.wg.Go(func() { mgr.Run(q.ctx) })
	q.wg.Go(q.drainReleased)
	return q
}

// Bind supplies the endorsement client and starts the goroutines that use it.
// It is separate from the constructor because BuildGateway only creates the
// client after the queue.
//
// The workers start here rather than in the constructor so that the endorser is
// written before the goroutines that read it exist. That ordering is what makes
// the field safe to read without a lock.
func (q *DepGraphQueue) Bind(e txEndorser) {
	q.endorser = e

	for range defaultEndorseWorkers {
		q.wg.Go(q.endorseLoop)
	}
	q.wg.Go(q.batchLoop)
}

// Enqueue accepts a transaction for scheduling. It must not block: the nonce
// gate calls it while holding its own write lock, so the endorsement happens on
// an endorse worker rather than here.
func (q *DepGraphQueue) Enqueue(tx *types.Transaction) {
	hash := tx.Hash()

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.done {
		return
	}
	if _, dup := q.tracked[hash]; dup {
		return
	}

	select {
	case q.admitted <- tx:
	default:
		// Enqueue cannot report this (#379) and the nonce gate holds its own
		// lock while calling us, so blocking is not an option either. Loud for
		// the prototype rather than a transaction quietly disappearing.
		panic(fmt.Sprintf("dependency manager queue: admitted channel full, tx %s", hash.Hex()))
	}
	q.tracked[hash] = &trackedTx{tx: tx}
	q.totalEnq++
}

// endorseLoop endorses admitted transactions and hands them to the manager.
func (q *DepGraphQueue) endorseLoop() {
	for {
		select {
		case <-q.ctx.Done():
			return
		case tx, ok := <-q.admitted:
			if !ok {
				return
			}
			q.submitToManager(tx)
		}
	}
}

// submitToManager endorses for a read-write set and hands the result to the
// batcher. A failure here drops the transaction: Enqueue has no error return,
// so there is nowhere to report it (#379).
func (q *DepGraphQueue) submitToManager(tx *types.Transaction) {
	hash := tx.Hash()

	end, err := q.endorser.ExecuteTransaction(q.ctx, tx)
	if err != nil {
		depGraphLogger.Errorf("endorse tx %s: %v", hash.Hex(), err)
		q.drop(hash)
		return
	}

	// An endorsement we cannot read a read-write set from means the endorser and
	// this queue disagree about the wire format, which no amount of dropping
	// recovers from. Loud for the prototype; see #386 for removing it before
	// production.
	content, err := txContent(end)
	if err != nil {
		panic(fmt.Sprintf("dependency manager queue: read-write set for tx %s: %v", hash.Hex(), err))
	}

	select {
	case q.endorsed <- &servicepb.TxWithRef{
		Ref:     &committerpb.TxRef{TxId: txRefID(hash)},
		Content: content,
	}:
	case <-q.ctx.Done():
		q.drop(hash)
	}
}

// batchLoop is the only sender to the manager, which is what keeps batch ids in
// order without a lock. It packs whatever the endorse workers produce, sending
// at the threshold or on the timeout so a thin stream is not held up.
func (q *DepGraphQueue) batchLoop() {
	ticker := time.NewTicker(defaultBatchTimeout)
	defer ticker.Stop()

	pending := make([]*servicepb.TxWithRef, 0, defaultBatchThreshold)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		// Ids start at 1: the constructor spins on CompareAndSwap(id-1, id)
		// against a zero-valued atomic, so a batch numbered 0 never lands.
		q.batchID++
		batch := &dependencygraph.TransactionBatch{ID: q.batchID, Txs: pending}
		select {
		case q.incoming <- batch:
		case <-q.ctx.Done():
		}
		pending = make([]*servicepb.TxWithRef, 0, defaultBatchThreshold)
	}

	for {
		select {
		case <-q.ctx.Done():
			return
		case tx := <-q.endorsed:
			pending = append(pending, tx)
			if len(pending) >= defaultBatchThreshold {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// drainReleased moves clash-free transactions to the ready list. The node is
// kept: feeding it back later is what releases whatever waited on it.
func (q *DepGraphQueue) drainReleased() {
	for {
		select {
		case <-q.ctx.Done():
			return
		case nodes, ok := <-q.outgoing:
			if !ok {
				return
			}
			q.markReady(nodes)
		}
	}
}

func (q *DepGraphQueue) markReady(nodes dependencygraph.TxNodeBatch) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, node := range nodes {
		hash := hashFromTxRefID(node.VerifierTx.GetRef().GetTxId())
		t, tracked := q.tracked[hash]
		if !tracked {
			continue
		}
		t.node = node
		q.ready = append(q.ready, t.tx)
	}
	q.cond.Broadcast()
}

// Dequeue blocks until the manager releases a transaction, or the queue closes.
func (q *DepGraphQueue) Dequeue() (*types.Transaction, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.ready) == 0 && !q.done {
		q.cond.Wait()
	}
	if len(q.ready) == 0 {
		return nil, false
	}

	tx := q.ready[0]
	q.ready[0] = nil
	q.ready = q.ready[1:]
	return tx, true
}

// IsPending reports a transaction the queue still tracks.
func (q *DepGraphQueue) IsPending(txHash common.Hash) *types.Transaction {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if t, ok := q.tracked[txHash]; ok {
		return t.tx
	}
	return nil
}

// InFlight is how many transactions the queue still tracks.
func (q *DepGraphQueue) InFlight() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracked)
}

// Complete drops a transaction from tracking and tells the manager it is done.
// A submission that failed never reaches a block, so without this anything
// waiting on it would wait forever.
func (q *DepGraphQueue) Complete(hash common.Hash) {
	q.release(q.takeNodes(hash))
}

// Handle tells the manager which transactions committed. Rejected ones go back
// too: either way they are no longer in flight, and holding one back strands
// everything behind it. The whole block goes in one pass, under one lock.
func (q *DepGraphQueue) Handle(_ context.Context, block *domain.Block) error {
	q.sampleDepWait()
	if len(block.Transactions) == 0 {
		return nil
	}

	hashes := make([]common.Hash, 0, len(block.Transactions))
	invalid := 0
	for _, tx := range block.Transactions {
		hashes = append(hashes, common.BytesToHash(tx.TxHash))
		if tx.Status == 0 {
			invalid++
		}
	}

	q.mu.Lock()
	q.total += len(hashes)
	q.invalid += invalid
	nodes := q.takeNodesLocked(hashes)
	q.mu.Unlock()

	q.release(nodes)
	return nil
}

// takeNodes untracks one transaction and returns its node if it has one.
func (q *DepGraphQueue) takeNodes(hash common.Hash) dependencygraph.TxNodeBatch {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.takeNodesLocked([]common.Hash{hash})
}

// takeNodesLocked untracks each hash and collects the nodes that still need
// feeding back. Clearing node is what makes a second Complete a no-op.
// Caller holds q.mu.
func (q *DepGraphQueue) takeNodesLocked(hashes []common.Hash) dependencygraph.TxNodeBatch {
	var nodes dependencygraph.TxNodeBatch
	for _, hash := range hashes {
		t, ok := q.tracked[hash]
		if !ok {
			continue
		}
		if t.node != nil {
			nodes = append(nodes, t.node)
			t.node = nil
		}
		delete(q.tracked, hash)
	}
	return nodes
}

// release hands finished transactions back to the manager, which is what frees
// their dependents. This blocks rather than dropping: a node that never arrives
// strands everything behind it for good.
func (q *DepGraphQueue) release(nodes dependencygraph.TxNodeBatch) {
	if len(nodes) == 0 {
		return
	}
	select {
	case q.validated <- nodes:
	case <-q.ctx.Done():
	}
}

// drop removes a transaction that will never reach the manager.
func (q *DepGraphQueue) drop(hash common.Hash) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.tracked[hash]; !ok {
		return
	}
	delete(q.tracked, hash)
	q.dropped++
}

// Close stops the manager and wakes anyone blocked in Dequeue. Safe to call
// more than once.
func (q *DepGraphQueue) Close() { q.closeOnce() }

func (q *DepGraphQueue) shutdown() {
	q.mu.Lock()
	q.done = true
	q.cond.Broadcast()
	q.mu.Unlock()

	q.cancel()
	q.wg.Wait()
}

// Stats returns committed, invalid and enqueued counts. The conflict slot stays
// zero: the graph reports contention as a peak, which divided by the enqueued
// total would not be the rate the shared callers print. PeakDependentTxs has it.
func (q *DepGraphQueue) Stats() (int, int, int, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.total, q.invalid, q.totalEnq, 0
}

// PeakDependentTxs is the most transactions the graph held on dependencies at
// once, sampled per block. The manager counts a transaction once for its
// batch-local dependencies and again for its global ones, so this is a measure
// of contention rather than a count of distinct transactions.
func (q *DepGraphQueue) PeakDependentTxs() int { 
	return int(q.depWaitPeak.Load()) 
}

// depWaitMetric is the manager's gauge of transactions currently waiting on
// dependencies, incremented per transaction that acquires one and decremented
// as dependents are freed.
const depWaitMetric = "coordinator_dependency_graph_dependent_transactions_queue_size"

// sampleDepWait records the gauge's high-water mark. It is called once per
// block, which keeps a Gather off the enqueue and release paths.
func (q *DepGraphQueue) sampleDepWait() {
	families, err := q.metrics.Registry().Gather()
	if err != nil {
		return
	}
	for _, f := range families {
		if f.GetName() != depWaitMetric {
			continue
		}
		for _, m := range f.GetMetric() {
			waiting := int64(m.GetGauge().GetValue())
			for {
				peak := q.depWaitPeak.Load()
				if waiting <= peak || q.depWaitPeak.CompareAndSwap(peak, waiting) {
					break
				}
			}
		}
	}
}

// Dropped is how many transactions never reached the manager, because the
// endorsement failed or the admitted channel was full. Enqueue cannot report
// either (#379), so this is the only place they are visible.
func (q *DepGraphQueue) Dropped() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.dropped
}
