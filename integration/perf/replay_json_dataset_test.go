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
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/grpclog"
)

// TxCompletionTracker tracks transaction completions for open-loop testing.
// It implements gwcore.TxHandler to receive notifications from the notification system
// and updates the test's atomic counters directly.
type TxCompletionTracker struct {
	successCount *int64
	failCount    *int64
}

// NewTxCompletionTracker creates a new tracker that updates the provided counters.
func NewTxCompletionTracker(successCount, failCount *int64) *TxCompletionTracker {
	return &TxCompletionTracker{
		successCount: successCount,
		failCount:    failCount,
	}
}

// HandleTx implements gwcore.TxHandler. It receives notifications about completed transactions
// and updates the success/fail counters based on transaction status.
func (t *TxCompletionTracker) HandleTx(ctx context.Context, notifs []gwcore.TxNotification) error {
	for _, notif := range notifs {
		// Update counters based on status
		if notif.Status == committerpb.Status_COMMITTED { // 0 = COMMITTED in fabric-x
			atomic.AddInt64(t.successCount, 1)
		} else {
			atomic.AddInt64(t.failCount, 1)
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
	t.Logf("Config: processingWorkers=%d submittingWorkers=%d", processingWorkerCount, submittingWorkerCount)

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

	// Replay transactions with parallel workers
	// Atomic counters for thread-safe counting
	var successCount, failCount, skippedCount int64

	// Create completion tracker that updates our counters
	tracker := NewTxCompletionTracker(&successCount, &failCount)

	// Use notification-based harness from PR #190 for fabric-x.
	th, err := integration.NewFabricXTestHarnessWithNotifications(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", map[string]any{"Gateway.WorkerCount": processingWorkerCount}, factory, gwcore.NewTxQueueV2(), tracker)
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

	// Start worker goroutines (open-loop: submit without waiting for completion)
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for item := range workChan {
				i := item.index
				transfer := item.transfer

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

				// Send the transaction (open-loop: don't wait for completion)
				err = wrappedGateway.SendTransaction(context.Background(), tx)
				if err != nil {
					t.Logf("Transfer %d: SendTransaction error: %v", i, err)
					atomic.AddInt64(&failCount, 1)
					continue
				}

				// Small sleep between submissions for open-loop pacing
				time.Sleep(time.Millisecond)
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
				goodput := float64(currentSuccess-lastLogCount+currentFail-lastLogCount) / elapsed
				if lastLogCount == 0 {
					goodput = float64(currentSuccess) / elapsed
				}

				totalElapsed := now.Sub(startTime).Seconds()
				overallThroughput := float64(currentTotal) / totalElapsed
				overallGoodput := float64(currentSuccess) / totalElapsed

				progressTarget := int64(len(window))
				if cfg.wrapAround {
					progressTarget = cfg.totalDispatches
				}

				t.Logf("Progress: %d/%d completed (%d successful, %d failed, %d skipped) | Throughput: %.2f tx/s, Goodput: %.2f tx/s (recent) | Overall: %.2f tx/s, %.2f tx/s",
					currentSuccess+currentFail+currentSkipped, progressTarget,
					currentSuccess, currentFail, currentSkipped,
					throughput, goodput,
					overallThroughput, overallGoodput)

				// Update for next interval
				lastLogTime.Store(now)
				lastLogCount = currentTotal

			case <-stopLogging:
				return
			}
		}
	}()

	// Feed work to the workers
	var dispatched int64
	cursor := 0

	for {
		if cfg.wrapAround {
			if dispatched >= cfg.totalDispatches {
				break
			}
		} else {
			if cursor >= len(window) {
				break
			}
		}

		workChan <- workItem{index: dispatched, transfer: window[cursor]}
		dispatched++
		cursor++

		if cursor >= len(window) {
			if cfg.wrapAround {
				cursor = 0
				// BalancePrimingWrapper.GetNonce() handles nonce validation bypass automatically,
				// so no explicit nonce priming is needed between wrap-around passes.
				t.Logf("Wrap-around: restarting from beginning (dispatched %d so far)", dispatched)
			} else {
				break
			}
		}
	}

	// Close the work channel and wait for all workers to finish submitting
	close(workChan)
	wg.Wait()

	t.Logf("All submissions complete, waiting for transaction completions...")

	// Wait for all transactions to complete (open-loop: submissions are done, now wait for notifications)
	expectedCompletions := dispatched - atomic.LoadInt64(&skippedCount)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		currentSuccess := atomic.LoadInt64(&successCount)
		currentFail := atomic.LoadInt64(&failCount)
		currentTotal := currentSuccess + currentFail

		if currentTotal >= expectedCompletions {
			t.Logf("All transactions completed: %d/%d", currentTotal, expectedCompletions)
			break
		}

		select {
		case <-ticker.C:
			t.Logf("Waiting for completions: %d/%d (%.1f%%)",
				currentTotal, expectedCompletions,
				float64(currentTotal)/float64(expectedCompletions)*100)
		case <-t.Context().Done():
			t.Logf("Context cancelled while waiting for completions: %d/%d received",
				currentTotal, expectedCompletions)
			break
		}
	}

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


	// Final counts
	finalSuccess := atomic.LoadInt64(&successCount)
	finalFail := atomic.LoadInt64(&failCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	t.Logf("Replay complete: %d successful, %d failed, %d skipped out of %d total transfers",
		finalSuccess, finalFail, finalSkipped, dispatched)

	// Calculate overall throughput and goodput
	totalElapsed := time.Since(startTime).Seconds()
	overallThroughput := float64(finalSuccess+finalFail) / totalElapsed
	overallGoodput := float64(finalSuccess) / totalElapsed

	t.Logf("Final metrics: Throughput=%.2f tx/s, Goodput=%.2f tx/s, Success rate=%.2f%%",
		overallThroughput, overallGoodput, float64(finalSuccess)/float64(finalSuccess+finalFail)*100)

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

	// Run the test with single worker configuration
	_, _, _ = runReplayTest(t, 16, 32, loadReplayConfigFromEnv(t))
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
