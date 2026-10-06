/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package metrics provides Prometheus instrumentation for the EVM gateway pipeline.
//
// # Pipeline steps
//
// Transactions flow through numbered steps; metrics at each step capture how long
// the step took and, where a channel or list is involved, how deep that queue is.
//
//	STEP  1  SendTransaction  – tx arrives at the gateway
//	STEP  2  nonceGate.Admit – tx enters nonce sequencing
//	STEP  3  nonceGate.park  – tx is parked waiting for an earlier nonce
//	STEP  4  queue.Enqueue   – tx enters the tx-queue (all three implementations)
//	STEP  5  DepGraphQueue admitted channel – tx queued for first endorsement
//	STEP  6  endorseLoop dequeues from admitted
//	STEP  7  endorsed channel – tx has been endorsed, queued for batching
//	STEP  8  incoming channel – batch submitted to the dependency manager
//	STEP  9  outgoing channel – tx released by the dependency manager as clash-free
//	STEP 10  worker Dequeue   – tx dequeued from the ready list
//	STEP 11  ExecuteEthTx     – EVM execution / first endorsement
//	STEP 12  endorser.Execute – individual endorser RPC call
//	STEP 13  SubmitFabricTx   – endorsement handed to the batch submitter
//	STEP 14  BatchSubmitter   – tx dequeued from the input channel for ordering
//	STEP 15  HandleBatch      – block committed; per-tx outcome recorded
//	STEP 16  handler.Handle   – per-handler dispatch latency
//	STEP 17  HandleBatch done – block fully processed
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// txTimestamps holds per-tx timestamps used to compute latency breakdowns.
// Fields are written once and read once; the sync.Map gives the required
// happens-before across goroutines.
type txTimestamps struct {
	step1  time.Time // STEP  1: SendTransaction received
	step14 time.Time // STEP 14: orderer Submit call started (tx handed to fabric)
	step15 time.Time // STEP 15: block notification received (ordering complete)
}

// Metrics holds all Prometheus instruments for the gateway pipeline.
// Obtain the process-wide instance via Default(); use New() in tests.
type Metrics struct {
	registry *prometheus.Registry

	// ── Counters ──────────────────────────────────────────────────────────────

	// TxReceived counts transactions that passed validation in SendTransaction.
	TxReceived prometheus.Counter
	// TxParked counts transactions parked by the nonce gate (future-nonce).
	TxParked prometheus.Counter
	// TxCommitted counts transactions that committed successfully (EVM status = 1).
	TxCommitted prometheus.Counter
	// TxMVCCConflict counts transactions invalidated by an MVCC conflict.
	TxMVCCConflict prometheus.Counter
	// TxInvalidSignature counts transactions invalidated due to a bad endorsement signature.
	TxInvalidSignature prometheus.Counter
	// TxOtherFailure counts transactions that were invalid for any other reason.
	TxOtherFailure prometheus.Counter
	// EndorseErrors counts endorsement call errors (transport / delivery failures).
	EndorseErrors prometheus.Counter

	// ── Histograms ────────────────────────────────────────────────────────────

	// StepLatency measures the duration of each pipeline step in seconds.
	// Labels: step (e.g. "nonce_admit", "endorse", "submit_fabric", "batch_submit",
	// "e2e", "handler").
	StepLatency *prometheus.HistogramVec

	// ── Gauges ────────────────────────────────────────────────────────────────

	// QueueDepth reports the instantaneous depth of each internal queue/channel.
	// Labels: queue (e.g. "nonce_parked", "tx_ready", "tx_waiting",
	// "depgraph_admitted", "depgraph_endorsed", "depgraph_incoming",
	// "depgraph_outgoing", "batch_submitter_input").
	QueueDepth *prometheus.GaugeVec

	// ── Per-tx tracking ───────────────────────────────────────────────────────

	// txTimes maps Ethereum tx hash → step-1 timestamp for end-to-end latency.
	txTimes sync.Map // map[common.Hash]txTimestamps
}

// stepBuckets are the histogram bucket boundaries for step latencies.
// Covers 0.1 ms → ~52 s in exponential steps (good for both fast local calls
// and slow remote endorsements or ordering rounds).
var stepBuckets = prometheus.ExponentialBuckets(0.0001, 2, 20)

