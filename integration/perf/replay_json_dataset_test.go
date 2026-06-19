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
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
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
	gwcore "github.com/hyperledger/fabric-x-evm/gateway/core"
	gwtestimpl "github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	"github.com/hyperledger/fabric-x-evm/integration"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
)

var gatewayConfig = flag.String("gateway-config", "fabx.yaml", "gateway config file for the Fabric-X network")
var metricsAddr = flag.String("metrics-addr", "0.0.0.0:2112", "address for Prometheus metrics endpoint")
var enableMetrics = flag.Bool("enable-metrics", false, "enable Prometheus metrics export")
var namespace = flag.String("namespace", "real", "namespace to commit transactions to")
var dataset = flag.String("dataset", "testdata/USDC_dataset.json.gz", "dataset to use")
var oldqueue = flag.Bool("oldqueue", false, "enable old queue")
var workers = flag.Int("workers", 20, "number of gateway workers processing transactions")
var submitters = flag.Int("submitters", 4, "number of goroutines submitting transactions to the gateway")
var orderers = flag.Int("orderers", 8, "number of goroutines submitting transactions to the orderer (BatchSubmitter workers)")
var outstanding = flag.Int("outstanding", 1000, "maximum number of outstanding transactions")

// TxCompletionTracker forwards all transaction completion notifications to a single channel.
// It implements gwcore.TxHandler to receive notifications from the notification system.
type TxCompletionTracker struct {
	mu           sync.Mutex
	completionCh chan gwcore.TxNotification
	stopped      bool
}

// NewTxCompletionTracker creates a new tracker with a completion channel.
func NewTxCompletionTracker(completionCh chan gwcore.TxNotification) *TxCompletionTracker {
	return &TxCompletionTracker{
		completionCh: completionCh,
	}
}

// Stop prevents any further sends to the completion channel. Must be called before
// closing the channel to avoid panics from in-flight notification goroutines.
func (t *TxCompletionTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

// HandleTx implements gwcore.TxHandler. It receives notifications about completed transactions
// and forwards them to the completion channel.
func (t *TxCompletionTracker) HandleTx(ctx context.Context, notifs []gwcore.TxNotification) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return nil
	}
	for _, notif := range notifs {
		select {
		case t.completionCh <- notif:
		default:
			// Channel full - this shouldn't happen with proper sizing
			return fmt.Errorf("completion channel full, dropping notification for tx %s", notif.EthTxHash.Hex())
		}
	}
	return nil
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
}

