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
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/endorser"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/testimpl"
	gwcore "github.com/hyperledger/fabric-x-evm/gateway/core"
	gwtestimpl "github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	"github.com/hyperledger/fabric-x-evm/integration"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
)

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

// runReplayTest executes the replay test with configurable worker counts and returns metrics.
// Returns: (goodput, failedTransactionCount, invalidatedTransactionCount, totalTransactionCount)
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int, cfg replayConfig) (float64, int64, int64, int64) {
	// Silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

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
	th, err := integration.NewLocalTestHarnessWithFactory(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", "fabric", map[string]any{"Gateway.WorkerCount": processingWorkerCount}, factory)
	assert.NoError(t, err)

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
	var successCount, invalidatedCount, failCount, skippedCount int64

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

	// Start worker goroutines
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Create native eth client for read operations (TransactionByHash, etc.)
			ec, err := integration.NewNativeEthClient(th.Gateways[0])
			assert.NoError(t, err)

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

				// Send the transaction and wait for it to be committed
				func() {
					outcome := "success"

					defer func() {
						if r := recover(); r != nil {
							atomic.AddInt64(&failCount, 1)
							return
						}
						switch outcome {
						case "invalidated":
							atomic.AddInt64(&invalidatedCount, 1)
						case "failed":
							atomic.AddInt64(&failCount, 1)
						default:
							atomic.AddInt64(&successCount, 1)
						}
					}()

					// Use the wrapped gateway directly to bypass nonce validation
					err = wrappedGateway.SendTransaction(context.Background(), tx)
					if err != nil {
						t.Logf("Transfer %d: SendTransaction error: %v", i, err)
						panic(err) // Trigger the defer recovery
					}

					// Wait for transaction to be committed
					ctr := 0
					for pending := true; pending && ctr < 100; ctr++ {
						_, pending, err = ec.TransactionByHash(t.Context(), tx.Hash())
						if err != nil {
							if !strings.Contains(err.Error(), "not found") {
								t.Logf("Transfer %d: TransactionByHash error: %v", i, err)
								panic(err)
							} else {
								pending = true
							}
						}

						if pending {
							time.Sleep(time.Millisecond)
						}
					}

					// Check receipt status to distinguish committed vs invalidated transactions.
					// Status == 0 means the transaction was invalidated at commit time (MVCC conflict
					// or other Fabric-level rejection). Status == 1 means successful commit.
					// Note: there is a brief window after non-pending where the receipt row may not
					// yet be in storage. Retry up to 10 times with 1ms sleep.
					var receipt *types.Receipt
					for attempt := 0; attempt < 10; attempt++ {
						receipt, err = ec.TransactionReceipt(context.Background(), tx.Hash())
						if err == nil && receipt != nil {
							break
						}
						time.Sleep(time.Millisecond)
					}

					if receipt == nil {
						t.Logf("Transfer %d: receipt not available after finality", i)
						outcome = "failed"
						return
					}

					if receipt.Status == 0 {
						outcome = "invalidated"
						return
					}
				}()
			}
		}(w)
	}

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
				currentInvalidated := atomic.LoadInt64(&invalidatedCount)
				currentFail := atomic.LoadInt64(&failCount)
				currentSkipped := atomic.LoadInt64(&skippedCount)
				currentTotal := currentSuccess + currentInvalidated + currentFail

				txProcessed := currentTotal - lastLogCount
				throughput := float64(txProcessed) / elapsed

				totalElapsed := now.Sub(startTime).Seconds()
				overallThroughput := float64(currentSuccess) / totalElapsed

				progressTarget := int64(len(window))
				if cfg.wrapAround {
					progressTarget = cfg.totalDispatches
				}

				t.Logf("Progress: %d/%d transfers processed (%d committed, %d invalidated, %d failed, %d skipped) | Throughput: %.2f tx/s (recent), %.2f tx/s (goodput)",
					currentSuccess+currentInvalidated+currentFail+currentSkipped, progressTarget,
					currentSuccess, currentInvalidated, currentFail, currentSkipped,
					throughput, overallThroughput)

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

	// Close the work channel and wait for all workers to finish
	close(workChan)
	wg.Wait()

	// Stop the logging goroutine
	close(stopLogging)
	loggingWg.Wait()

	// Final counts
	finalSuccess := atomic.LoadInt64(&successCount)
	finalInvalidated := atomic.LoadInt64(&invalidatedCount)
	finalFail := atomic.LoadInt64(&failCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	t.Logf("──────────────────────────────────────────────────────")
	t.Logf("  RESULT: %d committed, %d invalidated, %d failed, %d skipped / %d dispatched",
		finalSuccess, finalInvalidated, finalFail, finalSkipped, dispatched)

	totalElapsed := time.Since(startTime).Seconds()
	goodput := float64(finalSuccess) / totalElapsed
	submissionThroughput := float64(finalSuccess+finalInvalidated+finalFail) / totalElapsed

	t.Logf("  GOODPUT:    %.2f tx/s committed", goodput)
	t.Logf("  SUBMITTED:  %.2f tx/s attempted", submissionThroughput)
	t.Logf("──────────────────────────────────────────────────────")

	// Return metrics (goodput, failed count, invalidated count, total dispatched transfers)
	return goodput, finalFail, finalInvalidated, dispatched
}

