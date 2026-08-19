/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// LoadgenMetrics holds all Prometheus metrics for the loadgen test
type LoadgenMetrics struct {
	// Transaction counters
	transactionSent      prometheus.Counter
	transactionCommitted prometheus.Counter
	transactionAborted   prometheus.Counter

	// Latency breakdown histograms
	totalLatency      prometheus.Histogram // T4 - T1: end-to-end latency
	queueLatency      prometheus.Histogram // T2 - T1: queueing time
	processingLatency prometheus.Histogram // T3 - T2: processing time by the app
	backendLatency    prometheus.Histogram // T4 - T3: processing time by the backend

	// Block counters (for compatibility with existing dashboard)
	blockSent     prometheus.Counter
	blockReceived prometheus.Counter

	// Gauges for current state
	outstandingTxGauge prometheus.Gauge
	throughputGauge    prometheus.Gauge

	// Queue size gauges
	batchSubmitterInputQueueSize prometheus.Gauge
	txQueueReadyListSize         prometheus.Gauge
	txQueueWaitingListSize       prometheus.Gauge

	registry *prometheus.Registry
	server   *http.Server
	mu       sync.Mutex
}

// NewLoadgenMetrics creates and registers all Prometheus metrics
func NewLoadgenMetrics() *LoadgenMetrics {
	registry := prometheus.NewRegistry()

	m := &LoadgenMetrics{
		transactionSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loadgen_transaction_sent_total",
			Help: "Total number of transactions sent to the gateway",
		}),
		transactionCommitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loadgen_transaction_committed_total",
			Help: "Total number of transactions committed successfully",
		}),
		transactionAborted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loadgen_transaction_aborted_total",
			Help: "Total number of transactions aborted or failed",
		}),
		totalLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_total_latency_seconds",
			Help:    "End-to-end latency (T4-T1: test submit to notification) in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20), // 1ms to ~524s
		}),
		queueLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_queue_latency_seconds",
			Help:    "Queueing latency (T2-T1: test submit to dequeue) in seconds",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 20), // 0.1ms to ~52s
		}),
		processingLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_processing_latency_seconds",
			Help:    "Processing latency (T3-T2: dequeue to batch submit) in seconds",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 20), // 0.1ms to ~52s
		}),
		backendLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_backend_latency_seconds",
			Help:    "Backend latency (T4-T3: batch submit to notification) in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20), // 1ms to ~524s
		}),
		blockSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loadgen_block_sent_total",
			Help: "Total number of blocks sent (for dashboard compatibility)",
		}),
		blockReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loadgen_block_received_total",
			Help: "Total number of blocks received (for dashboard compatibility)",
		}),
		outstandingTxGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loadgen_outstanding_transactions",
			Help: "Current number of outstanding transactions",
		}),
		throughputGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loadgen_throughput_tx_per_second",
			Help: "Current throughput in transactions per second",
		}),
		batchSubmitterInputQueueSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_batch_submitter_input_queue_size",
			Help: "Current size of the batch submitter input channel queue",
		}),
		txQueueReadyListSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_txqueue_ready_list_size",
			Help: "Current size of the transaction queue ready list",
		}),
		txQueueWaitingListSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_txqueue_waiting_list_size",
			Help: "Current size of the transaction queue waiting list",
		}),
		registry: registry,
	}

	// Register all metrics
	registry.MustRegister(
		m.transactionSent,
		m.transactionCommitted,
		m.transactionAborted,
		m.totalLatency,
		m.queueLatency,
		m.processingLatency,
		m.backendLatency,
		m.blockSent,
		m.blockReceived,
		m.outstandingTxGauge,
		m.throughputGauge,
		m.batchSubmitterInputQueueSize,
		m.txQueueReadyListSize,
		m.txQueueWaitingListSize,
	)

	return m
}

// StartServer starts the Prometheus metrics HTTP server on the specified address
func (m *LoadgenMetrics) StartServer(addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		return nil // Already started
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))

	m.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash the test
			println("Metrics server error:", err.Error())
		}
	}()

	return nil
}

// StopServer gracefully stops the metrics HTTP server
func (m *LoadgenMetrics) StopServer() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server == nil {
		return nil
	}

	return m.server.Close()
}

// RecordTransactionSent increments the sent transaction counter
func (m *LoadgenMetrics) RecordTransactionSent() {
	m.transactionSent.Inc()
}

// RecordTransactionCommitted increments the committed transaction counter
func (m *LoadgenMetrics) RecordTransactionCommitted() {
	m.transactionCommitted.Inc()
}

// RecordTransactionAborted increments the aborted transaction counter
func (m *LoadgenMetrics) RecordTransactionAborted() {
	m.transactionAborted.Inc()
}

// RecordLatencies records the four latency measurements
func (m *LoadgenMetrics) RecordLatencies(total, queue, processing, backend time.Duration) {
	m.totalLatency.Observe(total.Seconds())
	m.queueLatency.Observe(queue.Seconds())
	m.processingLatency.Observe(processing.Seconds())
	m.backendLatency.Observe(backend.Seconds())
}

// RecordBlockSent increments the block sent counter (for dashboard compatibility)
func (m *LoadgenMetrics) RecordBlockSent() {
	m.blockSent.Inc()
}

// RecordBlockReceived increments the block received counter (for dashboard compatibility)
func (m *LoadgenMetrics) RecordBlockReceived() {
	m.blockReceived.Inc()
}

// SetOutstandingTransactions sets the current number of outstanding transactions
func (m *LoadgenMetrics) SetOutstandingTransactions(count int64) {
	m.outstandingTxGauge.Set(float64(count))
}

// SetThroughput sets the current throughput in tx/s
func (m *LoadgenMetrics) SetThroughput(txPerSecond float64) {
	m.throughputGauge.Set(txPerSecond)
}

// SetBatchSubmitterInputQueueSize sets the current size of the batch submitter input queue
func (m *LoadgenMetrics) SetBatchSubmitterInputQueueSize(size int) {
	m.batchSubmitterInputQueueSize.Set(float64(size))
}

// SetTxQueueReadyListSize sets the current size of the transaction queue ready list
func (m *LoadgenMetrics) SetTxQueueReadyListSize(size int) {
	m.txQueueReadyListSize.Set(float64(size))
}

// SetTxQueueWaitingListSize sets the current size of the transaction queue waiting list
func (m *LoadgenMetrics) SetTxQueueWaitingListSize(size int) {
	m.txQueueWaitingListSize.Set(float64(size))
}