func loadReplayConfigFromEnv(t *testing.T) replayConfig {
	cfg := replayConfig{windowSize: 3000, wrapAround: false}

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
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int, ordererSubmitterCount int, numOutstandingTx int, cfg replayConfig, gwConfig string) (float64, int64, int64) {
	// Silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	// Initialize Prometheus metrics if enabled
	var metrics *LoadgenMetrics
	if *enableMetrics {
		metrics = NewLoadgenMetrics()
		if err := metrics.StartServer(*metricsAddr); err != nil {
			t.Logf("Failed to start metrics server: %v", err)
		} else {
			t.Logf("Prometheus metrics available at http://localhost%s/metrics", *metricsAddr)
			defer metrics.StopServer()
		}

		// Wire up queue size metrics callbacks
		gwcore.SetBatchSubmitterQueueSizeMetric = metrics.SetBatchSubmitterInputQueueSize
		gwcore.SetTxQueueReadyListSizeMetric = metrics.SetTxQueueReadyListSize
		gwcore.SetTxQueueWaitingListSizeMetric = metrics.SetTxQueueWaitingListSize
	}

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

	// Create completion channel for transaction notifications
	completionCh := make(chan gwcore.TxNotification, numOutstandingTx*2)

	// Create completion tracker for async transaction monitoring
	tracker := NewTxCompletionTracker(completionCh)

	// Choose test harness based on backend:
	// - Local: Traditional block-based synchronization
	// - Fabric: Traditional block-based synchronization
	// - Fabric-X: Notification-based (MemoryStore + NotificationDispatcher)
	// th, err := integration.NewLocalTestHarnessWithFactoryAndTxQueue(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", "fabric", map[string]any{"Gateway.WorkerCount": processingWorkerCount, "Gateway.SubmitterCount": ordererSubmitterCount, "Network.Namespace": *namespace}, factory, gwcore.NewTxQueueV2())
	var queue gwcore.TxQueueInterface
	if *oldqueue {
		queue = gwcore.NewTxQueue()
	} else {
		queue = gwcore.NewTxQueueV2()
	}
	fmt.Printf("using queue type %T\n", queue)
	fmt.Printf("using namespace %s", *namespace)
	th, err := integration.NewFabricXTestHarnessWithNotifications(
		t,
		integration.TestLogger{T: t},
		evmConfig,
		"testdata/USDC_contract.json",
		map[string]any{
			"Gateway.WorkerCount":    processingWorkerCount,
			"Gateway.SubmitterCount": ordererSubmitterCount,
			"Network.Namespace":      *namespace,
		},
		factory,
		queue,
		tracker,
		gwConfig)
	// th, err = integration.NewFabricTestHarnessWithFactoryAndTxQueue(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", map[string]any{"Gateway.WorkerCount": processingWorkerCount, "Gateway.SubmitterCount": ordererSubmitterCount, "Network.Namespace": *namespace}, factory, gwcore.NewTxQueueV2())
	assert.NoError(t, err)

	// wait for the priming tx to be committed: we can no longer
	// rely on commit checks because we have disabled the block store
	time.Sleep(time.Second)

	// Wrap the gateway with NonceBypassGateway to skip nonce validation
	// This is necessary for wrap-around replay where the same transactions are replayed
	wrappedGateway := gwtestimpl.NewNonceBypassGateway(th.Gateways[0])

	// Load the JSON dataset
	// The dataset path can be:
	// 1. An absolute path
	// 2. A relative path from the current working directory
	// 3. A relative path from the repo root (../../ from this test file)
	//
	// When running `go test ./integration/perf/...` from repo root, the test's
	// working directory becomes integration/perf/, so we try both cwd and repo root.
	datasetPath := *dataset

	var file *os.File
	var fileErr error

	if filepath.IsAbs(datasetPath) {
		// Absolute path - use as-is
		file, fileErr = os.Open(datasetPath)
		if fileErr != nil {
			t.Fatalf("Failed to open dataset file %s: %v", datasetPath, fileErr)
		}
	} else {
		// Relative path - try from cwd first, then from repo root
		file, fileErr = os.Open(datasetPath)
		if fileErr != nil {
			// Try from repo root (../../ from integration/perf/)
			repoRootPath := filepath.Join("..", "..", datasetPath)
			file, fileErr = os.Open(repoRootPath)
			if fileErr != nil {
				t.Fatalf("Failed to open dataset file. Tried:\n  1. %s\n  2. %s\nError: %v",
					datasetPath, repoRootPath, fileErr)
			}
			datasetPath = repoRootPath
		}
	}
	defer file.Close()

	t.Logf("Loading dataset from %s", datasetPath)

	gzReader, err := gzip.NewReader(file)
	assert.NoError(t, err)
	defer gzReader.Close()

	var allTransfers []TokenTransfer
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

	// Validate numOutstandingTx
	if numOutstandingTx > len(window) {
		panic(fmt.Sprintf("numOutstandingTx (%d) cannot be larger than window size (%d)", numOutstandingTx, len(window)))
	}

	// Replay transactions with parallel workers
	// Atomic counters for thread-safe counting
	var successCount, failCount, skippedCount int64

	// Latency tracking: map transaction hash to submission time (T1)
	latencyMu := sync.Mutex{}
	submissionTimes := make(map[common.Hash]time.Time)

	// Enable T2 timestamp tracking in txqueue (when dequeued for processing)
	gwcore.ProcessingStartTimestamps = make(map[common.Hash]time.Time)
	defer func() {
		gwcore.ProcessingStartTimestamps = nil // Clean up after test
	}()

	// Enable T3 timestamp tracking in batch_submitter (when submitted to orderer)
	gwcore.SubmissionTimestamps = make(map[common.Hash]time.Time)
	defer func() {
		gwcore.SubmissionTimestamps = nil // Clean up after test
	}()

	runtime.GC()

	// Track throughput
	startTime := time.Now()
	var lastLogTime atomic.Value
	lastLogTime.Store(startTime)
	var lastLogCount int64

	// Create a channel for work items
	type workItem struct {
		index    int64
		transfer TokenTransfer
	}
	// Buffer size = numOutstandingTx + numWorkers to avoid blocking
	workChan := make(chan workItem, numOutstandingTx+submittingWorkerCount)

	// Metrics for outstanding transactions
	var outstandingTxCount int64

	// Worker pool configuration
	numWorkers := submittingWorkerCount
	var wg sync.WaitGroup

	// Start worker goroutines - they continuously submit without waiting for completion
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for item := range workChan {
				i := item.index
				transfer := item.transfer

				// Unmarshal the transaction from bytes
				tx := new(types.Transaction)
				err := tx.UnmarshalBinary(transfer.Transaction)
				if err != nil {
					t.Logf("Transfer %d: Failed to unmarshal transaction: %v", i, err)
					panic(err)
				}

				// Record submission time
				txHash := tx.Hash()
				submissionTime := time.Now()
				latencyMu.Lock()
				submissionTimes[txHash] = submissionTime
				latencyMu.Unlock()

				// Send the transaction without waiting for completion
				// Use the wrapped gateway directly to bypass nonce validation
				err = wrappedGateway.SendTransaction(context.Background(), tx)
				if err != nil {
					t.Logf("Transfer %d: SendTransaction error: %v", i, err)
					atomic.AddInt64(&failCount, 1)
					atomic.AddInt64(&outstandingTxCount, -1)
					// Remove from tracking on failure
					latencyMu.Lock()
					delete(submissionTimes, txHash)
					latencyMu.Unlock()
					continue
				}
				// Transaction submitted successfully - it's now outstanding
				// The completion will be tracked by the refill goroutine
				if metrics != nil {
					metrics.RecordTransactionSent()
				}
			}
		}()
	}

	// Progress logging goroutine
	stopLogging := make(chan struct{})
	var loggingWg sync.WaitGroup
	loggingWg.Add(1)
	go func() {
		defer loggingWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		itrctr := 0

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
				currentOutstanding := atomic.LoadInt64(&outstandingTxCount)

				txProcessed := currentTotal - lastLogCount
				throughput := float64(txProcessed) / elapsed

				totalElapsed := now.Sub(startTime).Seconds()
				overallThroughput := float64(currentTotal) / totalElapsed

				progressTarget := int64(len(window))
				if cfg.wrapAround {
					progressTarget = cfg.totalDispatches
				}

				t.Logf("Progress: %d/%d transfers processed (%d successful, %d failed, %d skipped, %d outstanding) | Throughput: %.2f tx/s (recent), %.2f tx/s (overall)",
					currentSuccess+currentFail+currentSkipped, progressTarget,
					currentSuccess, currentFail, currentSkipped, currentOutstanding,
					throughput, overallThroughput)

				// Update metrics
				if metrics != nil {
					metrics.SetOutstandingTransactions(currentOutstanding)
					metrics.SetThroughput(overallThroughput)
				}

				// Update for next interval
				lastLogTime.Store(now)
				lastLogCount = currentTotal

				_ = itrctr
				// if itrctr%50 == 0 {
				// 	runtime.GC()
				// 	logMem("blah")
				// 	writeHeapProfile(fmt.Sprintf("heap_%d.prof", itrctr))
				// }
				// itrctr++

			case <-stopLogging:
				return
			}
		}
	}()

	// Feed work to the workers (refill goroutine)
	var refillWg sync.WaitGroup
	refillWg.Add(1)
	var dispatched int64
	cursor := 0

	go func() {
		defer refillWg.Done()
		defer close(workChan)

		// Pre-fill the channel with numOutstandingTx transactions
		t.Logf("Pre-filling work channel with %d transactions", numOutstandingTx)
		for range numOutstandingTx {
			workChan <- workItem{index: dispatched, transfer: window[cursor]}
			atomic.AddInt64(&outstandingTxCount, 1)
			dispatched++
			cursor++
		}
		t.Logf("Pre-fill complete, %d transactions dispatched", dispatched)

		// Process completions and refill
		for notif := range completionCh {
			atomic.AddInt64(&outstandingTxCount, -1)

			// T4: notification received time
			t4 := time.Now()

			// Get T1 (test submission time)
			latencyMu.Lock()
			t1, existsT1 := submissionTimes[notif.EthTxHash]
			if existsT1 {
				delete(submissionTimes, notif.EthTxHash)
			}
			latencyMu.Unlock()

			// Get T2 (dequeue/processing start time)
			gwcore.ProcessingStartTimestampsMu.Lock()
			t2, existsT2 := gwcore.ProcessingStartTimestamps[notif.EthTxHash]
			if existsT2 {
				delete(gwcore.ProcessingStartTimestamps, notif.EthTxHash)
			}
			gwcore.ProcessingStartTimestampsMu.Unlock()

			// Get T3 (batch submitter time)
			gwcore.SubmissionTimestampsMu.Lock()
			t3, existsT3 := gwcore.SubmissionTimestamps[notif.EthTxHash]
			if existsT3 {
				delete(gwcore.SubmissionTimestamps, notif.EthTxHash)
			}
			gwcore.SubmissionTimestampsMu.Unlock()

			// Calculate and record latencies if we have all timestamps
			if metrics != nil && existsT1 && existsT2 && existsT3 {
				totalLatency := t4.Sub(t1)      // T4 - T1: total end-to-end latency
				queueLatency := t2.Sub(t1)      // T2 - T1: queueing time
				processingLatency := t3.Sub(t2) // T3 - T2: processing time by the app
				backendLatency := t4.Sub(t3)    // T4 - T3: processing time by the backend
				metrics.RecordLatencies(totalLatency, queueLatency, processingLatency, backendLatency)
			}

			// Update success/fail counts
			if notif.Status == committerpb.Status_COMMITTED {
				atomic.AddInt64(&successCount, 1)
				if metrics != nil {
					metrics.RecordTransactionCommitted()
				}
			} else {
				atomic.AddInt64(&failCount, 1)
				t.Logf("Transaction %s failed with status: %v", notif.EthTxHash.Hex(), notif.Status)
				if metrics != nil {
					metrics.RecordTransactionAborted()
				}
			}

			// Check if we should dispatch more work
			if cfg.wrapAround {
				if dispatched >= cfg.totalDispatches {
					// Check if all outstanding transactions are done
					if atomic.LoadInt64(&outstandingTxCount) == 0 {
						t.Logf("All transactions completed, closing work channel")
						return
					}
					continue
				}
			} else {
				if cursor >= len(window) {
					// Check if all outstanding transactions are done
					if atomic.LoadInt64(&outstandingTxCount) == 0 {
						t.Logf("All transactions completed, closing work channel")
						return
					}
					continue
				}
			}

			// Add next transaction to the channel
			workChan <- workItem{index: dispatched, transfer: window[cursor]}
			atomic.AddInt64(&outstandingTxCount, 1)
			dispatched++
			cursor++

			// Handle wrap-around
			if cursor >= len(window) {
				if cfg.wrapAround {
					cursor = 0
					// BalancePrimingWrapper.GetNonce() handles nonce validation bypass automatically,
					// so no explicit nonce priming is needed between wrap-around passes.
					t.Logf("Wrap-around: restarting from beginning (dispatched %d so far)", dispatched)
				}
			}
		}
	}()

	// Wait for all workers to finish processing
	wg.Wait()

	// Stop the tracker before closing the channel — the notification streaming
	// goroutine (started by the test harness) outlives this function and would
	// otherwise panic by sending on a closed channel.
	tracker.Stop()

	// Close completion channel to signal refill goroutine
	close(completionCh)

	// Wait for refill goroutine to finish
	refillWg.Wait()

	// Stop the logging goroutine
	close(stopLogging)
	loggingWg.Wait()

	// Final counts
	finalSuccess := atomic.LoadInt64(&successCount)
	finalFail := atomic.LoadInt64(&failCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	t.Logf("Replay complete: %d successful, %d failed, %d skipped out of %d total transfers",
		finalSuccess, finalFail, finalSkipped, dispatched)

	// Calculate overall throughput
	totalElapsed := time.Since(startTime).Seconds()
	overallThroughput := float64(finalSuccess+finalFail) / totalElapsed

	// Return metrics (throughput, failed count, total dispatched transfers)
	return overallThroughput, finalFail, dispatched
}

