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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"

	cmn "github.com/hyperledger/fabric-x-evm/common"
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
	// defaultWaitingTxsLimit caps what the graph holds. It must exceed the
	// largest batch or Acquire blocks forever; batches here are one transaction.
	defaultWaitingTxsLimit = 1 << 16
)

// txEndorser produces the read-write set the manager schedules on.
type txEndorser interface {
	ExecuteTransaction(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error)
}

// trackedTx is one transaction between Enqueue and Complete.
//
// node is nil until the manager releases it. Feeding a node back is what
// unblocks its dependents, so it is kept for the whole lifetime rather than
// dropped at Dequeue, and cleared once fed back so it cannot be sent twice.
//
// enqueuedAtBlock is the committed-block count when this transaction arrived.
// If the count has moved by the time the manager releases it, it waited on
// something: that is the only signal we have, since the package clears a node's
// dependencies before handing it over.
type trackedTx struct {
	tx              *types.Transaction
	node            *dependencygraph.TransactionNode
	enqueuedAtBlock uint64
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

	// sendMu keeps batch-ID assignment and the send to the manager atomic.
	// The local constructor spins on CompareAndSwap(id-1, id) and reads the
	// channel with one worker, so a batch arriving out of order wedges it: it
	// waits for a predecessor that is stuck behind it in the same channel.
	sendMu        sync.Mutex
	batchID       atomic.Uint64
	blocksHandled atomic.Uint64

	mu      sync.RWMutex
	cond    *sync.Cond
	ready   []*types.Transaction
	tracked map[common.Hash]*trackedTx
	byTxID  map[string]common.Hash
	done    bool

	endorser txEndorser

	total       int
	invalid     int
	totalEnq    int
	conflictEnq int
	dropped     int

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
		tracked:   make(map[common.Hash]*trackedTx),
		byTxID:    make(map[string]common.Hash),
	}
	q.cond = sync.NewCond(&q.mu)
	q.ctx, q.cancel = context.WithCancel(context.Background())
	q.closeOnce = sync.OnceFunc(q.shutdown)

	mgr := dependencygraph.NewManager(&dependencygraph.Parameters{
		IncomingTxs:               q.incoming,
		OutgoingDepFreeTxsNode:    q.outgoing,
		IncomingValidatedTxsNode:  q.validated,
		NumOfLocalDepConstructors: 1,
		WaitingTxsLimit:           defaultWaitingTxsLimit,
		// Required, not optional: newPerformanceMetrics calls straight through
		// to the provider, so a nil one panics on construction. Nothing serves
		// this registry yet; exposing it is the Phase 2 metrics item.
		PrometheusMetricsProvider: monitoring.NewProvider(),
	})

	q.spawn(func() { mgr.Run(q.ctx) })
	q.spawn(q.drainReleased)
	for range defaultEndorseWorkers {
		q.spawn(q.endorseLoop)
	}
	return q
}

func (q *DepGraphQueue) spawn(fn func()) {
	q.wg.Add(1)
	go func() { defer q.wg.Done(); fn() }()
}

// Bind supplies the endorsement client and checks the protocol. It is separate
// from the constructor because BuildGateway only creates the client after the
// queue, and it errors rather than degrading: a classic Fabric endorsement
// carries no namespaces to schedule on, so every transaction would be dropped.
func (q *DepGraphQueue) Bind(e txEndorser, protocol string) error {
	resolved, err := cmn.NormalizeProtocol(protocol)
	if err != nil {
		return err
	}
	if resolved != cmn.ProtocolFabricX {
		return fmt.Errorf("dependency manager queue requires %q, got %q", cmn.ProtocolFabricX, resolved)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.endorser = e
	return nil
}

// Enqueue accepts a transaction for scheduling. It must not block: the nonce
// gate calls it while holding its own write lock, so the endorsement happens on
// an endorse worker rather than here.
func (q *DepGraphQueue) Enqueue(tx *types.Transaction) {
	hash := tx.Hash()

	q.mu.Lock()
	if q.done {
		q.mu.Unlock()
		return
	}
	if _, dup := q.tracked[hash]; dup {
		q.mu.Unlock()
		return
	}
	q.tracked[hash] = &trackedTx{tx: tx, enqueuedAtBlock: q.blocksHandled.Load()}
	q.byTxID[hash.Hex()] = hash
	q.totalEnq++
	q.mu.Unlock()

	select {
	case q.admitted <- tx:
	default:
		// Nothing downstream can report this, and blocking here would stall the
		// nonce gate for every sender. Dropping is the behaviour #379 covers.
		depGraphLogger.Warnf("admitted channel full, dropping tx %s", hash.Hex())
		q.drop(hash)
	}
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

// submitToManager endorses for a read-write set and sends a one-transaction
// batch. A failure here drops the transaction: Enqueue has no error return, so
// there is nowhere to report it (#379).
func (q *DepGraphQueue) submitToManager(tx *types.Transaction) {
	hash := tx.Hash()

	q.mu.RLock()
	endorser := q.endorser
	q.mu.RUnlock()
	if endorser == nil {
		depGraphLogger.Errorf("Bind was never called, dropping tx %s", hash.Hex())
		q.drop(hash)
		return
	}

	end, err := endorser.ExecuteTransaction(q.ctx, tx)
	if err != nil {
		depGraphLogger.Errorf("endorse tx %s: %v", hash.Hex(), err)
		q.drop(hash)
		return
	}
	content, err := txContent(end)
	if err != nil {
		depGraphLogger.Errorf("read-write set for tx %s: %v", hash.Hex(), err)
		q.drop(hash)
		return
	}

	// IDs must start at 1 and rise by one: the constructor spins on
	// CompareAndSwap(id-1, id) against a zero-valued atomic, so ID 0 never
	// lands. Numbering and sending are one step, or endorse workers racing here
	// deliver them out of order and the constructor deadlocks.
	q.sendMu.Lock()
	batch := &dependencygraph.TransactionBatch{
		ID: q.batchID.Add(1),
		Txs: []*servicepb.TxWithRef{{
			Ref:     &committerpb.TxRef{TxId: hash.Hex()},
			Content: content,
		}},
	}
	select {
	case q.incoming <- batch:
		q.sendMu.Unlock()
	case <-q.ctx.Done():
		q.sendMu.Unlock()
		q.drop(hash)
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
		hash, known := q.byTxID[node.VerifierTx.GetRef().GetTxId()]
		if !known {
			continue
		}
		t, tracked := q.tracked[hash]
		if !tracked {
			continue
		}
		t.node = node
		// Approximate, and the only signal available: a transaction released in
		// the same block window it arrived in cannot have waited on anything.
		if q.blocksHandled.Load() > t.enqueuedAtBlock {
			q.conflictEnq++
		}
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
	q.blocksHandled.Add(1)
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
		delete(q.byTxID, hash.Hex())
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
	delete(q.byTxID, hash.Hex())
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

// Stats returns committed, invalid, enqueued and conflicting counts.
func (q *DepGraphQueue) Stats() (int, int, int, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.total, q.invalid, q.totalEnq, q.conflictEnq
}

// Dropped is how many transactions never reached the manager, because the
// endorsement failed or the admitted channel was full. Enqueue cannot report
// either (#379), so this is the only place they are visible.
func (q *DepGraphQueue) Dropped() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.dropped
}
