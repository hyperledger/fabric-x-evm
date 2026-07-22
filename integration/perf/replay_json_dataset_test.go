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
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-evm/endorser/testimpl"
	"github.com/hyperledger/fabric-x-evm/gateway/app"
	gwconfig "github.com/hyperledger/fabric-x-evm/gateway/config"
	gwcore "github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	gwtestimpl "github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	"github.com/hyperledger/fabric-x-evm/integration"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
)

// memHeightTracker is a minimal blocks.BlockHandler, network.BlockHeightReader,
// and core.Store that tracks only the latest committed block number in memory.
// It is used by perf tests as a lightweight replacement for the SQLite-backed
// core.Chain: it carries no block data and imposes no I/O overhead.
//
// The core.Store query methods (GetBlockByNumber, GetTransactionByHash, etc.) are
// no-ops that return nil — perf tests never issue RPC queries against the chain.
type memHeightTracker struct {
	height atomic.Uint64
}

// Handle implements blocks.BlockHandler — records the block number of every
// committed block so the synchronizer knows how far delivery has progressed.
func (m *memHeightTracker) Handle(_ context.Context, b blocks.Block) error {
	m.height.Store(b.Number)
	return nil
}

// BlockNumber implements network.BlockHeightReader and core.Store — returns 0
// until the first block is committed.
func (m *memHeightTracker) BlockNumber(_ context.Context) (uint64, error) {
	return m.height.Load(), nil
}

// The remaining methods implement core.Store. Perf tests never issue chain
// queries via RPC, so these are intentional no-ops.

func (m *memHeightTracker) BlockNumberByHash(_ context.Context, _ []byte) (*uint64, error) {
	return nil, nil
}
func (m *memHeightTracker) LatestBlock(_ context.Context, _ bool) (*domain.Block, error) {
	return nil, nil
}
func (m *memHeightTracker) GetBlockByNumber(_ context.Context, _ uint64, _ bool) (*domain.Block, error) {
	return nil, nil
}
func (m *memHeightTracker) GetBlockByHash(_ context.Context, _ []byte, _ bool) (*domain.Block, error) {
	return nil, nil
}
func (m *memHeightTracker) GetBlockTxCountByHash(_ context.Context, _ []byte) (int64, error) {
	return 0, nil
}
func (m *memHeightTracker) GetBlockTxCountByNumber(_ context.Context, _ uint64) (int64, error) {
	return 0, nil
}
func (m *memHeightTracker) GetTransactionByHash(_ context.Context, _ []byte) (*domain.Transaction, error) {
	return nil, nil
}
func (m *memHeightTracker) GetTransactionByBlockHashAndIndex(_ context.Context, _ []byte, _ int64) (*domain.Transaction, error) {
	return nil, nil
}
func (m *memHeightTracker) GetTransactionByBlockNumberAndIndex(_ context.Context, _ uint64, _ int64) (*domain.Transaction, error) {
	return nil, nil
}
func (m *memHeightTracker) GetLogs(_ context.Context, _ domain.LogFilter) ([]domain.Log, error) {
	return nil, nil
}
func (m *memHeightTracker) GetLogsByTxHash(_ context.Context, _ []byte) ([]domain.Log, error) {
	return nil, nil
}

// perfHandlerChain is an integration.HandlerChainFactory for perf tests.
// It wires a memHeightTracker instead of core.Chain (no persistence, no I/O),
// and appends completionTracker as the last handler so the perf test can observe
// every committed transaction.
//
// Handler order:
//
//	[endorser KVS…, memHeightTracker, gateway, completionTracker]
func perfHandlerChain(completionTracker *TxCompletionTracker) integration.HandlerChainFactory {
	return func(t *testing.T, ctx context.Context, cfg gwconfig.Config, ends []eapi.Service, gwSigner sdk.Signer, submitters []gwcore.Submitter, txQueue gwcore.TxQueueInterface, dbs []estorage.KVS) (*gwcore.Gateway, []blocks.BlockHandler, network.BlockHeightReader) {
		tracker := new(memHeightTracker)

		txPerSec := 0
		if cfg.Network.Namespace == "synthetic" {
			txPerSec = 10000
		}
		gw, err := app.BuildGateway(ctx, ends, gwSigner, cfg.Network, tracker, submitters, cfg.Gateway.SubmitterCount, cfg.Gateway.WorkerCount, txQueue, cfg.Gateway.EndorsementChanSize, txPerSec)
		if err != nil {
			t.Fatalf("build gateway: %v", err)
		}

		handlers := make([]blocks.BlockHandler, 0, len(dbs)+3)
		for _, db := range dbs {
			handlers = append(handlers, db)
		}
		handlers = append(handlers, tracker, gw, completionTracker)
		return gw, handlers, tracker
	}
}

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

// txCompletion carries the fields needed by the refill loop after a transaction commits.
type txCompletion struct {
	EthTxHash common.Hash
	Valid     bool
	Status    committerpb.Status
}

