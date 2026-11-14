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
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/gateway/metrics"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
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
	// defaultMaxMVCCRetries is how many extra attempts a transaction gets after
	// an MVCC conflict. Small on purpose: the retries must finish before the
	// nonce gate's reaper drops the sender's later nonces, which stay parked
	// behind it the whole time (see config.DepGraphQueue.MaxMVCCRetries).
	defaultMaxMVCCRetries = 3
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

// txEndorser produces the read-write set the manager schedules on.
//
// ExecuteTransaction returns an endorsement or fails outright, with no partial
// result and one endorser's failure failing the whole call. "Endorsement" is
// wider than "success": a success, an EVM revert and a failed EVM execution all
// come back as endorsements and all reach a block, the last two with a status=0
// receipt.
//
// A failure drops the transaction (see submitToManager), because the error value
// does not say which kind it was -- all are flat fmt.Errorf strings. The
// categories and what each should do instead are in
// docs/TODO_ENDORSEMENT_ERROR_MODEL.md.
type txEndorser interface {
	ExecuteTransaction(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error)
}

// admittedTx pairs a transaction with the timestamp from when it entered the
// pipeline (STEP 5), so the timestamp can be propagated lock-free through the
// endorsed channel all the way to batchLoop.
//
// retry marks a transaction adopted after an MVCC conflict, which enters here
// rather than on the ready list because the queue has no node for it. Its per-tx
// metrics timestamps restart on the endorse worker, not at admit time: the
// conflicting block only reaches ObserveTxComplete — which deletes them — once
// every handler has returned, which is after Handle admitted it.
type admittedTx struct {
	tx         *types.Transaction
	enqueuedAt time.Time
	retry      bool
}

// endorsedTx carries a transaction's proto payload together with the timestamp
// from when it was first admitted (STEP 5), so batchLoop can measure the full
// endorse+batch leg (STEP 5→8) without touching q.tracked.
type endorsedTx struct {
	proto      *servicepb.TxWithRef
	enqueuedAt time.Time
}

