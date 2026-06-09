package txmonitor

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Step represents a specific location in the transaction processing pipeline
type Step int

const (
	StepTestStart                  Step = iota // STEP 0: Test starts sending transaction
	StepSendRawTransaction                     // STEP 1: Gateway receives raw transaction
	StepSendTransaction                        // STEP 2: SendTransaction called
	StepEnqueueTransaction                     // STEP 3: Transaction enqueued for processing
	StepTxQueueEnqueue                         // STEP 4: TxQueueV2.Enqueue called
	StepTxQueueEnqueueEnd                      // STEP 5: TxQueueV2.Enqueue completed
	StepWorkerDequeue                          // STEP 6: Worker dequeues transaction
	StepProcessTxStart                         // STEP 7: Start processing transaction
	StepExecuteTransactionStart                // STEP 8: ExecuteTransaction starts
	StepBeforeEndorserCall                     // STEP 9: Before calling endorser API
	StepProcessEVMTransactionStart             // STEP 10: ProcessEVMTransaction starts
	StepAfterSenderExtract                     // STEP 11: After extracting sender
	StepPrepareMessageStart                    // STEP 12: PrepareMessage starts
	StepAfterNonceValidation                   // STEP 13: After nonce validation
	StepBeforeTransactionToMessage             // STEP 14: Before converting to message
	StepApplyMessageStart                      // STEP 15: ApplyMessage starts
	StepApplyMessageCalled                     // STEP 16: ApplyMessage called
	StepExecutorEnd                            // STEP 17: Executor ends
	StepBeforeEndorse                          // STEP 18: Before endorsement building
	StepAfterEndorserCall                      // STEP 19: After endorser call completes
	StepAfterExecuteEthTx                      // STEP 20: After ExecuteEthTx completes
	StepBeforeSubmitFabricTx                   // STEP 21: Before SubmitFabricTx
	StepSubmitCalled                           // STEP 22: Submit called
	StepAfterSubmitFabricTx                    // STEP 23: After SubmitFabricTx completes
	StepBeforeUnmarshalTx                      // STEP 24: Before unmarshaling transaction
	StepTransactionCommitted                   // STEP 25: Transaction committed
)

// String returns the human-readable name of the step
func (s Step) String() string {
	names := map[Step]string{
		StepTestStart:                  "TestStart",
		StepSendRawTransaction:         "SendRawTransaction",
		StepSendTransaction:            "SendTransaction",
		StepEnqueueTransaction:         "EnqueueTransaction",
		StepTxQueueEnqueue:             "TxQueueEnqueue",
		StepTxQueueEnqueueEnd:          "TxQueueEnqueueEnd",
		StepWorkerDequeue:              "WorkerDequeue",
		StepProcessTxStart:             "ProcessTxStart",
		StepExecuteTransactionStart:    "ExecuteTransactionStart",
		StepBeforeEndorserCall:         "BeforeEndorserCall",
		StepProcessEVMTransactionStart: "ProcessEVMTransactionStart",
		StepAfterSenderExtract:         "AfterSenderExtract",
		StepPrepareMessageStart:        "PrepareMessageStart",
		StepAfterNonceValidation:       "AfterNonceValidation",
		StepBeforeTransactionToMessage: "BeforeTransactionToMessage",
		StepApplyMessageStart:          "ApplyMessageStart",
		StepApplyMessageCalled:         "ApplyMessageCalled",
		StepExecutorEnd:                "ExecutorEnd",
		StepBeforeEndorse:              "BeforeEndorse",
		StepAfterEndorserCall:          "AfterEndorserCall",
		StepAfterExecuteEthTx:          "AfterExecuteEthTx",
		StepBeforeSubmitFabricTx:       "BeforeSubmitFabricTx",
		StepSubmitCalled:               "SubmitCalled",
		StepAfterSubmitFabricTx:        "AfterSubmitFabricTx",
		StepBeforeUnmarshalTx:          "BeforeUnmarshalTx",
		StepTransactionCommitted:       "TransactionCommitted",
	}
	if name, ok := names[s]; ok {
		return name
	}
	return fmt.Sprintf("UnknownStep(%d)", s)
}

// Timestamp represents a single measurement point
type Timestamp struct {
	Step Step      `json:"step"`
	Name string    `json:"name"`
	Time time.Time `json:"time"`
	Nano int64     `json:"nano"` // Nanoseconds since epoch for precision
}

