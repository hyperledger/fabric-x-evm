/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/endorser"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/testimpl"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	gwcore "github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/metrics"
	gwtestimpl "github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	"github.com/hyperledger/fabric-x-evm/integration"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/grpclog"
)

// TxCompletionTracker tracks pending transactions and notifies workers when they complete.
// It implements gwcore.TxHandler to receive notifications from the notification system.
type TxCompletionTracker struct {
	mu      sync.RWMutex
	pending map[common.Hash]chan gwcore.TxNotification // eth hash -> completion channel
}

// NewTxCompletionTracker creates a new tracker.
func NewTxCompletionTracker() *TxCompletionTracker {
	return &TxCompletionTracker{
		pending: make(map[common.Hash]chan gwcore.TxNotification),
	}
}

// Register creates and returns a completion channel for the given transaction hash.
// The worker should call this before sending the transaction to avoid race conditions.
func (t *TxCompletionTracker) Register(ethHash common.Hash) <-chan gwcore.TxNotification {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Use buffered channel to avoid blocking the notifier goroutine
	ch := make(chan gwcore.TxNotification, 1)
	t.pending[ethHash] = ch
	return ch
}

// HandleTx implements gwcore.TxHandler. It receives notifications about completed transactions
// and signals the corresponding worker via the completion channel (if registered).
// Also unconditionally increments evm_loadgen_committed_total so open-loop runs (which
// don't register per tx) still get an accurate commit-rate metric.
func (t *TxCompletionTracker) HandleTx(ctx context.Context, notifs []gwcore.TxNotification) error {
	for _, notif := range notifs {
		// Bookkeeping: every notification = a commit observed by the loadgen, regardless of mode.
		if notif.Status == committerpb.Status_COMMITTED {
			metrics.LoadgenCommittedTotal.WithLabelValues("success").Inc()
		} else {
			metrics.LoadgenCommittedTotal.WithLabelValues("failed").Inc()
		}

		// Extract ethereum transaction hash from the notification
		var ethTx types.Transaction
		if err := ethTx.UnmarshalBinary(notif.EthTxBytes); err != nil {
			// Log error but don't fail - this shouldn't happen in normal operation
			fmt.Printf("TxCompletionTracker: failed to unmarshal eth tx: %v\n", err)
			continue
		}

		ethHash := ethTx.Hash()

		t.mu.Lock()
		ch, exists := t.pending[ethHash]
		if exists {
			delete(t.pending, ethHash)
		}
		t.mu.Unlock()

		if exists {
			// Send notification and close channel
			ch <- notif
			close(ch)
		}
		// If not exists (open-loop mode), the tx wasn't registered; the metric bump above is the only signal.
	}

	return nil
}

// Cleanup removes any pending registrations (useful for cleanup on shutdown).
func (t *TxCompletionTracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Close all pending channels
	for _, ch := range t.pending {
		close(ch)
	}
	t.pending = make(map[common.Hash]chan gwcore.TxNotification)
}

// balancePrimingEndorserFactory creates endorsers with balance priming support for testing.
func balancePrimingEndorserFactory(balancePriming *testimpl.BalancePrimingConfig) integration.EndorserFactory {
	return func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig endorser.EVMConfig, protocol string) (endorser.KVS, endorsement.Builder, gwcore.Endorser) {
		// Create the base endorser components
		db, builder, baseEndorser := integration.NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)

		// Extract the base EVMEngine
		baseEngine, ok := baseEndorser.Engine.(*endorser.EVMEngine)
		if !ok {
			t.Fatalf("Expected *endorser.EVMEngine, got %T", baseEndorser.Engine)
		}

		// Wrap the engine with balance priming support
		wrappedEngine := testimpl.NewEVMEngineWrapper(
			namespace,
			db,
			evmConfig,
			protocol == "fabric-x", // monotonicVersions
			baseEngine,
		)
		wrappedEngine.SetBalancePriming(balancePriming)

		// Replace the engine in the endorser
		baseEndorser.Engine = wrappedEngine

		return db, builder, baseEndorser
	}
}

// loopMode controls per-tx submission semantics. "closed" waits for each tx's
// commit notification before continuing. "open" fires and continues — no per-tx
// wait, no per-tx latency measurement.
type loopMode string