// New creates a fresh Metrics instance with its own Prometheus registry.
// Use this in tests. Use Default() in production code.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,

		TxReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_received_total",
			Help: "Total transactions received by SendTransaction (post-validation).",
		}),
		TxParked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_parked_total",
			Help: "Total transactions parked by the nonce gate awaiting an earlier nonce.",
		}),
		TxCommitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_committed_total",
			Help: "Total transactions committed successfully (EVM status=1).",
		}),
		TxMVCCConflict: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_mvcc_conflict_total",
			Help: "Total transactions invalidated by an MVCC read-write conflict.",
		}),
		TxInvalidSignature: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_invalid_signature_total",
			Help: "Total transactions invalidated due to a bad endorsement signature.",
		}),
		TxOtherFailure: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_tx_other_failure_total",
			Help: "Total transactions that were invalid for any other reason.",
		}),
		EndorseErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evm_gateway_endorse_errors_total",
			Help: "Total endorsement transport/delivery errors.",
		}),

		StepLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "evm_gateway_step_latency_seconds",
			Help:    "Duration of each gateway pipeline step in seconds.",
			Buckets: stepBuckets,
		}, []string{"step"}),

		QueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "evm_gateway_queue_depth",
			Help: "Instantaneous depth of an internal queue or channel.",
		}, []string{"queue"}),
	}

	reg.MustRegister(
		m.TxReceived,
		m.TxParked,
		m.TxCommitted,
		m.TxMVCCConflict,
		m.TxInvalidSignature,
		m.TxOtherFailure,
		m.EndorseErrors,
		m.StepLatency,
		m.QueueDepth,
	)
	return m
}

// ── Step label constants ──────────────────────────────────────────────────────

const (
	StepNonceAdmit          = "nonce_admit"           // STEP 2: time from SendTransaction to Admit
	StepNoncePark           = "nonce_park"            // STEP 3: parking overhead
	StepAdmittedQueue       = "admitted_queue"        // STEP 5→6: time in the admitted channel
	StepEndorse             = "endorse"               // STEP 6→7: endorsement round-trip
	StepEndorsedQueue       = "endorsed_queue"        // STEP 7→8: time in endorsed / incoming channels
	StepDepgraphPreSubmit   = "depgraph_pre_submit"   // STEP 5→8: enqueue to dep-manager submission (endorse + batch leg)
	StepDepMgrRelease       = "dep_mgr_release"       // STEP 8→9: time inside the dep-graph manager
	StepDepgraphPostRelease = "depgraph_post_release" // STEP 9→10: dep-manager release to Dequeue (ready-list wait)
	StepReadyQueue          = "ready_queue"           // STEP 9→10: time in the ready list (alias for TxQueueV2)
	StepExecute             = "execute"               // STEP 11: EVM execution (all endorsers)
	StepEndorseRPC          = "endorse_rpc"           // STEP 12: single endorser RPC
	StepSubmitFabric        = "submit_fabric"         // STEP 13: hand-off to batch submitter
	StepBatchSubmitQueue    = "batch_submit_queue"    // STEP 13→14: wait in batch submitter input channel
	StepBatchSubmit         = "batch_submit"          // STEP 14: orderer submit call

	// Three coarse per-tx latency buckets derived from the stored timestamps:
	StepTxPreSubmit = "tx_pre_submit" // STEP 1→14: gateway processing until orderer hand-off
	StepTxOrdering  = "tx_ordering"   // STEP 14→15: time the tx spent inside the orderer
	StepTxPostOrder = "tx_post_order" // STEP 15→17: post-commit handler dispatch time

	StepE2E     = "e2e"     // STEP 1→15: full end-to-end
	StepHandler = "handler" // STEP 15→16: per-handler dispatch
)

// ── Queue label constants ─────────────────────────────────────────────────────

const (
	QueueNonceParked         = "nonce_parked"          // transactions parked in the nonce gate
	QueueTxReady             = "tx_ready"              // DepGraphQueue / TxQueueV2 ready list
	QueueTxWaiting           = "tx_waiting"            // TxQueueV2 waiting list
	QueueDepgraphAdmitted    = "depgraph_admitted"     // DepGraphQueue admitted channel
	QueueDepgraphEndorsed    = "depgraph_endorsed"     // DepGraphQueue endorsed channel
	QueueDepgraphIncoming    = "depgraph_incoming"     // DepGraphQueue incoming (→ dep manager)
	QueueDepgraphOutgoing    = "depgraph_outgoing"     // DepGraphQueue outgoing (← dep manager)
	QueueBatchSubmitterInput = "batch_submitter_input" // BatchSubmitter input channel
)

