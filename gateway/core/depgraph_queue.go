/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	sdk "github.com/hyperledger/fabric-x-sdk"
)

var depGraphLogger = flogging.MustGetLogger("gateway.core.depgraph_queue")

const (
	// defaultEndorseWorkers drain the admitted channel. Enqueue runs under the
	// nonce gate's write lock, so the endorsement cannot happen there.
	defaultEndorseWorkers = 8
	// defaultChanSize buffers each hop. The manager sizes an internal channel
	// from cap(IncomingTxs), so an unbuffered one would starve it.
	defaultChanSize = 1024
	// defaultWaitingTxsLimit caps what the graph holds. Only a positive value
	// works: the manager's Acquire waits for one free slot and then subtracts the
	// whole batch, so it parks forever on a limit of zero and nothing can ever
	// release it. A limit below defaultBatchThreshold merely lets a batch overdraw
	// the counter, which serialises batches rather than breaking them.
	defaultWaitingTxsLimit = 1 << 16
	// defaultBatchThreshold and defaultBatchTimeout size the batches handed to
	// the manager: send at the threshold, or on the timeout if traffic is thin.
	// Both are guesses pending guidance from the committer side.
	defaultBatchThreshold = 64
	defaultBatchTimeout   = 10 * time.Millisecond
)

// positiveOrDefault resolves one optional override. A zero value is the
// documented way to ask for the default; a non-positive one is a mistake, and
// the specific ways each field breaks are why none of them is passed through:
// zero EndorseWorkers leaves nothing draining admitted until Enqueue panics on a
// full channel, a zero ChanSize makes that channel unbuffered so the panic comes
// almost immediately, a zero WaitingTxsLimit parks the manager's first Acquire
// forever, a negative BatchThreshold panics in make(), and a non-positive
// BatchTimeout panics in time.NewTicker.
func positiveOrDefault[T int | time.Duration](name string, override T, def T) T {
	if override <= 0 {
		if override < 0 {
			depGraphLogger.Warnf(
				"config.DepGraphQueue.%s must be positive, got %v; falling back to %v", name, override, def)
		}
		return def
	}
	return override
}

// txRefID is the id the manager knows a transaction by. TxRef.TxId is a plain
// string the manager never interprets, so this queue puts the Ethereum hash
// there and gets the same bytes back on release. No Fabric transaction id is
// involved on either side.
func txRefID(hash common.Hash) string { return hash.Hex() }

// hashFromTxRefID reverses txRefID, reporting whether the id was one this queue
// produced. The check is not decoration: common.HexToHash truncates or pads
// anything malformed and returns a hash rather than an error, so without it an
// unexpected id would silently become the zero hash and the transaction would
// look untracked.
func hashFromTxRefID(id string) (common.Hash, bool) {
	if len(id) != 2+2*common.HashLength || id[:2] != "0x" {
		return common.Hash{}, false
	}
	raw, err := hex.DecodeString(id[2:])
	if err != nil {
		return common.Hash{}, false
	}
	return common.BytesToHash(raw), true
}

// txEndorser produces the read-write set the manager schedules on. Satisfied by
// *Gateway, so this endorsement uses the same block context as the real one.
type txEndorser interface {
	ExecuteEthTx(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error)
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
// different message entirely, which submitToManager rejects.
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

	// metrics is the manager's own registry.
	metrics *monitoring.Provider

	endorseWorkers int
	batchThreshold int
	batchTimeout   time.Duration

	mu      sync.RWMutex
	cond    *sync.Cond
	ready   []*types.Transaction
	tracked map[common.Hash]*trackedTx
	done    bool

	// Statistics
	total   int
	invalid int

	// endorser is written once by Bind, which then starts the workers that read
	// it. Starting them after the write is the happens-before, so no lock.
	endorser txEndorser

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce func()
}