// TxCompletionTracker forwards all transaction completion notifications to a single channel.
// It implements common.BlockHandler to receive notifications from the notification system.
type TxCompletionTracker struct {
	mu           sync.Mutex
	completionCh chan txCompletion
	started      bool
	stopped      bool
}

// NewTxCompletionTracker creates a new tracker with a completion channel.
// The tracker is dormant until Start is called: blocks committed before Start
// (e.g. during catch-up or state priming) are silently ignored.
func NewTxCompletionTracker(completionCh chan txCompletion) *TxCompletionTracker {
	return &TxCompletionTracker{
		completionCh: completionCh,
	}
}

// Start opens the tracker for business. Call this once the test is ready to
// receive completions (i.e. just before dispatching the first transaction).
// Completions that arrive before Start is called are silently dropped.
func (t *TxCompletionTracker) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = true
}

// Stop prevents any further sends to the completion channel. Must be called before
// closing the channel to avoid panics from in-flight notification goroutines.
func (t *TxCompletionTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

// Handle implements common.BlockHandler. It receives committed transactions and forwards
// them to the completion channel, reconstructing the Ethereum tx hash from InputArgs.
func (t *TxCompletionTracker) Handle(ctx context.Context, b blocks.Block) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started || t.stopped {
		return nil
	}
	for _, tx := range b.Transactions {
		var ethTx types.Transaction
		if len(tx.InputArgs) < 2 {
			continue
		}
		if err := ethTx.UnmarshalBinary(tx.InputArgs[1]); err != nil {
			continue
		}
		notif := txCompletion{
			EthTxHash: ethTx.Hash(),
			Valid:     tx.Valid,
			Status:    committerpb.Status(tx.Status),
		}
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
	return func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) integration.EndorserComponents {
		// Create the base endorser components
		db, builder, baseEndorser := integration.NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)

		// Extract the base EVMEngine
		baseEngine, ok := baseEndorser.Engine.(*execution.EVMEngine)
		if !ok {
			t.Fatalf("Expected *execution.EVMEngine, got %T", baseEndorser.Engine)
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

		return integration.EndorserComponents{KVS: db, Builder: builder, Service: baseEndorser}
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
// Returns: (overallThroughput, failedTransactionCount, totalTransactionCount, invalidRate, conflictRate)
// invalidRate  = fraction of committed transactions that were invalid (MVCC / signature failures).
// conflictRate = fraction of enqueued transactions that were rejected due to conflicts.
func runReplayTest(t *testing.T, processingWorkerCount int, submittingWorkerCount int, ordererSubmitterCount int, numOutstandingTx int, cfg replayConfig, gwConfig string) (float64, int64, int64, float64, float64) {
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
	evmConfig := execution.EVMConfig{}

	// Setup test harness with USDC contract and balance priming enabled
	factory := balancePrimingEndorserFactory(balancePriming)

	// Create completion channel for transaction notifications
	completionCh := make(chan txCompletion, numOutstandingTx*2)

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
		perfHandlerChain(tracker),
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

	// Open the tracker now that we are about to dispatch real transactions.
	// Any completions that arrived during catch-up or state priming are discarded.
	tracker.Start()

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
		wg.Go(func() {

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
		})
	}

	// Progress logging goroutine
	stopLogging := make(chan struct{})
	var loggingWg sync.WaitGroup
	loggingWg.Go(func() {
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
	})

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
			if notif.Valid {
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

	// Read queue stats before t.Cleanup fires and calls gw.Stop().
	total, invalid, totalEnq, conflictEnq := th.Gateways[0].TxQueue.Stats()
	var invalidRate, conflictRate float64
	if total > 0 {
		invalidRate = float64(invalid) / float64(total)
	}
	if totalEnq > 0 {
		conflictRate = float64(conflictEnq) / float64(totalEnq)
	}

	// Return metrics (throughput, failed count, total dispatched transfers, invalidRate, conflictRate)
	return overallThroughput, finalFail, dispatched, invalidRate, conflictRate
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

	throughput, failedTxs, totalTxs, invalidRate, conflictRate := runReplayTest(t, processingWorkerCount, submittingWorkerCount, ordererSubmitterCount, numOutstandingTx, replayConfig{windowSize: 1000000}, *gatewayConfig)

	// Machine-readable summary parsed by CI to post on the PR.
	// Format: PERF RESULT throughput=<tx/s> invalid_rate=<0.NNN> conflict_rate=<0.NNN>
	t.Logf("PERF RESULT throughput=%.2f invalid_rate=%.6f conflict_rate=%.6f total=%d failed=%d",
		throughput, invalidRate, conflictRate, totalTxs, failedTxs)
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

				throughput, failedTxs, totalTxs, _, _ := runReplayTest(t, processingWorkers, submittingWorkers, ordererSubmitters, 100, loadReplayConfigFromEnv(t), *gatewayConfig)
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