// ── Per-tx helpers ────────────────────────────────────────────────────────────

// TrackTxStart records the ingress timestamp for hash. Call at STEP 1.
func (m *Metrics) TrackTxStart(hash common.Hash) {
	m.txTimes.Store(hash, txTimestamps{step1: time.Now()})
}

// RecordTxSubmitted stamps the moment the orderer Submit call starts for hash.
// Call at STEP 14, just before invoking the orderer RPC. Records the
// STEP 1→14 (pre-submit) latency as a side-effect.
func (m *Metrics) RecordTxSubmitted(hash common.Hash) {
	now := time.Now()
	v, ok := m.txTimes.Load(hash)
	if !ok {
		return
	}
	ts := v.(txTimestamps)
	ts.step14 = now
	m.txTimes.Store(hash, ts)
	m.StepLatency.WithLabelValues(StepTxPreSubmit).Observe(now.Sub(ts.step1).Seconds())
}

// RecordTxNotified stamps the moment the block notification arrives for hash
// and records the STEP 14→15 (ordering) latency. Call at STEP 15.
func (m *Metrics) RecordTxNotified(hash common.Hash) {
	now := time.Now()
	v, ok := m.txTimes.Load(hash)
	if !ok {
		return
	}
	ts := v.(txTimestamps)
	ts.step15 = now
	m.txTimes.Store(hash, ts)
	if !ts.step14.IsZero() {
		m.StepLatency.WithLabelValues(StepTxOrdering).Observe(now.Sub(ts.step14).Seconds())
	}
}

// ObserveTxComplete reads and deletes the stored timestamps for hash, then
// records the end-to-end and post-order latencies. Call at STEP 17.
func (m *Metrics) ObserveTxComplete(hash common.Hash) {
	now := time.Now()
	v, ok := m.txTimes.LoadAndDelete(hash)
	if !ok {
		return
	}
	ts := v.(txTimestamps)
	if !ts.step1.IsZero() {
		m.StepLatency.WithLabelValues(StepE2E).Observe(now.Sub(ts.step1).Seconds())
	}
	if !ts.step15.IsZero() {
		m.StepLatency.WithLabelValues(StepTxPostOrder).Observe(now.Sub(ts.step15).Seconds())
	}
}

// ── Convenience wrappers ──────────────────────────────────────────────────────

// ObserveStep records a duration for the named step.
func (m *Metrics) ObserveStep(step string, d time.Duration) {
	m.StepLatency.WithLabelValues(step).Observe(d.Seconds())
}

// SetQueue sets the depth gauge for the named queue.
func (m *Metrics) SetQueue(queue string, depth int) {
	m.QueueDepth.WithLabelValues(queue).Set(float64(depth))
}

// ── HTTP server ───────────────────────────────────────────────────────────────

// Handler returns an http.Handler that serves the Prometheus metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ── Process-wide default ──────────────────────────────────────────────────────

var (
	defaultOnce    sync.Once
	defaultMetrics *Metrics
)

// Default returns the process-wide Metrics instance, creating it on first call.
// All production code should use this rather than New().
func Default() *Metrics {
	defaultOnce.Do(func() { defaultMetrics = New() })
	return defaultMetrics
}

// ReplaceDefault swaps the process-wide default. Intended for use in tests only.
func ReplaceDefault(m *Metrics) {
	defaultOnce.Do(func() {}) // ensure the once is "used" so it never fires again
	defaultMetrics = m
}

// ── Timestamp helpers for multi-step tracking ────────────────────────────────

// Now is an alias for time.Now, exposed so tests can override it via monkey-patching
// if needed. Production code should call metrics.Now() instead of time.Now() at
// instrument points so the coupling is explicit.
func Now() time.Time { return time.Now() }

// Since returns the elapsed time since t using the same clock as Now().
func Since(t time.Time) time.Duration { return time.Since(t) }