// TestReplayJSONDataset loads the USDC_dataset.json.gz file with pre-generated transactions
// and replays them with batched priming of sender balances.
func TestReplayJSONDataset(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Run the test with single worker configuration
	_, _, _, _ = runReplayTest(t, 1, 1, loadReplayConfigFromEnv(t))
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

			goodput, failedTxs, invalidatedTxs, totalTxs := runReplayTest(t, processingWorkers, submittingWorkers, loadReplayConfigFromEnv(t))
			failureRate := float64(failedTxs+invalidatedTxs) / float64(totalTxs)

			results = append(results, performanceResult{
				processingWorkers:       processingWorkers,
				submittingWorkers:       submittingWorkers,
				throughput:              goodput,
				invalidatedTransactions: invalidatedTxs,
				failedTransactions:      failedTxs,
				totalTransactions:       totalTxs,
				failureRate:             failureRate,
			})

			t.Logf("Result: Goodput=%.2f tx/s, Invalidated=%d, Failed=%d, Failure Rate=%.4f\n",
				goodput, invalidatedTxs, failedTxs, failureRate)
		}
	}

	// Print results table
	t.Logf("\n\n")
	t.Logf("████████████████████████████████████████████████████████████████████████████████")
	t.Logf("█                        PERFORMANCE TEST RESULTS                              █")
	t.Logf("████████████████████████████████████████████████████████████████████████████████")
	t.Logf("%-20s | %-20s | %-20s | %-15s | %-15s | %-15s",
		"Processing Workers", "Submitting Workers", "Goodput (tx/s)", "Invalidated", "Failed Txs", "Failure Rate")
	t.Logf("----------------------------------------------------------------------------------------------------------")

	for _, r := range results {
		t.Logf("%-20d | %-20d | %-20.2f | %-15d | %-15d | %-15.4f",
			r.processingWorkers, r.submittingWorkers, r.throughput, r.invalidatedTransactions, r.failedTransactions, r.failureRate)
	}
	t.Logf("████████████████████████████████████████████████████████████████████████████████")
	t.Logf("\n\n")

	// Find best configuration
	var bestResult performanceResult
	bestThroughput := 0.0
	for _, r := range results {
		if r.throughput > bestThroughput {
			bestThroughput = r.throughput
			bestResult = r
		} else if r.throughput >= bestThroughput*0.95 {
			bestInvalidationRate := float64(bestResult.invalidatedTransactions) / float64(bestResult.totalTransactions)
			thisInvalidationRate := float64(r.invalidatedTransactions) / float64(r.totalTransactions)
			if thisInvalidationRate < bestInvalidationRate {
				bestResult = r
			}
		}
	}

	t.Logf("\nBest Configuration:")
	t.Logf("  Processing Workers: %d", bestResult.processingWorkers)
	t.Logf("  Submitting Workers: %d", bestResult.submittingWorkers)
	t.Logf("  Goodput: %.2f tx/s", bestResult.throughput)
	t.Logf("  Invalidated Transactions: %d", bestResult.invalidatedTransactions)
	t.Logf("  Failed Transactions: %d", bestResult.failedTransactions)
	t.Logf("  Failure Rate: %.4f", bestResult.failureRate)

	// Export results to CSV for plotting
	csvPath := "./performance_results.csv"
	err := exportResultsToCSV(csvPath, results)
	if err != nil {
		t.Logf("Warning: Failed to export results to CSV: %v", err)
	} else {
		t.Logf("\nResults exported to: %s", csvPath)
		t.Logf("Run 'python3 integration/perf/plot_performance.py' to generate 3D plots")
	}
}

// performanceResult stores the results of a single performance test run
type performanceResult struct {
	processingWorkers       int
	submittingWorkers       int
	throughput              float64
	invalidatedTransactions int64
	failedTransactions      int64
	totalTransactions       int64
	failureRate             float64
}

// exportResultsToCSV writes the performance results to a CSV file
func exportResultsToCSV(path string, results []performanceResult) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"ProcessingWorkers", "SubmittingWorkers", "Goodput", "InvalidatedTransactions", "FailedTransactions", "TotalTransactions", "FailureRate"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write data rows
	for _, r := range results {
		row := []string{
			fmt.Sprintf("%d", r.processingWorkers),
			fmt.Sprintf("%d", r.submittingWorkers),
			fmt.Sprintf("%.2f", r.throughput),
			fmt.Sprintf("%d", r.invalidatedTransactions),
			fmt.Sprintf("%d", r.failedTransactions),
			fmt.Sprintf("%d", r.totalTransactions),
			fmt.Sprintf("%.6f", r.failureRate),
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	return nil
}