// NewDepGraphQueue starts the dependency manager and the goroutines feeding it.
// cfg may be nil, in which case all fields fall back to built-in defaults.
// Bind must be called before the first Enqueue.
func NewDepGraphQueue(cfg *config.DepGraphQueue) *DepGraphQueue {
	if cfg == nil {
		cfg = &config.DepGraphQueue{}
	}
	endorseWorkers := positiveOrDefault("EndorseWorkers", cfg.EndorseWorkers, defaultEndorseWorkers)
	chanSize := positiveOrDefault("ChanSize", cfg.ChanSize, defaultChanSize)
	waitingTxsLimit := positiveOrDefault("WaitingTxsLimit", cfg.WaitingTxsLimit, defaultWaitingTxsLimit)
	batchThreshold := positiveOrDefault("BatchThreshold", cfg.BatchThreshold, defaultBatchThreshold)
	batchTimeout := positiveOrDefault("BatchTimeout", cfg.BatchTimeout, defaultBatchTimeout)

	// Legal but slow, so it is worth saying out loud rather than validating away:
	// the manager takes a batch once one slot is free and then subtracts the whole
	// batch, so a limit under the threshold admits one batch at a time and waits
	// for it to drain before looking at the next.
	if waitingTxsLimit < batchThreshold {
		depGraphLogger.Warnf(
			"DepGraphQueue: WaitingTxsLimit %d is below BatchThreshold %d, so batches will be admitted one at a time",
			waitingTxsLimit, batchThreshold)
	}

	q := &DepGraphQueue{
		incoming:       make(chan *dependencygraph.TransactionBatch, chanSize),
		outgoing:       make(chan dependencygraph.TxNodeBatch, chanSize),
		validated:      make(chan dependencygraph.TxNodeBatch, chanSize),
		admitted:       make(chan *types.Transaction, chanSize),
		endorsed:       make(chan *servicepb.TxWithRef, chanSize),
		tracked:        make(map[common.Hash]*trackedTx),
		endorseWorkers: endorseWorkers,
		batchThreshold: batchThreshold,
		batchTimeout:   batchTimeout,
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
		WaitingTxsLimit:           waitingTxsLimit,
		PrometheusMetricsProvider: q.metrics,
	})

	q.wg.Go(func() { mgr.Run(q.ctx) })
	q.wg.Go(q.drainReleased)
	return q
}

// Bind supplies the endorser and starts the goroutines that use it.
// It is separate from the constructor because BuildGateway only creates the
// gateway after the queue.
//
// The workers start here rather than in the constructor so that the endorser is
// written before the goroutines that read it exist. That ordering is what makes
// the field safe to read without a lock.
func (q *DepGraphQueue) Bind(e txEndorser) {
	depGraphLogger.Infof("DepGraphQueue.Bind() starting %d endorse workers", q.endorseWorkers)
	q.endorser = e

	for range q.endorseWorkers {
		q.wg.Go(q.endorseLoop)
	}
	q.wg.Go(q.batchLoop)
}

// Enqueue accepts a transaction for scheduling. It must not block: the nonce
// gate calls it while holding its own write lock, so the endorsement happens on
// an endorse worker rather than here.
func (q *DepGraphQueue) Enqueue(tx *types.Transaction) {
	hash := tx.Hash()
	depGraphLogger.Debugf("DepGraphQueue.Enqueue() called with tx %s", hash.Hex())

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.done {
		depGraphLogger.Warnf("DepGraphQueue.Enqueue() tx %s ignored: queue is closed", hash.Hex())
		return
	}
	if _, dup := q.tracked[hash]; dup {
		depGraphLogger.Warnf("DepGraphQueue.Enqueue() tx %s ignored: already tracked", hash.Hex())
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
	depGraphLogger.Debugf("DepGraphQueue.submitToManager() endorsing tx %s", hash.Hex())

	end, err := q.endorser.ExecuteEthTx(q.ctx, tx)
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
		depGraphLogger.Errorf("read-write set for tx %s: %v", hash.Hex(), err)
		panic(fmt.Sprintf("dependency manager queue: read-write set for tx %s: %v", hash.Hex(), err))
	}

	select {
	case q.endorsed <- &servicepb.TxWithRef{
		Ref:     &committerpb.TxRef{TxId: txRefID(hash)},
		Content: content,
	}:
		depGraphLogger.Debugf("DepGraphQueue.submitToManager() tx %s queued for batching", hash.Hex())
	case <-q.ctx.Done():
		depGraphLogger.Errorf("failed to queue tx %s for batching: context done: %v", hash.Hex(), q.ctx.Err())
		q.drop(hash)
	}
}

// batchLoop is the only sender to the manager, which is what keeps batch ids in
// order without a lock. It packs whatever the endorse workers produce, sending
// at the threshold or on the timeout so a thin stream is not held up.
func (q *DepGraphQueue) batchLoop() {
	ticker := time.NewTicker(q.batchTimeout)
	defer ticker.Stop()

	pending := make([]*servicepb.TxWithRef, 0, q.batchThreshold)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		// Ids start at 1: the constructor spins on CompareAndSwap(id-1, id)
		// against a zero-valued atomic, so a batch numbered 0 never lands.
		q.batchID++
		batch := &dependencygraph.TransactionBatch{ID: q.batchID, Txs: pending}
		depGraphLogger.Debugf("DepGraphQueue.batchLoop() sending batch id=%d with %d txs to dependency manager", batch.ID, len(batch.Txs))
		select {
		case q.incoming <- batch:
			depGraphLogger.Debugf("DepGraphQueue.batchLoop() batch id=%d sent to dependency manager", batch.ID)
		case <-q.ctx.Done():
			depGraphLogger.Warnf("failed to send batch id=%d to dependency manager: context done: %v", batch.ID, q.ctx.Err())
		}
		pending = make([]*servicepb.TxWithRef, 0, q.batchThreshold)
	}

	for {
		select {
		case <-q.ctx.Done():
			return
		case tx := <-q.endorsed:
			pending = append(pending, tx)
			if len(pending) >= q.batchThreshold {
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
			depGraphLogger.Debugf("DepGraphQueue.drainReleased() received %d clash-free nodes from dependency manager", len(nodes))
			q.markReady(nodes)
		}
	}
}