const (
	loopModeClosed loopMode = "closed"
	loopModeOpen   loopMode = "open"
)

// queueKind picks the in-process gateway's TxQueue implementation.
// "auto" mirrors the loop mode (closed→v2, open→v1) — the historical default.
type queueKind string

const (
	queueAuto queueKind = "auto"
	queueV1   queueKind = "v1" // FIFO, no dependency tracking
	queueV2   queueKind = "v2" // dependency graph
)

type replayConfig struct {
	// windowSize is the number of transfers to use from the dataset.
	// 0 means use the entire dataset.
	windowSize int

	// wrapAround, when true, restarts the feed from the beginning of the
	// window after every pass. The feed continues until totalDispatches
	// transfers have been sent to workChan. Ignored when false.
	wrapAround bool

	// wrapCount is the raw wrap count requested by configuration.
	// totalDispatches is computed later, after the effective window size is known.
	wrapCount int64

	// totalDispatches is the total number of transfers to dispatch when
	// wrapAround is true. Ignored when wrapAround is false.
	totalDispatches int64

	// loop sets the per-tx mode. Default closed.
	loop loopMode

	// forever, when true, the feeder keeps wrapping the dataset until the
	// test context is cancelled (e.g. via duration or Ctrl-C). Implies
	// wrapAround=true; ignores wrapCount/totalDispatches.
	forever bool

	// duration, when > 0, cancels the test ctx after that long. Combine
	// with forever for a steady-state TPS-for-N-seconds measurement.
	duration time.Duration

	// targetTPS, when > 0, throttles SendTransaction calls (open-loop only)
	// via a global rate.Limiter shared across all submitting workers, so the
	// aggregate submit rate is bounded. 0 = unlimited (legacy fire-and-forget).
	targetTPS int

	// queueKind picks the in-process gateway's TxQueue implementation.
	// "auto" (default) mirrors loop mode; "v1" / "v2" force a specific queue.
	queueKind queueKind
}

func loadReplayConfigFromEnv(t *testing.T) replayConfig {
	cfg := replayConfig{windowSize: 3000, wrapAround: false, loop: loopModeClosed, queueKind: queueAuto}

	windowSizeStr := os.Getenv("PERF_REPLAY_WINDOW_SIZE")
	if windowSizeStr != "" {
		var parsedWindowSize int
		_, err := fmt.Sscanf(windowSizeStr, "%d", &parsedWindowSize)
		assert.NoError(t, err, "PERF_REPLAY_WINDOW_SIZE must be a valid integer")
		assert.True(t, parsedWindowSize >= 0, "PERF_REPLAY_WINDOW_SIZE must be >= 0")
		cfg.windowSize = parsedWindowSize
	}

	if cfg.windowSize == 0 {
		t.Log("WARNING: full dataset mode selected — this is intended for distributed infra, not local runs")
	}

	wrapCountStr := os.Getenv("PERF_REPLAY_WRAP_COUNT")
	if wrapCountStr != "" {
		var wrapCount int64
		_, err := fmt.Sscanf(wrapCountStr, "%d", &wrapCount)
		assert.NoError(t, err, "PERF_REPLAY_WRAP_COUNT must be a valid integer")
		assert.True(t, wrapCount >= 1, "PERF_REPLAY_WRAP_COUNT must be >= 1")
		cfg.wrapCount = wrapCount
		if wrapCount > 1 {
			cfg.wrapAround = true
		}
	}

	switch strings.ToLower(os.Getenv("PERF_LOOP_MODE")) {
	case "", "closed":
		cfg.loop = loopModeClosed
	case "open":
		cfg.loop = loopModeOpen
	default:
		t.Fatalf("PERF_LOOP_MODE must be 'closed' or 'open', got %q", os.Getenv("PERF_LOOP_MODE"))
	}

	if os.Getenv("PERF_FOREVER") == "1" {
		cfg.forever = true
		cfg.wrapAround = true
	}

	if v := os.Getenv("PERF_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		assert.NoError(t, err, "PERF_DURATION must be a valid Go duration (e.g. 60s, 2m)")
		cfg.duration = d
	}

	if v := os.Getenv("PERF_TARGET_TPS"); v != "" {
		var parsed int
		_, err := fmt.Sscanf(v, "%d", &parsed)
		assert.NoError(t, err, "PERF_TARGET_TPS must be a valid integer")
		assert.True(t, parsed >= 0, "PERF_TARGET_TPS must be >= 0 (0 = unlimited)")
		cfg.targetTPS = parsed
	}

	switch strings.ToLower(os.Getenv("PERF_TXQUEUE")) {
	case "", "auto":
		cfg.queueKind = queueAuto
	case "v1":
		cfg.queueKind = queueV1
	case "v2":
		cfg.queueKind = queueV2
	default:
		t.Fatalf("PERF_TXQUEUE must be 'v1', 'v2', or 'auto', got %q", os.Getenv("PERF_TXQUEUE"))
	}

	return cfg
}