// trackedTx is one transaction between Enqueue and Complete.
//
// node is nil until the manager releases it. Feeding a node back is what
// unblocks its dependents, so it is kept for the whole lifetime rather than
// dropped at Dequeue, and cleared once fed back so it cannot be sent twice.
type trackedTx struct {
	tx      *types.Transaction
	node    *dependencygraph.TransactionNode
	readyAt time.Time // STEP 9: when the dep manager released it as clash-free

	// end is the endorsement submitToManager made, kept while endorsed so that
	// processTx need not execute the transaction again; see ReusableEndorsement.
	// readSeq is the commitSeq it was executed at.
	end      sdk.Endorsement
	endorsed bool
	readSeq  uint64
	reads    []string
	writes   []string

	// retries counts the MVCC conflicts this transaction has been re-executed
	// for. It survives because the tracked entry does: a retried transaction is
	// never untracked, which also keeps IsPending answering across attempts.
	retries int

	// restartMetrics asks Dequeue to start a fresh per-tx metrics window: the
	// conflicted attempt already went through ObserveTxComplete, which deletes
	// the timestamps, so without this a retry records no latency at all.
	restartMetrics bool
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
	admitted  chan admittedTx
	endorsed  chan endorsedTx

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
	maxMVCCRetries int

	mu      sync.RWMutex
	cond    *sync.Cond
	ready   []*types.Transaction
	tracked map[common.Hash]*trackedTx
	done    bool

	// commitSeq counts the blocks Handle has seen. lastWrite maps each key a
	// tracked transaction wrote in the last reuseWindow blocks to the commitSeq
	// of that write; recent lists each of those blocks' keys, so a block's
	// entries can be dropped once it falls out of the window. commitSeq is
	// written under mu but atomic, so submitToManager can read it without
	// contending for mu.
	commitSeq atomic.Uint64
	lastWrite map[string]uint64
	recent    [reuseWindow][]string

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
	maxMVCCRetries := positiveOrDefault("MaxMVCCRetries", cfg.MaxMVCCRetries, defaultMaxMVCCRetries)

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
		admitted:       make(chan admittedTx, chanSize),
		endorsed:       make(chan endorsedTx, chanSize),
		tracked:        make(map[common.Hash]*trackedTx),
		lastWrite:      make(map[string]uint64),
		endorseWorkers: endorseWorkers,
		batchThreshold: batchThreshold,
		batchTimeout:   batchTimeout,
		maxMVCCRetries: maxMVCCRetries,
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
		// No sampling interval since committer v1.0.6: the manager reports its
		// queue sizes on scrape rather than from a ticker of its own, so the
		// QueueMonitorSamplingTime this used to need is gone.
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
func (q *DepGraphQueue) Enqueue(tx *types.Transaction) error {
	hash := tx.Hash()
	depGraphLogger.Debugf("DepGraphQueue.Enqueue() called with tx %s", hash.Hex())

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.done {
		depGraphLogger.Warnf("DepGraphQueue.Enqueue() tx %s rejected: queue is closed", hash.Hex())
		return domain.ErrQueueClosed
	}
	if _, dup := q.tracked[hash]; dup {
		depGraphLogger.Warnf("DepGraphQueue.Enqueue() tx %s rejected: already tracked", hash.Hex())
		return domain.ErrTransactionAlreadyPending
	}

	// STEP 5: tx lands in the admitted channel – stamp the ingress time on the
	// admittedTx so it flows lock-free to batchLoop without ever re-reading q.tracked.
	if !q.admit(tx, &trackedTx{tx: tx}, false) {
		// Blocking is not an option: the nonce gate holds its own lock while
		// calling us. The caller reports it, and the client can retry.
		msg := fmt.Sprintf("DepGraphQueue.Enqueue() tx %s rejected: admitted channel full", hash.Hex())
		depGraphLogger.Error(msg)
		fc.DebugPanic(msg)
		return domain.ErrQueueFull
	}
	return nil
}

// admit starts a transaction down the pipeline: tracked under t, then handed to
// the endorse workers — the same two steps whether the queue is seeing it for the
// first time (Enqueue) or re-admitting one whose entry was lost (adopt). retry
// asks the worker to restart the per-tx metrics window that a previous attempt's
// ObserveTxComplete consumed.
//
// It never blocks, and reports whether the channel had room: neither caller can
// wait on a channel it is not draining, Enqueue holding the nonce gate's lock
// and adopt running on the block-handler goroutine. Both surface a refusal, but
// only Enqueue can tell the client (domain.ErrQueueFull); adopt's transaction
// has no caller left, so it can only log and debug-panic.
//
// Tracking before the hand-off is safe only because the caller holds q.mu: an
// endorse worker can pick the transaction up the moment it is sent, and
// keepEndorsement takes q.mu to store what it produces, discarding it silently
// for a transaction the queue does not track. The lock keeps the worker out
// until the entry is there.
//
// A failed send removes the entry again, because nothing reaps q.tracked: an
// entry never handed over would answer IsPending for the life of the process and
// count against InFlight, with nothing ever going to execute it.
//
// Caller must hold q.mu.
func (q *DepGraphQueue) admit(tx *types.Transaction, t *trackedTx, retry bool) bool {
	hash := tx.Hash()
	q.tracked[hash] = t

	metrics.Default().SetQueue(metrics.QueueDepgraphAdmitted, len(q.admitted))
	select {
	case q.admitted <- admittedTx{tx: tx, enqueuedAt: time.Now(), retry: retry}:
		return true
	default:
		delete(q.tracked, hash)
		return false
	}
}

// endorseLoop endorses admitted transactions and hands them to the manager.
func (q *DepGraphQueue) endorseLoop() {
	for {
		select {
		case <-q.ctx.Done():
			return
		case atx, ok := <-q.admitted:
			// STEP 6: tx dequeued from admitted – report remaining depth.
			if !ok {
				return
			}
			metrics.Default().SetQueue(metrics.QueueDepgraphAdmitted, len(q.admitted))
			if atx.retry {
				// Restart the per-tx latency window: the attempt that conflicted
				// has already been through ObserveTxComplete, which consumed the
				// timestamps this transaction started with.
				metrics.Default().TrackTxStart(atx.tx.Hash())
			}
			q.submitToManager(atx)
		}
	}
}

// submitToManager endorses for a read-write set and hands the result to the
// batcher. A failure here drops the transaction: Enqueue has already returned,
// so, like a failed processTx, it is logged and the client can resubmit.
func (q *DepGraphQueue) submitToManager(atx admittedTx) {
	hash := atx.tx.Hash()
	depGraphLogger.Debugf("DepGraphQueue.submitToManager() endorsing tx %s", hash.Hex())

	seq := q.commitSeq.Load()

	// STEP 6→7 endorsement: measure how long the endorser call takes.
	t6 := time.Now()
	end, err := q.endorser.ExecuteTransaction(q.ctx, atx.tx)
	metrics.Default().ObserveStep(metrics.StepEndorse, time.Since(t6))
	if err != nil {
		metrics.Default().EndorseErrors.Inc()
		depGraphLogger.Errorf("endorse tx %s: %v", hash.Hex(), err)
		q.drop(hash)
		return
	}

	// The endorser and this queue disagree about the wire format; retrying cannot fix it.
	content, err := txContent(end)
	if err != nil {
		msg := fmt.Sprintf("DepGraphQueue.submitToManager() read-write set for tx %s: %v", hash.Hex(), err)
		depGraphLogger.Error(msg)
		q.drop(hash)
		fc.DebugPanic(msg)
		return
	}
	q.keepEndorsement(hash, seq, end, content)

	// STEP 7: tx endorsed – carry enqueuedAt through to batchLoop via the
	// endorsed channel; no lock needed, the timestamp came from our own struct.
	metrics.Default().SetQueue(metrics.QueueDepgraphEndorsed, len(q.endorsed))
	select {
	case q.endorsed <- endorsedTx{
		proto: &servicepb.TxWithRef{
			Ref:     &committerpb.TxRef{TxId: txRefID(hash)},
			Content: content,
		},
		enqueuedAt: atx.enqueuedAt,
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

	pending := make([]endorsedTx, 0, q.batchThreshold)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		// Ids start at 1: the constructor spins on CompareAndSwap(id-1, id)
		// against a zero-valued atomic, so a batch numbered 0 never lands.
		q.batchID++
		protos := make([]*servicepb.TxWithRef, len(pending))
		now8 := time.Now()
		for i, etx := range pending {
			protos[i] = etx.proto
			// STEP 8: record the endorse+batch leg duration (STEP 5→8) using the
			// timestamp carried directly on the struct — no lock needed.
			if !etx.enqueuedAt.IsZero() {
				metrics.Default().ObserveStep(metrics.StepDepgraphPreSubmit, now8.Sub(etx.enqueuedAt))
			}
		}
		batch := &dependencygraph.TransactionBatch{ID: q.batchID, Txs: protos}
		depGraphLogger.Debugf("DepGraphQueue.batchLoop() sending batch id=%d with %d txs to dependency manager", batch.ID, len(batch.Txs))
		metrics.Default().SetQueue(metrics.QueueDepgraphIncoming, len(q.incoming))
		select {
		case q.incoming <- batch:
			depGraphLogger.Debugf("DepGraphQueue.batchLoop() batch id=%d sent to dependency manager", batch.ID)
		case <-q.ctx.Done():
			depGraphLogger.Warnf("failed to send batch id=%d to dependency manager: context done: %v", batch.ID, q.ctx.Err())
		}
		pending = pending[:0]
	}

	for {
		select {
		case <-q.ctx.Done():
			return
		case etx := <-q.endorsed:
			pending = append(pending, etx)
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
			// STEP 9: tx released by the dep manager as clash-free – report the
			// remaining depth of the outgoing channel.
			if !ok {
				return
			}
			metrics.Default().SetQueue(metrics.QueueDepgraphOutgoing, len(q.outgoing))
			depGraphLogger.Debugf("DepGraphQueue.drainReleased() received %d clash-free nodes from dependency manager", len(nodes))
			q.markReady(nodes)
		}
	}
}

func (q *DepGraphQueue) markReady(nodes dependencygraph.TxNodeBatch) {
	var orphans dependencygraph.TxNodeBatch
	var unexpected []string

	q.mu.Lock()
	readyAt := time.Now()
	for _, node := range nodes {
		id := node.VerifierTx.GetRef().GetTxId()
		hash, ok := hashFromTxRefID(id)
		if !ok {
			// Only this queue sets TxId, so the manager and the queue disagree about the reference.
			msg := fmt.Sprintf("DepGraphQueue.markReady() unrecognised tx ref %q received from dependency manager", id)
			depGraphLogger.Error(msg)
			unexpected = append(unexpected, msg)
			orphans = append(orphans, node)
			continue
		}
		t, tracked := q.tracked[hash]
		if !tracked {
			depGraphLogger.Warnf("DepGraphQueue.markReady() tx %s node received from dependency manager but not tracked", hash.Hex())
			orphans = append(orphans, node)
			continue
		}
		t.node = node
		t.readyAt = readyAt // STEP 9: stamp when the dep manager released this tx
		q.ready = append(q.ready, t.tx)
		depGraphLogger.Debugf("DepGraphQueue.markReady() tx %s marked ready", hash.Hex())
	}
	q.cond.Broadcast()
	q.mu.Unlock()

	// Nothing will run these, so hand them back now or their dependents wait forever.
	q.release(orphans)
	for _, msg := range unexpected {
		fc.DebugPanic(msg)
	}
}

// Dequeue blocks until the manager releases a transaction, or the queue closes.
//
// Taking the transaction off the ready list here is what keeps that list free of
// duplicates, so nothing has to guard against them. Only processTx submits, and
// it runs after Dequeue, so one still on the list has not been submitted — and
// under the single-gateway assumption a block only carries transactions this
// gateway submitted. So no commit or conflict can arrive for a transaction
// waiting on the list, and markReady and reschedule cannot find one present.
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

	// STEP 10 (DepGraphQueue): measure how long the tx waited in the ready list
	// after the dep manager released it (STEP 9→10), and report ready-list depth.
	if t, ok := q.tracked[tx.Hash()]; ok {
		if !t.readyAt.IsZero() {
			metrics.Default().ObserveStep(metrics.StepDepgraphPostRelease, time.Since(t.readyAt))
		}
		if t.restartMetrics {
			t.restartMetrics = false
			metrics.Default().TrackTxStart(tx.Hash())
		}
	}
	metrics.Default().SetQueue(metrics.QueueTxReady, len(q.ready))

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
	nodes := q.untrack(hash)
	q.mu.Unlock()
	q.release(nodes)
}

// untrack forgets one transaction and returns the node it still owes the
// manager, which is what releases whatever queued behind it. Clearing the node
// is what makes a second call a no-op.
//
// An untracked hash is normal on both paths, so do not make this an error or a
// debug-panic. Complete is documented to accept one, and reaches it when a
// submission fails for a transaction already committed or completed. A committed
// block can carry one whose entry is already gone, or one this queue never
// submitted at all: every block in the namespace comes through untrackOrRetry,
// including those the hardhat state RPCs produce (setBalance, setCode,
// setStorageAt), which write state directly instead of calling Enqueue -- which
// alone makes this branch routine in the gateway/app tests.
//
// Caller must hold q.mu.
func (q *DepGraphQueue) untrack(hash common.Hash) dependencygraph.TxNodeBatch {
	t, ok := q.tracked[hash]
	if !ok {
		depGraphLogger.Debugf("DepGraphQueue.untrack() tx %s is not tracked", hash.Hex())
		return nil
	}
	delete(q.tracked, hash)
	if t.node == nil {
		return nil
	}
	node := t.node
	t.node = nil
	return dependencygraph.TxNodeBatch{node}
}

// Handle tells the manager which transactions committed. Rejected ones go back
// too: either way they are no longer in flight, and holding one back strands
// everything behind it. The whole block goes in one pass, under one lock.
//
// An MVCC-rejected transaction is the one exception: another writer reached one
// of its keys first, so instead of being untracked it is executed again, up to
// maxMVCCRetries times. Its node still goes back to the manager, since the
// attempt that conflicted is no longer in flight and whatever queued behind it
// must be let go.
func (q *DepGraphQueue) Handle(_ context.Context, block *domain.Block) error {
	depGraphLogger.Debugf("DepGraphQueue.Handle() called with block containing %d txs", len(block.Transactions))
	if len(block.Transactions) == 0 {
		return nil
	}

	outcomes := make([]txOutcome, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		// Counts attempts, not transactions: a retried one is counted again when
		// its next attempt commits. Stats is a benchmarking aid (see its TODO),
		// and an attempt is the thing the committer actually saw.
		q.total++
		if tx.Status == 0 {
			q.invalid++
		}
		outcomes = append(outcomes, txOutcome{
			hash: common.BytesToHash(tx.TxHash),
			// Only an MVCC conflict is worth another attempt. A bad signature, a
			// malformed envelope or a duplicate id would fail the same way again.
			conflicted: tx.FabricTxStatus == blocks.StatusMVCCConflict,
			tx:         tx.ToEthTx(),
			from:       common.BytesToAddress(tx.FromAddress),
		})
	}

	// Each error is a state that should not arise: a conflict this queue can no
	// longer do anything useful about. Panicking here rather than inside keeps
	// q.mu out of it, the way markReady does.
	for _, err := range q.untrackOrRetry(block.Transactions, outcomes, block.BlockNumber) {
		fc.DebugPanic(err.Error())
	}
	return nil
}

// txOutcome is one transaction's fate in a committed block, as Handle reads it
// off the block before taking the lock. tx is the decoded Ethereum transaction,
// needed to retry one this queue has no record of, and is nil if the raw bytes
// would not decode. from comes off the block rather than a signer recovery, so
// giveUp can report it even then.
type txOutcome struct {
	hash       common.Hash
	conflicted bool
	tx         *types.Transaction
	from       common.Address
}

// untrackOrRetry resolves every transaction in a committed block: each is either
// finished — untracked, with its node handed back so its dependents go — or put
// up for another attempt. It owns both, the lock they need, and the two ways a
// retry is scheduled, so Handle has nothing left to decide.
//
// committed is the block as it arrived, which recordCommitted needs: it reads the
// write sets of the entries this pass is about to forget, so it runs first and
// under the same lock.
//
// It returns one error per state that should not have arisen, for the caller to
// debug-panic on once it is off the lock. Takes q.mu itself.
func (q *DepGraphQueue) untrackOrRetry(
	committed []domain.Transaction, outcomes []txOutcome, blockNum uint64,
) []error {
	var nodes dependencygraph.TxNodeBatch
	var errs []error

	q.mu.Lock()
	q.recordCommitted(committed)
	for _, o := range outcomes {
		// Each case answers one question: is this transaction still ours after
		// this block? A transaction awaiting another attempt is, and keeps its
		// node so its dependents stay held. Anything else is untracked, handing
		// the node back to free whatever queued behind it. err is non-nil only
		// for a state that should not have arisen.
		var pending bool
		var err error
		switch {
		case !o.conflicted:
			// Finished: committed, or failed in a way another attempt cannot fix.
		case q.done:
			// Shutting down; starting an attempt nothing will finish is pointless.
		case q.tracked[o.hash] == nil:
			// Conflicted with no entry behind it (a nil value is a missing key),
			// so there is nothing to retry from and adopt builds one.
			pending, err = q.adopt(o)
		default:
			// Conflicted and tracked: another attempt if the budget allows.
			pending = q.retry(o.hash)
			if !pending {
				err = q.giveUp(o, blockNum)
			}
		}

		if err != nil {
			errs = append(errs, err)
		}
		if !pending {
			nodes = append(nodes, q.untrack(o.hash)...)
		}
	}
	q.mu.Unlock()

	depGraphLogger.Debugf("DepGraphQueue.untrackOrRetry() releasing %d nodes (block txs=%d)", len(nodes), len(outcomes))
	q.release(nodes)
	return errs
}

// retry puts a tracked transaction that conflicted back up for another attempt,
// reporting whether it had the budget for one. Only a conflict reaches it, and
// only while the queue is running.
//
// It goes straight back on the ready list and keeps the node it already has.
// Both halves matter:
//
//   - The node is what holds the transaction's dependents. Feeding it back would
//     run freeDependents and release everything queued behind this key, but the
//     transaction has not written yet — that is what the conflict means — so a
//     dependent endorsed now would read state missing this write and conflict in
//     turn. The node goes back only at a final outcome, via untrack.
//
//   - Not re-entering the manager is what makes that safe. A transaction sent
//     back through the admitted channel becomes a second node, while the
//     detector still holds the first node's keys (dropped only when it is fed
//     back), so the new node would wait on the held one forever.
//
// This is sound only while the second execution touches the first's keys, which
// are what the graph is still protecting — the assumption ReusableEndorsement
// documents: a key only a re-execution touches is neither protected nor recorded.
//
// No duplicate check is needed when appending: the conflicted attempt left the
// ready list when Dequeue served it (see Dequeue). It does not untrack either,
// so untrackOrRetry stays the only caller of untrack. Caller must hold q.mu.
func (q *DepGraphQueue) retry(hash common.Hash) bool {
	t := q.tracked[hash]
	if t.retries >= q.maxMVCCRetries {
		return false
	}
	t.retries++
	// Clearing the endorsement is what makes the worker execute again: with
	// nothing to reuse, processTx falls through to ExecuteEthTx. The stored one
	// read state that has since been written, so it must not be submitted.
	t.end, t.endorsed = sdk.Endorsement{}, false
	// Restart the ready-list clock and ask Dequeue to restart the per-tx metrics
	// window, which the finished attempt's ObserveTxComplete has consumed.
	t.readyAt = time.Now()
	t.restartMetrics = true
	q.ready = append(q.ready, t.tx)
	q.cond.Broadcast()
	metrics.Default().SetQueue(metrics.QueueTxReady, len(q.ready))
	metrics.Default().TxMVCCRetried.Inc()
	depGraphLogger.Warnf(
		"[MVCC-RETRY] tx %s conflicted and is being executed again (attempt %d of %d); "+
			"it keeps its graph node, so its dependents stay held",
		hash.Hex(), t.retries+1, q.maxMVCCRetries+1)
	return true
}

// giveUp reports that a conflicted transaction gets no further attempt,
// returning the error the caller should debug-panic on. The caller untracks it,
// which releases its node.
//
// This should be unreachable at any budget: the conflict is the problem, not the
// number of attempts spent on it, and neither cause below is fixed by retrying.
// A budget of zero makes us blind to them rather than immune, so even that lands
// here as a diagnosis rather than a configured outcome.
//
// Either a writer the graph cannot see is reaching these keys — a second gateway
// on the namespace, or a test writing state directly, both breaking the
// single-gateway assumption — or the keys moved under us. The graph holds the
// *first* execution's keys and a retry re-executes, so a contract branching on
// state can touch a key the first execution did not, which is then neither
// protected nor recorded (the assumption ReusableEndorsement documents) and a
// transaction from this very queue can conflict with the retry. The message
// names both, since nothing here can tell which.
//
// Releasing the node makes things worse rather than containing them: the
// dependents go out to face whatever refused this transaction and may fail the
// same way. Holding it would strand them for good, so there is no good option
// here — only a loud one.
//
// Caller must hold q.mu.
func (q *DepGraphQueue) giveUp(o txOutcome, blockNum uint64) error {
	metrics.Default().TxMVCCRetriesExhausted.Inc()

	var nonce uint64
	if t := q.tracked[o.hash]; t != nil && t.tx != nil {
		nonce = t.tx.Nonce()
	}

	err := fmt.Errorf(
		"[MVCC-GIVEUP] tx %s dropped after %d write-conflict attempts: from=%s nonce=%d block=%d\n"+
			"  None of its writes were applied, so it has no receipt and eth_getTransactionByHash "+
			"now answers null for it.\n"+
			"  Its nonce was not consumed, so this sender's later transactions stay parked until "+
			"nonce %d is resubmitted, and are dropped %s after they were parked.\n"+
			"  Likely causes: (1) a writer outside this gateway is writing these keys — a second "+
			"gateway on the namespace, or a test writing state directly; (2) re-executing this "+
			"transaction touched keys its first execution did not, which the dependency graph does "+
			"not protect, so another transaction from this same gateway conflicted with it.\n"+
			"  Action: the client must resubmit nonce %d. Check that only this gateway submits to "+
			"the namespace, and whether this contract varies the keys it touches depending on state",
		o.hash.Hex(), q.maxMVCCRetries+1, o.from.Hex(), nonce, blockNum,
		nonce, defaultParkedTTL, nonce)
	depGraphLogger.Error(err)
	return err
}

// adopt takes on an MVCC-conflicted transaction this queue has no entry for,
// giving it one and submitting it for another attempt. Called only for a
// conflict: that is the one outcome a missing entry has to be rebuilt for.
//
// Under the single-gateway assumption this queue sees every transaction in the
// namespace, so an untracked one means the entry is gone rather than never
// there: the process restarted mid-flight, or Complete already took it after an
// ambiguous submit failure. (A test RPC's direct write also arrives this way.)
//
// A conflict in that state is what the retry exists for. The pre-crash attempt
// collided with an in-flight write the restarted queue could not know about, so
// it gets an entry and the full budget; dropping it would be the worst outcome
// available, since the nonce was not consumed and the sender's later
// transactions are stuck behind the gap.
//
// Unlike a tracked retry it has no node, so the manager has never heard of it
// and it goes back through the admitted channel rather than onto the ready list.
// It reports whether another attempt is pending, with the error the caller
// should debug-panic on — nil unless a transaction that should have been adopted
// could not be.
//
// Caller must hold q.mu.
func (q *DepGraphQueue) adopt(o txOutcome) (bool, error) {
	if o.tx == nil {
		// Without the raw bytes there is nothing to execute: the transaction
		// cannot be reconstructed from a hash alone.
		err := fmt.Errorf(
			"[MVCC-ORPHAN] tx %s conflicted, is not tracked, and its raw bytes would not decode, "+
				"so it cannot be executed again; the client must resubmit",
			o.hash.Hex())
		depGraphLogger.Error(err)
		return false, err
	}
	// retries starts at 1: the attempt that just conflicted was the first.
	if !q.admit(o.tx, &trackedTx{tx: o.tx, retries: 1}, true) {
		// Same condition Enqueue debug-panics on, but worse: Enqueue can at least
		// tell its caller, while this transaction has no caller left to resubmit
		// it. Blocking is no answer either, since this runs under q.mu, which the
		// endorse workers draining the channel need. So say so loudly.
		metrics.Default().ForgetTx(o.hash)
		err := fmt.Errorf(
			"[MVCC-ORPHAN] tx %s conflicted and is not tracked, but the admitted channel is full, "+
				"so it cannot be executed again; the client must resubmit",
			o.hash.Hex())
		depGraphLogger.Error(err)
		return false, err
	}
	metrics.Default().TxMVCCRetried.Inc()
	depGraphLogger.Warnf(
		"[MVCC-ADOPT] tx %s conflicted but was not tracked, most likely in flight across a restart; "+
			"adopting it with a fresh entry and executing it again (attempt 2 of %d)",
		o.hash.Hex(), q.maxMVCCRetries+1)
	return true, nil
}

// reuseWindow is how many blocks an endorsement can be reused across. One that
// waited longer is re-executed: it has most likely gone stale anyway, and the
// bound keeps lastWrite to the writes of the last reuseWindow blocks.
const reuseWindow = 64

// keepEndorsement records what tx read and wrote and keeps its endorsement for
// processTx. seq is the commitSeq it was executed at.
//
// This assumes the endorser's state already includes every block Handle has
// counted, which holds for the local endorser, whose store handles blocks before
// the gateway does. A lagging remote endorser could serve an older read; the
// second execution this replaces, made later, had a similar exposure.
func (q *DepGraphQueue) keepEndorsement(hash common.Hash, seq uint64, end sdk.Endorsement, content *applicationpb.Tx) {
	reads, writes := readWriteKeys(content)
	q.mu.Lock()
	defer q.mu.Unlock()
	if t, ok := q.tracked[hash]; ok {
		t.end, t.endorsed, t.readSeq, t.reads, t.writes = end, true, seq, reads, writes
	}
}

// recordCommitted counts one block and notes the keys its valid tracked
// transactions wrote, dropping those of the block that falls out of the reuse
// window. It must run before settle, which forgets their write sets. Caller
// must hold q.mu.
//
// Only transactions this queue still tracks are seen: a domain.Block carries no
// read-write sets, so a write by another gateway, or by a tx already completed
// after an ambiguous submit failure, goes unnoticed, and so does state the test
// RPCs change directly (hardhat_set*, evm_revert). A reused endorsement that
// read it then fails the committer's MVCC check instead of being re-executed.
func (q *DepGraphQueue) recordCommitted(txs []domain.Transaction) {
	seq := q.commitSeq.Add(1)
	slot := &q.recent[seq%reuseWindow]
	for _, k := range *slot {
		if q.lastWrite[k] == seq-reuseWindow {
			delete(q.lastWrite, k)
		}
	}
	clear(*slot)
	*slot = (*slot)[:0]

	for _, tx := range txs {
		if !tx.FabricTxStatus.Valid() {
			continue
		}
		t, ok := q.tracked[common.BytesToHash(tx.TxHash)]
		if !ok {
			continue
		}
		for _, k := range t.writes {
			q.lastWrite[k] = seq
			*slot = append(*slot, k)
		}
	}
}

// ReusableEndorsement hands over, once, the endorsement submitToManager made
// for hash, if nothing it read has been overwritten since. Call it after Dequeue
// serves hash: by then every transaction it depended on has finished, and none
// depending on it can commit first, so only commits seen so far can stale it.
//
// When it reports false, the caller executes again, as before this existed.
// The graph and recordCommitted still hold the first execution's read-write
// set, so this relies on a re-execution touching the same keys: a key only the
// re-execution touches is neither protected from new transactions nor recorded.
func (q *DepGraphQueue) ReusableEndorsement(hash common.Hash) (sdk.Endorsement, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	t, ok := q.tracked[hash]
	if !ok || !t.endorsed {
		return sdk.Endorsement{}, false
	}
	end := t.end
	t.end, t.endorsed = sdk.Endorsement{}, false
	if q.commitSeq.Load()-t.readSeq >= reuseWindow {
		return sdk.Endorsement{}, false
	}
	for _, k := range t.reads {
		if q.lastWrite[k] > t.readSeq {
			return sdk.Endorsement{}, false
		}
	}
	return end, true
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
	metrics.Default().ForgetTx(hash)
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