func (q *DepGraphQueue) markReady(nodes dependencygraph.TxNodeBatch) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, node := range nodes {
		id := node.VerifierTx.GetRef().GetTxId()
		hash, ok := hashFromTxRefID(id)
		if !ok {
			// Only this queue sets TxId, so anything else means the manager and
			// the queue disagree about the reference. Loud for the prototype;
			// see #386.
			depGraphLogger.Errorf("unrecognised tx ref %q received from dependency manager", id)
			panic(fmt.Sprintf("dependency manager queue: unrecognised tx ref %q", id))
		}
		t, tracked := q.tracked[hash]
		if !tracked {
			depGraphLogger.Warnf("DepGraphQueue.markReady() tx %s node received from dependency manager but not tracked", hash.Hex())
			continue
		}
		t.node = node
		q.ready = append(q.ready, t.tx)
		depGraphLogger.Debugf("DepGraphQueue.markReady() tx %s marked ready", hash.Hex())
	}
	q.cond.Broadcast()
}

// Dequeue blocks until the manager releases a transaction, or the queue closes.
func (q *DepGraphQueue) Dequeue() (*types.Transaction, bool) {
	depGraphLogger.Debugf("DepGraphQueue.Dequeue() called")

	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.ready) == 0 && !q.done {
		q.cond.Wait()
	}
	if len(q.ready) == 0 {
		depGraphLogger.Debugf("DepGraphQueue.Dequeue() returning false (closed)")
		return nil, false
	}

	tx := q.ready[0]
	q.ready[0] = nil
	q.ready = q.ready[1:]
	depGraphLogger.Debugf("DepGraphQueue.Dequeue() returning tx %s", tx.Hash().Hex())
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
	depGraphLogger.Debugf("DepGraphQueue.Complete() called with tx %s", hash.Hex())
	q.mu.Lock()
	nodes := q.takeNodes([]common.Hash{hash})
	q.mu.Unlock()
	q.release(nodes)
}

// Handle tells the manager which transactions committed. Rejected ones go back
// too: either way they are no longer in flight, and holding one back strands
// everything behind it. The whole block goes in one pass, under one lock.
func (q *DepGraphQueue) Handle(_ context.Context, block *domain.Block) error {
	depGraphLogger.Debugf("DepGraphQueue.Handle() called with block containing %d txs", len(block.Transactions))
	if len(block.Transactions) == 0 {
		return nil
	}

	hashes := make([]common.Hash, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		q.total++
		if tx.Status == 0 {
			q.invalid++
		}

		hashes = append(hashes, common.BytesToHash(tx.TxHash))
	}

	q.mu.Lock()
	nodes := q.takeNodes(hashes)
	q.mu.Unlock()

	depGraphLogger.Debugf("DepGraphQueue.Handle() releasing %d nodes (total txs=%d)", len(nodes), len(hashes))
	q.release(nodes)
	return nil
}

// takeNodes untracks each hash and collects their nodes that still need
// feeding back. Clearing a node is what makes a second Complete a no-op.
// Caller must hold q.mu.
func (q *DepGraphQueue) takeNodes(hashes []common.Hash) dependencygraph.TxNodeBatch {
	var nodes dependencygraph.TxNodeBatch
	for _, hash := range hashes {
		t, ok := q.tracked[hash]
		if !ok {
			depGraphLogger.Warnf("DepGraphQueue.takeNodesLocked() tx %s not tracked", hash.Hex())
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
		depGraphLogger.Warnf("DepGraphQueue.drop() tx %s not tracked", hash.Hex())
		return
	}
	delete(q.tracked, hash)
}

// Close stops the manager and wakes anyone blocked in Dequeue. Safe to call
// more than once.
func (q *DepGraphQueue) Close() { q.closeOnce() }

func (q *DepGraphQueue) shutdown() {
	depGraphLogger.Infof("DepGraphQueue shutting down")
	q.mu.Lock()
	q.done = true
	q.cond.Broadcast()
	q.mu.Unlock()

	q.cancel()
	q.wg.Wait()
	depGraphLogger.Infof("DepGraphQueue shutdown complete")
}

// Stats returns committed, invalid and enqueued counts. The conflict slot stays
// zero: the graph reports contention as a peak, which divided by the enqueued
// total would not be the rate the shared callers print. PeakDependentTxs has it.
func (q *DepGraphQueue) Stats() (int, int, int, int) {
	// TODO: pull missing stats from the dependency manager
	return q.total, q.invalid, 0, 0
}