//lint:ignore U1000 kept for future tests / debugging
func logMem(tag string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[%s] Alloc = %d MB | TotalAlloc = %d MB | Sys = %d MB | NumGC = %d\n",
		tag,
		m.Alloc/1024/1024,
		m.TotalAlloc/1024/1024,
		m.Sys/1024/1024,
		m.NumGC,
	)
}

//lint:ignore U1000 kept for future tests / debugging
func writeHeapProfile(filename string) {
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	runtime.GC() // normalize heap before snapshot
	if err := pprof.WriteHeapProfile(f); err != nil {
		panic(err)
	}
}

// runReplayTest executes the replay test with configurable worker counts and returns metrics.
// Returns: (overallThroughput, failedTransactionCount, totalTransactionCount)
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int, cfg replayConfig) (float64, int64, int64) {
	if os.Getenv("PERF_PPROF") == "1" {
		go func() {
			t.Logf("Starting pprof server on http://localhost:6060/debug/pprof/")
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				t.Logf("pprof server failed: %v", err)
			}
		}()
	}

	// Silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))
	t.Logf("Config: processingWorkers=%d submittingWorkers=%d loop=%s forever=%v duration=%s",
		processingWorkerCount, submittingWorkerCount, cfg.loop, cfg.forever, cfg.duration)

	// Publish worker config as gauges so any Grafana chart can render it alongside
	// throughput — no need to flip back to the run log to know the worker mix.
	metrics.LoadgenWorkers.WithLabelValues("processing").Set(float64(processingWorkerCount))
	metrics.LoadgenWorkers.WithLabelValues("submitting").Set(float64(submittingWorkerCount))

	// Derive a cancellable feeder context. The duration timer is ARMED later
	// (just before the feeder loop), AFTER the harness has synced — otherwise on
	// a stale chain with many blocks to catch up, the timer can expire during
	// sync and the workload never runs.
	feederCtx, feederCancel := context.WithCancel(t.Context())
	defer feederCancel()

	// USDC contract address
	USDCAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")

	// Configure balance priming for USDC transfers
	balancePriming := &testimpl.BalancePrimingConfig{
		Enabled:         true,
		ContractAddress: USDCAddr,
		MappingPosition: 9, // USDC balance mapping is at slot 9
	}
	evmConfig := endorser.EVMConfig{}

	// Setup test harness with USDC contract and balance priming enabled
	factory := balancePrimingEndorserFactory(balancePriming)

	// Create completion tracker for async transaction monitoring
	tracker := NewTxCompletionTracker()
	defer tracker.Cleanup()

	// Queue selection: controlled by PERF_TXQUEUE ("v1", "v2", or "auto"). "auto"
	// mirrors loop mode — V2 for closed-loop (dependency-graph aware, avoids MVCC
	// aborts) and V1 for open-loop (FIFO, no dependency tracking — push as fast as
	// we can and let the committer's MVCC handle conflicts). Setting v1 or v2
	// explicitly forces that queue regardless of loop mode, for A/B testing.
	kind := cfg.queueKind
	if kind == queueAuto {
		if cfg.loop == loopModeOpen {
			kind = queueV1
		} else {
			kind = queueV2
		}
	}
	var txQueue gwcore.TxQueueInterface
	switch kind {
	case queueV1:
		t.Logf("TxQueue: V1 FIFO (no dependency tracking)")
		txQueue = gwcore.NewTxQueue()
	case queueV2:
		t.Logf("TxQueue: V2 dependency-graph")
		txQueue = gwcore.NewTxQueueV2()
	}

	// Use notification-based harness from PR #190 for fabric-x.
	th, err := integration.NewFabricXTestHarnessWithNotifications(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", map[string]any{"Gateway.WorkerCount": processingWorkerCount}, factory, txQueue, tracker)
	require.NoError(t, err)

	// Wrap the gateway with NonceBypassGateway to skip nonce validation
	// This is necessary for wrap-around replay where the same transactions are replayed
	wrappedGateway := gwtestimpl.NewNonceBypassGateway(th.Gateways[0])

	// Load the JSON dataset
	datasetPath := "testdata/USDC_dataset.json.gz"
	t.Logf("Loading dataset from %s", datasetPath)

	file, err := os.Open(datasetPath)
	assert.NoError(t, err)
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	assert.NoError(t, err)
	defer gzReader.Close()

	var allTransfers []utils.TokenTransfer
	decoder := json.NewDecoder(gzReader)
	err = decoder.Decode(&allTransfers)
	assert.NoError(t, err)
	assert.NotEmpty(t, allTransfers, "dataset should contain transfers")

	t.Logf("Loaded %d transfers from dataset", len(allTransfers))

	window := allTransfers
	if cfg.windowSize > 0 && cfg.windowSize < len(allTransfers) {
		window = allTransfers[:cfg.windowSize]
	}

	if cfg.wrapAround && cfg.wrapCount > 0 {
		cfg.totalDispatches = int64(len(window)) * cfg.wrapCount
	}

	// Replay transactions with parallel workers
	// Atomic counters for thread-safe counting
	var successCount, failCount, skippedCount int64

	var latenciesMu sync.Mutex
	var latencies []float64

	var sentCount, committedCount atomic.Int64

	runtime.GC()

	// Track throughput
	startTime := time.Now()
	var lastLogTime atomic.Value
	lastLogTime.Store(startTime)
	var lastLogCount int64

	// Create a channel for work items
	type workItem struct {
		index    int64
		transfer utils.TokenTransfer
	}
	workChan := make(chan workItem, 500) // Buffer to avoid blocking

	// Worker pool configuration
	numWorkers := submittingWorkerCount
	var wg sync.WaitGroup

	// Optional rate limiter (open-loop only): one limiter shared across all
	// submitting workers, so the AGGREGATE SendTransaction rate is bounded
	// by cfg.targetTPS. nil = unlimited (legacy fire-and-forget).
	var submitLimiter *rate.Limiter
	if cfg.targetTPS > 0 && cfg.loop == loopModeOpen {
		submitLimiter = rate.NewLimiter(rate.Limit(cfg.targetTPS), 1)
		t.Logf("Open-loop rate limit: %d tx/s (shared across %d submitting workers)",
			cfg.targetTPS, numWorkers)
	}

	// Start worker goroutines
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for item := range workChan {
				i := item.index
				transfer := item.transfer
				txStart := time.Now()

				// Skip transfers without transactions
				if len(transfer.Transaction) == 0 {
					atomic.AddInt64(&skippedCount, 1)
					continue
				}

				// Unmarshal the transaction from bytes
				tx := new(types.Transaction)
				err := tx.UnmarshalBinary(transfer.Transaction)
				if err != nil {
					t.Logf("Transfer %d: Failed to unmarshal transaction: %v", i, err)
					atomic.AddInt64(&failCount, 1)
					continue
				}

				if cfg.loop == loopModeOpen {
					// Open-loop: fire and continue. No per-tx wait, no per-tx latency
					// measurement on this side. Commit counts come from
					// LoadgenCommittedTotal which TxCompletionTracker.HandleTx
					// increments on every notification.
					if submitLimiter != nil {
						_ = submitLimiter.Wait(context.Background())
					}
					if err := wrappedGateway.SendTransaction(context.Background(), tx); err != nil {
						t.Logf("Transfer %d: SendTransaction error: %v", i, err)
						atomic.AddInt64(&failCount, 1)
						continue
					}
					atomic.AddInt64(&successCount, 1) // "successfully submitted" in open-loop
					sentCount.Add(1)
					continue
				}

				// Closed-loop: send the transaction and wait for it to be committed
				func() {
					inflightBumped := false
					defer func() {
						if r := recover(); r != nil {
							// t.Logf("Transfer %d: Failed to send transaction (panic recovered): %v", i, r)
							atomic.AddInt64(&failCount, 1)
						} else {
							atomic.AddInt64(&successCount, 1)
							latSec := time.Since(txStart).Seconds()
							latMs := latSec * 1000
							latenciesMu.Lock()
							latencies = append(latencies, latMs)
							latenciesMu.Unlock()
							metrics.LoadgenCommittedLatency.Observe(latSec)
						}
						if inflightBumped {
							metrics.LoadgenInflight.Dec()
						}
					}()

					// Register for completion notification BEFORE sending
					completionCh := tracker.Register(tx.Hash())

					// Use the wrapped gateway directly to bypass nonce validation
					err = wrappedGateway.SendTransaction(context.Background(), tx)
					if err != nil {
						t.Logf("Transfer %d: SendTransaction error: %v", i, err)
						panic(err) // Trigger the defer recovery
					}
					sentCount.Add(1)
					metrics.LoadgenInflight.Inc()
					inflightBumped = true // track that we Inc'd so the defer knows to Dec

					// Wait for transaction completion notification from the tracker
					select {
					case notif := <-completionCh:
						// Transaction committed - check status
						if notif.Status != committerpb.Status_COMMITTED { // 0 = COMMITTED in fabric-x
							t.Logf("Transfer %d: Transaction failed with status: %v", i, notif.Status)
							panic(fmt.Sprintf("transaction failed with status %v", notif.Status))
						}
					case <-t.Context().Done():
						// Test context cancelled
						panic("test context cancelled")
					}
					committedCount.Add(1)
				}()
			}
		}(w)
	}

	// recentSamples collects windowed throughput values for stability reporting.
	// Written only by the logging goroutine; safe to read after loggingWg.Wait().
	var recentSamples []float64

	// Progress logging goroutine
	stopLogging := make(chan struct{})
	var loggingWg sync.WaitGroup
	loggingWg.Add(1)
	go func() {
		defer loggingWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				now := time.Now()
				lastTime := lastLogTime.Load().(time.Time)
				elapsed := now.Sub(lastTime).Seconds()

				currentSuccess := atomic.LoadInt64(&successCount)
				currentFail := atomic.LoadInt64(&failCount)
				currentSkipped := atomic.LoadInt64(&skippedCount)
				currentTotal := currentSuccess + currentFail

				txProcessed := currentTotal - lastLogCount
				throughput := float64(txProcessed) / elapsed
				recentSamples = append(recentSamples, throughput)

				totalElapsed := now.Sub(startTime).Seconds()
				overallThroughput := float64(currentTotal) / totalElapsed

				progressTarget := int64(len(window))
				if cfg.wrapAround {
					progressTarget = cfg.totalDispatches
				}

				sent := sentCount.Load()
				committed := committedCount.Load()
				t.Logf("Progress: %d/%d transfers processed (%d successful, %d failed, %d skipped) | Throughput: %.2f tx/s (recent), %.2f tx/s (overall) | sent=%d committed=%d inflight=%d",
					currentSuccess+currentFail+currentSkipped, progressTarget,
					currentSuccess, currentFail, currentSkipped,
					throughput, overallThroughput,
					sent, committed, sent-committed)

				// Update for next interval
				lastLogTime.Store(now)
				lastLogCount = currentTotal

			case <-stopLogging:
				return
			}
		}
	}()

	// Arm the duration timer NOW (just before the feeder loop) so that sync /
	// dataset-load time isn't deducted from PERF_DURATION. The harness has
	// finished syncing by the time we reach here.
	if cfg.duration > 0 {
		timer := time.AfterFunc(cfg.duration, func() {
			t.Logf("PERF_DURATION reached (%s), cancelling feeder", cfg.duration)
			feederCancel()
		})
		defer timer.Stop()
	}

	// Feed work to the workers. Termination has three paths:
	//   - wrap-count: stop when dispatched >= cfg.totalDispatches (cfg.wrapAround && !cfg.forever)
	//   - single-pass: stop when cursor exhausts window (no wrapAround, no forever)
	//   - forever / duration: stop only when feederCtx is cancelled (cfg.forever)
	var dispatched int64
	cursor := 0

