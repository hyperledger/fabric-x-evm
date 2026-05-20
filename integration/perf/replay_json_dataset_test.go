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

// runReplayTest executes the replay test with configurable worker counts and returns metrics.
// Returns: (overallThroughput, failedTransactionCount, totalTransactionCount)
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int) (float64, int64, int64) {
	// Silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	// USDC contract address
	USDC_addr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")

	// Configure balance priming for USDC transfers
	balancePriming := &testimpl.BalancePrimingConfig{
		Enabled:         true,
		ContractAddress: USDC_addr,
		MappingPosition: 9, // USDC balance mapping is at slot 9
	}

	evmConfig := endorser.EVMConfig{}

	// Setup test harness with USDC contract and balance priming enabled
	// Use the factory pattern to create endorsers with balance priming
	factory := balancePrimingEndorserFactory(balancePriming)
	th, err := integration.NewLocalTestHarnessWithFactory(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", "fabric", map[string]any{"Gateway.WorkerCount": processingWorkerCount}, factory)
	assert.NoError(t, err)

	// Load the JSON dataset
	datasetPath := "testdata/USDC_dataset.json.gz"
	t.Logf("Loading dataset from %s", datasetPath)

	file, err := os.Open(datasetPath)
	assert.NoError(t, err)
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	assert.NoError(t, err)
	defer gzReader.Close()

	var transfers []utils.TokenTransfer
	decoder := json.NewDecoder(gzReader)
	err = decoder.Decode(&transfers)
	assert.NoError(t, err)
	assert.NotEmpty(t, transfers, "dataset should contain transfers")

	t.Logf("Loaded %d transfers from dataset", len(transfers))

	////////////////////////////////////////////////
	////////////////////////////////////////////////
	////////////////////////////////////////////////
	transfers = transfers[:3000]
	////////////////////////////////////////////////
	////////////////////////////////////////////////
	////////////////////////////////////////////////

	// Replay transactions with parallel workers
	// Atomic counters for thread-safe counting
	var successCount, failCount, skippedCount int64

	runtime.GC()

	// Track throughput
	startTime := time.Now()
	var lastLogTime atomic.Value
	lastLogTime.Store(startTime)
	var lastLogCount int64

	// Create a channel for work items
	type workItem struct {
		index    int
		transfer utils.TokenTransfer
	}
	workChan := make(chan workItem, 100) // Buffer to avoid blocking

	// Worker pool configuration
	numWorkers := submittingWorkerCount
	var wg sync.WaitGroup

	// Start worker goroutines
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Create native eth client for sending transactions
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
					defer func() {
						if r := recover(); r != nil {
							// t.Logf("Transfer %d: Failed to send transaction (panic recovered): %v", i, r)
							atomic.AddInt64(&failCount, 1)
						} else {
							atomic.AddInt64(&successCount, 1)
						}
					}()

					err = ec.SendTransaction(context.Background(), tx)
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
				currentFail := atomic.LoadInt64(&failCount)
				currentSkipped := atomic.LoadInt64(&skippedCount)
				currentTotal := currentSuccess + currentFail

				txProcessed := currentTotal - lastLogCount
				throughput := float64(txProcessed) / elapsed

				totalElapsed := now.Sub(startTime).Seconds()
				overallThroughput := float64(currentTotal) / totalElapsed

				t.Logf("Progress: %d/%d transfers processed (%d successful, %d failed, %d skipped) | Throughput: %.2f tx/s (recent), %.2f tx/s (overall)",
					currentSuccess+currentFail+currentSkipped, len(transfers),
					currentSuccess, currentFail, currentSkipped,
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
	for i, transfer := range transfers {
		workChan <- workItem{index: i, transfer: transfer}
	}

	// Close the work channel and wait for all workers to finish
	close(workChan)
	wg.Wait()

	// Stop the logging goroutine
	close(stopLogging)
	loggingWg.Wait()

	// Final counts
	finalSuccess := atomic.LoadInt64(&successCount)
	finalFail := atomic.LoadInt64(&failCount)
	finalSkipped := atomic.LoadInt64(&skippedCount)

	t.Logf("Replay complete: %d successful, %d failed, %d skipped out of %d total transfers",
		finalSuccess, finalFail, finalSkipped, len(transfers))

	// Calculate overall throughput
	totalElapsed := time.Since(startTime).Seconds()
	overallThroughput := float64(finalSuccess+finalFail) / totalElapsed

	// Return metrics (throughput, failed count, total attempted transactions)
	totalAttempted := finalSuccess + finalFail + finalSkipped
	return overallThroughput, finalFail, totalAttempted
}

// TestReplayJSONDataset loads the USDC_dataset.json.gz file with pre-generated transactions
// and replays them with batched priming of sender balances.
func TestReplayJSONDataset(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Run the test with single worker configuration
	_, _, _ = runReplayTest(t, 1, 1)
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

			throughput, failedTxs, totalTxs := runReplayTest(t, processingWorkers, submittingWorkers)
			failureRate := float64(failedTxs) / float64(totalTxs)

			results = append(results, performanceResult{
				processingWorkers:  processingWorkers,
				submittingWorkers:  submittingWorkers,
				throughput:         throughput,
				failedTransactions: failedTxs,
				totalTransactions:  totalTxs,
				failureRate:        failureRate,
			})

			t.Logf("Result: Throughput=%.2f tx/s, Failed=%d, Failure Rate=%.4f\n",
				throughput, failedTxs, failureRate)
		}
	}

	// Print results table
	t.Logf("\n\n================================================================================")
	t.Logf("PERFORMANCE TEST RESULTS")
	t.Logf("================================================================================")
	t.Logf("%-20s | %-20s | %-20s | %-15s | %-15s",
		"Processing Workers", "Submitting Workers", "Throughput (tx/s)", "Failed Txs", "Failure Rate")
	t.Logf("--------------------------------------------------------------------------------")

	for _, r := range results {
		t.Logf("%-20d | %-20d | %-20.2f | %-15d | %-15.4f",
			r.processingWorkers, r.submittingWorkers, r.throughput, r.failedTransactions, r.failureRate)
	}
	t.Logf("================================================================================")

	// Find best configuration
	var bestResult performanceResult
	bestThroughput := 0.0
	for _, r := range results {
		if r.throughput > bestThroughput {
			bestThroughput = r.throughput
			bestResult = r
		}
	}

	t.Logf("\nBest Configuration:")
	t.Logf("  Processing Workers: %d", bestResult.processingWorkers)
	t.Logf("  Submitting Workers: %d", bestResult.submittingWorkers)
	t.Logf("  Throughput: %.2f tx/s", bestResult.throughput)
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
	processingWorkers  int
	submittingWorkers  int
	throughput         float64
	failedTransactions int64
	totalTransactions  int64
	failureRate        float64
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
	header := []string{"ProcessingWorkers", "SubmittingWorkers", "Throughput", "FailedTransactions", "TotalTransactions", "FailureRate"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write data rows
	for _, r := range results {
		row := []string{
			fmt.Sprintf("%d", r.processingWorkers),
			fmt.Sprintf("%d", r.submittingWorkers),
			fmt.Sprintf("%.2f", r.throughput),
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