// TestReplayJSONDataset loads the USDC_dataset.json.gz file with pre-generated transactions
// and replays them with batched priming of sender balances.
func TestReplayJSONDataset(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// flogging.ActivateSpec("gateway.core.txqueue_v2=debug")

	// Use flag values for worker configuration
	processingWorkerCount := *workers    // Number of gateway workers processing transactions
	submittingWorkerCount := *submitters // Number of goroutines submitting transactions TO the gateway
	ordererSubmitterCount := *orderers   // Number of goroutines submitting transactions TO the orderer (BatchSubmitter workers)
	numOutstandingTx := *outstanding     // Maximum number of outstanding transactions

	_, _, _ = runReplayTest(t, processingWorkerCount, submittingWorkerCount, ordererSubmitterCount, numOutstandingTx, replayConfig{windowSize: 1000000}, *gatewayConfig)
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
	ordererSubmitterCounts := []int{16} // Default orderer submitter count

	// Store results
	var results []performanceResult

	t.Logf("Starting performance test with varying worker counts...")

	// Run tests with different worker configurations
	for _, processingWorkers := range processingWorkerCounts {
		for _, submittingWorkers := range submittingWorkerCounts {
			for _, ordererSubmitters := range ordererSubmitterCounts {
				t.Logf("\n=== Testing with processingWorkers=%d, submittingWorkers=%d, ordererSubmitters=%d ===",
					processingWorkers, submittingWorkers, ordererSubmitters)

				throughput, failedTxs, totalTxs := runReplayTest(t, processingWorkers, submittingWorkers, ordererSubmitters, 100, loadReplayConfigFromEnv(t), *gatewayConfig)
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
			}
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