feed:
	for {
		select {
		case <-feederCtx.Done():
			break feed
		default:
		}

		if cfg.forever {
			// nothing to do — only feederCtx terminates us
		} else if cfg.wrapAround {
			if dispatched >= cfg.totalDispatches {
				break feed
			}
		} else if cursor >= len(window) {
			break feed
		}

		// blocking send — but respect feederCtx so we can exit cleanly.
		select {
		case workChan <- workItem{index: dispatched, transfer: window[cursor]}:
		case <-feederCtx.Done():
			break feed
		}

		dispatched++
		cursor++

		if cursor >= len(window) {
			if cfg.wrapAround || cfg.forever {
				cursor = 0
				// BalancePrimingWrapper.GetNonce() handles nonce validation bypass automatically,
				// so no explicit nonce priming is needed between wrap-around passes.
				t.Logf("Wrap-around: restarting from beginning (dispatched %d so far)", dispatched)
			} else {
				break feed
			}
		}
	}

	// Close the work channel and wait for all workers to finish
	close(workChan)
	wg.Wait()

	// Stop the logging goroutine
	close(stopLogging)
	loggingWg.Wait()

	if len(recentSamples) >= 2 {
		var sum float64
		minTPS, maxTPS := recentSamples[0], recentSamples[0]
		for _, s := range recentSamples {
			sum += s
			if s < minTPS {
				minTPS = s
			}
			if s > maxTPS {
				maxTPS = s
			}
		}
		mean := sum / float64(len(recentSamples))
		var variance float64
		for _, s := range recentSamples {
			d := s - mean
			variance += d * d
		}
		stddev := math.Sqrt(variance / float64(len(recentSamples)))
		cv := 0.0
		if mean > 0 {
			cv = stddev / mean * 100
		}
		t.Logf("TPS stability (%d samples): min=%.2f max=%.2f avg=%.2f stddev=%.2f CV=%.1f%%",
			len(recentSamples), minTPS, maxTPS, mean, stddev, cv)
	}

	if len(latencies) > 0 {
		sortedLat := make([]float64, len(latencies))
		copy(sortedLat, latencies)
		sort.Float64s(sortedLat)

		pct := func(p int) float64 {
			idx := len(sortedLat) * p / 100
			if idx >= len(sortedLat) {
				idx = len(sortedLat) - 1
			}
			return sortedLat[idx]
		}

		t.Logf("Latency: p50=%.2fms p90=%.2fms p99=%.2fms max=%.2fms",
			pct(50), pct(90), pct(99), sortedLat[len(sortedLat)-1])
	}

	// Final counts
	finalSuccess := atomic.LoadInt64(&successCount)
	finalFail := atomic.LoadInt64(&failCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	t.Logf("Replay complete: %d successful, %d failed, %d skipped out of %d total transfers",
		finalSuccess, finalFail, finalSkipped, dispatched)

	// Calculate overall throughput
	totalElapsed := time.Since(startTime).Seconds()
	overallThroughput := float64(finalSuccess+finalFail) / totalElapsed
	t.Logf("Result: Throughput=%.2f tx/s", overallThroughput)

	// Return metrics (throughput, failed count, total dispatched transfers)
	return overallThroughput, finalFail, dispatched
}

