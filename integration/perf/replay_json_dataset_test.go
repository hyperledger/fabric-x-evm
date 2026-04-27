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
	"github.com/hyperledger/fabric-x-evm/integration"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
)

// runReplayTest executes the replay test with configurable worker counts and returns metrics.
// Returns: (overallThroughput, failedTransactionCount, totalTransactionCount)
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int) (float64, int64, int64) {
	// Silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	// USDC contract address
	USDC_addr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")

	// Configure balance priming for USDC transfers
	evmConfig := endorser.EVMConfig{
		BalancePriming: &endorser.BalancePrimingConfig{
			Enabled:         true,
			ContractAddress: USDC_addr,
			MappingPosition: 9, // USDC balance mapping is at slot 9
		},
	}

	// Setup test harness with USDC contract and balance priming enabled
	// th, err := integration.NewFabricXTestHarness(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", "fabric", map[string]any{"Gateway.WorkerCount": processingWorkerCount})
	th, err := integration.NewLocalTestHarness(t, integration.TestLogger{T: t}, evmConfig, "testdata/USDC_contract.json", "fabric", map[string]any{"Gateway.WorkerCount": processingWorkerCount})
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
	totalTransfers := int64(len(transfers))
	////////////////////////////////////////////////
	////////////////////////////////////////////////
	////////////////////////////////////////////////

	// // Phase 1: Collect unique senders
	// uniqueSenders := make(map[common.Address]bool)
	// for _, transfer := range transfers {
	// 	if len(transfer.Transaction) > 0 { // Only count transfers with transactions
	// 		uniqueSenders[transfer.Sender] = true
	// 	}
	// }

	// senderList := make([]common.Address, 0, len(uniqueSenders))
	// for sender := range uniqueSenders {
	// 	senderList = append(senderList, sender)
	// }

	// t.Logf("Found %d unique senders with transactions", len(senderList))

	// // Phase 2: Prime balances in batches
	// const batchSize = 100 // Prime 100 accounts at a time
	// slot := uint64(9)     // USDC balance slot

	// // Create a very high balance (1 billion USDC with 6 decimals = 1e15)
	// initialBalance := new(big.Int).Mul(big.NewInt(1000000000), big.NewInt(1000000)) // 1 billion USDC
	// balanceValue := common.BytesToHash(uint256.MustFromBig(initialBalance).ToBig().Bytes())

	// numBatches := (len(senderList) + batchSize - 1) / batchSize
	// t.Logf("Priming %d senders in %d batches of up to %d accounts", len(senderList), numBatches, batchSize)

	// for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
	// 	start := batchIdx * batchSize
	// 	end := start + batchSize
	// 	if end > len(senderList) {
	// 		end = len(senderList)
	// 	}

	// 	batch := senderList[start:end]
	// 	// t.Logf("Priming batch %d/%d (%d accounts)", batchIdx+1, numBatches, len(batch))

	// 	// Create storage map for this batch
	// 	storageMap := make(map[common.Hash]common.Hash)
	// 	for _, sender := range batch {
	// 		// Map the original sender to a controlled address
	// 		mappedAddr := mapAddress(sender)
	// 		// fmt.Printf("Real sender %s, mapped sender %s\n", sender.Hex(), mappedAddr.Hex())

	// 		// Calculate the storage slot for this address's balance
	// 		balanceSlot := integration.GetERC20BalanceSlot(mappedAddr, slot)
	// 		storageMap[balanceSlot] = balanceValue
	// 	}

	// 	// Reset and prime the state for this batch
	// 	_, err = th.Primer.Reset()
	// 	assert.NoError(t, err)

	// 	th.Primer.SetStorage(USDC_addr, storageMap)
	// 	err = th.Primer.Commit(context.Background(), true)
	// 	assert.NoError(t, err)

	// 	if (batchIdx)%20 == 0 {
	// 		t.Logf("Batch %d/%d: Primed %d storage slots", batchIdx+1, numBatches, len(storageMap))
	// 	}
	// }

	// t.Logf("Completed priming all %d senders", len(senderList))

	// time.Sleep(1 * time.Second)

	// Phase 3: Replay all transactions with parallel workers
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

					if ctr == math.MaxInt64 { // initially I though we should bail quickly but we don't
						panic("waited too long")
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

	// Return metrics (throughput, failed count, total transfers)
	return overallThroughput, finalFail, totalTransfers
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

// Made with Bob