// TxTrace contains all timestamps for a single transaction
type TxTrace struct {
	TxHash     common.Hash   `json:"tx_hash"`
	Timestamps [26]Timestamp `json:"timestamps"` // Fixed-size array for lock-free writes (0-25)
}

// Monitor is a lightweight in-memory monitoring system for transaction processing
type Monitor struct {
	mu     sync.RWMutex
	traces map[common.Hash]*TxTrace
}

// Global monitor instance
var globalMonitor = NewMonitor()

// NewMonitor creates a new monitoring instance
func NewMonitor() *Monitor {
	return &Monitor{
		traces: make(map[common.Hash]*TxTrace),
	}
}

// Record records a timestamp for a transaction at a specific step
// This is lock-free for writes after the trace is created, using the step as array index
func (m *Monitor) Record(txHash common.Hash, step Step) {
	now := time.Now()

	// Fast path: try to find existing trace with read lock
	m.mu.RLock()
	trace, exists := m.traces[txHash]
	m.mu.RUnlock()

	if !exists {
		// Slow path: acquire write lock to create new trace
		m.mu.Lock()
		// Double-check after acquiring write lock (another goroutine might have created it)
		trace, exists = m.traces[txHash]
		if !exists {
			trace = &TxTrace{
				TxHash: txHash,
				// Timestamps array is zero-initialized
			}
			m.traces[txHash] = trace
		}
		m.mu.Unlock()
	}

	// Lock-free write: use step number as index (step is 0-based, array is 0-based)
	if step >= 0 && step <= 25 {
		trace.Timestamps[step] = Timestamp{
			Step: step,
			Name: step.String(),
			Time: now,
			Nano: now.UnixNano(),
		}
	}
}

// GetTrace retrieves the trace for a specific transaction
func (m *Monitor) GetTrace(txHash common.Hash) (*TxTrace, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trace, exists := m.traces[txHash]
	return trace, exists
}

// Clear removes all traces from memory
func (m *Monitor) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.traces = make(map[common.Hash]*TxTrace)
}

// DumpJSON writes all traces to a JSON file
func (m *Monitor) DumpJSON(filename string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	// Convert map to slice for consistent output
	traces := make([]*TxTrace, 0, len(m.traces))
	for _, trace := range m.traces {
		traces = append(traces, trace)
	}

	if err := encoder.Encode(traces); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	return nil
}

// DumpCSV writes all traces to a CSV file suitable for plotting
// Format: tx_hash,step_number,step_name,timestamp_nano,elapsed_from_start_ms
func (m *Monitor) DumpCSV(filename string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	// Write CSV header
	fmt.Fprintf(file, "tx_hash,step_number,step_name,timestamp_nano,elapsed_from_start_ms\n")

	// Write data for each transaction
	for _, trace := range m.traces {
		// Find first non-zero timestamp to use as start time
		var startTime int64
		for _, ts := range trace.Timestamps {
			if ts.Nano != 0 {
				startTime = ts.Nano
				break
			}
		}

		if startTime == 0 {
			continue // No valid timestamps for this transaction
		}

		// Write all non-zero timestamps
		for _, ts := range trace.Timestamps {
			if ts.Nano == 0 {
				continue // Skip unrecorded steps
			}
			elapsedMs := float64(ts.Nano-startTime) / 1e6
			fmt.Fprintf(file, "%s,%d,%s,%d,%.3f\n",
				trace.TxHash.Hex(),
				ts.Step,
				ts.Name,
				ts.Nano,
				elapsedMs,
			)
		}
	}

	return nil
}

// GetStats returns statistics about the monitoring data
func (m *Monitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Each TxTrace has: 32 bytes (hash) + 26 * ~48 bytes (timestamp struct) ≈ 1.3KB per tx
	return map[string]interface{}{
		"total_transactions": len(m.traces),
		"memory_estimate_kb": len(m.traces) * 1280 / 1024,
	}
}

// Global functions for convenience

// Record records a timestamp for a transaction at a specific step using the global monitor
func Record(txHash common.Hash, step Step) {
	globalMonitor.Record(txHash, step)
}

// DumpJSON writes all traces to a JSON file using the global monitor
func DumpJSON(filename string) error {
	return globalMonitor.DumpJSON(filename)
}

// DumpCSV writes all traces to a CSV file using the global monitor
func DumpCSV(filename string) error {
	return globalMonitor.DumpCSV(filename)
}

// Clear removes all traces from the global monitor
func Clear() {
	globalMonitor.Clear()
}

// GetStats returns statistics from the global monitor
func GetStats() map[string]interface{} {
	return globalMonitor.GetStats()
}

// Made with Bob