// TestReplayJSONDataset loads the USDC_dataset.json.gz file with pre-generated transactions
// and replays them with batched priming of sender balances.
// TestMain starts the EVM-side metrics HTTP server once for the whole package so
// both TestReplayJSONDataset and the sweep TestReplayJSONDatasetPerformance can
// be scraped by Prometheus.
//
// Resolution order for the bind addr:
//  1. EVM_METRICS_ADDR env var (escape hatch for ad-hoc runs without a config file).
//  2. loadgen.metrics-addr in the fabx.yaml at FABX_CONFIG_PATH (the canonical
//     source — symmetric with how orderer/committer get their metrics ports).
//  3. Otherwise, no metrics endpoint is started.
func TestMain(m *testing.M) {
	addr := os.Getenv("EVM_METRICS_ADDR")
	if addr == "" {
		if cfgPath := os.Getenv("FABX_CONFIG_PATH"); cfgPath != "" {
			if cfg, err := config.Load(cfgPath); err == nil {
				addr = cfg.Loadgen.MetricsAddr
			}
		}
	}
	var srv *http.Server
	if addr != "" {
		s, err := metrics.Listen(addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "EVM metrics Listen(%s) failed: %v\n", addr, err)
			os.Exit(1)
		}
		if s != nil {
			fmt.Fprintf(os.Stderr, "EVM-side metrics serving on http://%s/metrics\n", addr)
			srv = s
		}
	}
	code := m.Run()
	if srv != nil {
		metrics.Shutdown(srv)
	}
	os.Exit(code)
}

