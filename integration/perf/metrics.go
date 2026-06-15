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

	// Latency histograms
	validTxLatency   prometheus.Histogram
	invalidTxLatency prometheus.Histogram

	// Block counters (for compatibility with existing dashboard)
	blockSent     prometheus.Counter
	blockReceived prometheus.Counter

	// Gauges for current state
	outstandingTxGauge prometheus.Gauge
	throughputGauge    prometheus.Gauge

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
		validTxLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_valid_transaction_latency_seconds",
			Help:    "Latency of valid (committed) transactions in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20), // 1ms to ~524s
		}),
		invalidTxLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loadgen_invalid_transaction_latency_seconds",
			Help:    "Latency of invalid (aborted) transactions in seconds",
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
		registry: registry,
	}

	// Register all metrics
	registry.MustRegister(
		m.transactionSent,
		m.transactionCommitted,
		m.transactionAborted,
		m.validTxLatency,
		m.invalidTxLatency,
		m.blockSent,
		m.blockReceived,
		m.outstandingTxGauge,
		m.throughputGauge,
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

// RecordTransactionCommitted increments the committed transaction counter and records latency
func (m *LoadgenMetrics) RecordTransactionCommitted(latency time.Duration) {
	m.transactionCommitted.Inc()
	m.validTxLatency.Observe(latency.Seconds())
}

// RecordTransactionAborted increments the aborted transaction counter and records latency
func (m *LoadgenMetrics) RecordTransactionAborted(latency time.Duration) {
	m.transactionAborted.Inc()
	m.invalidTxLatency.Observe(latency.Seconds())
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

// Made with Bob