func TestReplayJSONDataset(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// flogging.ActivateSpec("gateway.core.txqueue_v2=debug")

	processingWorkers := 1
	if v := os.Getenv("PERF_PROCESSING_WORKERS"); v != "" {
		_, err := fmt.Sscanf(v, "%d", &processingWorkers)
		assert.NoError(t, err, "PERF_PROCESSING_WORKERS must be a valid integer")
		assert.True(t, processingWorkers >= 1, "PERF_PROCESSING_WORKERS must be >= 1")
	}
	submittingWorkers := 8
	if v := os.Getenv("PERF_SUBMITTING_WORKERS"); v != "" {
		_, err := fmt.Sscanf(v, "%d", &submittingWorkers)
		assert.NoError(t, err, "PERF_SUBMITTING_WORKERS must be a valid integer")
		assert.True(t, submittingWorkers >= 1, "PERF_SUBMITTING_WORKERS must be >= 1")
	}
	_, _, _ = runReplayTest(t, processingWorkers, submittingWorkers, loadReplayConfigFromEnv(t))
}

type performanceResult struct {
	processingWorkers  int
	submittingWorkers  int
	throughput         float64
	failedTransactions int64
	totalTransactions  int64
	failureRate        float64
}

// TestReplayJSONDatasetPerformance runs the replay test with varying worker counts
// to measure performance characteristics across different configurations.
func TestReplayJSONDatasetPerformance(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Define the range of worker counts to test
	processingWorkerCounts := []int{1, 4, 8}
	submittingWorkerCounts := []int{4, 8, 16, 24}

	// Store results
	var results []performanceResult

	t.Logf("Starting performance test with varying worker counts...")

	// Run tests with different worker configurations
	for _, processingWorkers := range processingWorkerCounts {
		for _, submittingWorkers := range submittingWorkerCounts {
			t.Logf("\n=== Testing with processingWorkers=%d, submittingWorkers=%d ===",
				processingWorkers, submittingWorkers)

			throughput, failedTxs, totalTxs := runReplayTest(t, processingWorkers, submittingWorkers, loadReplayConfigFromEnv(t))
			failureRate := float64(failedTxs) / float64(totalTxs)

			results = append(results, performanceResult{
				processingWorkers:  processingWorkers,
				submittingWorkers:  submittingWorkers,
				throughput:         throughput,
				failedTransactions: failedTxs,
				totalTransactions:  totalTxs,
				failureRate:        failureRate,
			})

			t.Logf("Result: Throughput=%.2f tx/s, Failed=%d/%d (%.2f%%)",
				throughput, failedTxs, totalTxs, failureRate*100)
			t.Logf("PerfResult: pw=%d sw=%d throughput=%.2f failed=%d total=%d",
				processingWorkers, submittingWorkers, throughput, failedTxs, totalTxs)
		}
	}

	// Write results to CSV file
	csvPath := "performance_results.csv"
	file, err := os.Create(csvPath)
	assert.NoError(t, err)
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{
		"processing_workers",
		"submitting_workers",
		"throughput_tx_per_s",
		"failed_transactions",
		"total_transactions",
		"failure_rate",
	})
	assert.NoError(t, err)

	// Write data rows
	for _, result := range results {
		err = writer.Write([]string{
			fmt.Sprintf("%d", result.processingWorkers),
			fmt.Sprintf("%d", result.submittingWorkers),
			fmt.Sprintf("%.2f", result.throughput),
			fmt.Sprintf("%d", result.failedTransactions),
			fmt.Sprintf("%d", result.totalTransactions),
			fmt.Sprintf("%.4f", result.failureRate),
		})
		assert.NoError(t, err)
	}

	t.Logf("Performance results written to %s", csvPath)
}
