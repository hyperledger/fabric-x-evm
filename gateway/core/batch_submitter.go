/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"golang.org/x/time/rate"
)

var batchLogger = flogging.MustGetLogger("gateway.core.batch_submitter")

// SubmissionTimestamps is an optional map for tracking submission timestamps.
// If non-nil, timestamps are recorded when transactions are submitted to the orderer.
// Key: Ethereum transaction hash, Value: T2 timestamp (when submitted to orderer)
var SubmissionTimestamps map[common.Hash]time.Time

// SubmissionTimestampsMu protects access to SubmissionTimestamps
var SubmissionTimestampsMu sync.Mutex

// SetBatchSubmitterQueueSizeMetric is an optional callback for reporting the batch submitter input queue size.
// If non-nil, it will be called to report the current queue size.
var SetBatchSubmitterQueueSizeMetric func(size int)

// BatchSubmitter reads endorsements from a channel, optionally records them in the
// pending-tx cache (when cache != nil), then submits each one to the orderer.
// The cache is used by AllTxBatchDispatcher to correlate commit events with the
// originating Ethereum transaction.
// Multiple worker goroutines read from inputChan and submit in parallel.
// Each worker has its own submitter instance for better performance.
// Rate limiting is optionally applied across all workers to ensure aggregate submission rate
// does not exceed the configured limit.
type BatchSubmitter struct {
	submitters     []Submitter // One submitter per worker for parallel submission
	inputChan      chan sdk.Endorsement
	stopChan       chan struct{}
	doneChan       chan struct{}
	numWorkers     int
	rateLimiter    *rate.Limiter  // Shared rate limiter across all workers (nil if disabled)
	silenceConfig  *SilenceConfig // Optional periodic silence windows (nil if disabled)
	lastSilenceEnd atomic.Value   // time.Time - tracks when last silence window ended
}

// SilenceConfig defines periodic silence windows to create gaps for other processes.
// Example: interval=1s, duration=10ms means pause for 10ms every second.
type SilenceConfig struct {
	Interval time.Duration // How often to pause (e.g., 1 second)
	Duration time.Duration // How long to pause (e.g., 10 milliseconds)
}

const DefaultNumWorkers = 16

// MaxSubmissionsPerSecond is the maximum aggregate submission rate across all workers.
// Set to 10,000 transactions per second to ensure we never exceed this limit.
// Only enforced when rate limiting is enabled.
const MaxSubmissionsPerSecond = 10000

// SilenceInterval defines how often to pause submissions (e.g., every 1 second)
const SilenceInterval = 1 * time.Second

// SilenceDuration defines how long to pause (e.g., 10 milliseconds)
const SilenceDuration = 10 * time.Millisecond

// NewBatchSubmitter creates a new BatchSubmitter.
// If cache is non-nil, EthTxBytes are stored per-transaction before submission.
// numWorkers specifies the number of parallel submission goroutines (default: 16).
// Creates one submitter instance per worker for optimal parallel performance.
// If enableRateLimiting is true, rate limiting is applied to ensure aggregate submission rate
// across all workers does not exceed MaxSubmissionsPerSecond (11,000 tx/s).
func NewBatchSubmitter(
	submitters []Submitter,
	inputChan chan sdk.Endorsement,
	numWorkers int,
	enableRateLimiting bool,
) *BatchSubmitter {
	if numWorkers <= 0 {
		numWorkers = DefaultNumWorkers
	}

	// Ensure we have enough submitters for the workers
	if len(submitters) < numWorkers {
		panic(fmt.Sprintf("Only %d submitters provided for %d workers.", len(submitters), numWorkers))
	}

	var rateLimiter *rate.Limiter
	var silenceConfig *SilenceConfig

	if enableRateLimiting {
		// Enable silence windows when rate limiting is active
		silenceConfig = &SilenceConfig{
			Interval: SilenceInterval,
			Duration: SilenceDuration,
		}

		// Calculate burst size to allow catching up after silence windows
		// During a 10ms silence at 10,000 tx/s, we "miss" 100 transactions
		// Allow burst to catch up: 100 transactions
		missedTxDuringSilence := int(float64(MaxSubmissionsPerSecond) * SilenceDuration.Seconds())
		burstSize := missedTxDuringSilence
		if burstSize < 1 {
			burstSize = 1
		}

		// Create a rate limiter with the same rate but larger burst to allow catch-up
		rateLimiter = rate.NewLimiter(rate.Limit(MaxSubmissionsPerSecond), burstSize)
		batchLogger.Infof("Rate limiting enabled with periodic silence windows:")
		batchLogger.Infof("  Target rate: %d tx/s", MaxSubmissionsPerSecond)
		batchLogger.Infof("  Silence: %v every %v", SilenceDuration, SilenceInterval)
		batchLogger.Infof("  Burst size: %d (to catch up after silence)", burstSize)
	}

	bs := &BatchSubmitter{
		submitters:    submitters,
		inputChan:     inputChan,
		stopChan:      make(chan struct{}),
		doneChan:      make(chan struct{}),
		numWorkers:    numWorkers,
		rateLimiter:   rateLimiter,
		silenceConfig: silenceConfig,
	}

	// Initialize lastSilenceEnd to now if silence is configured
	if silenceConfig != nil {
		bs.lastSilenceEnd.Store(time.Now())
	}

	return bs
}

// Start begins the submission loop with multiple worker goroutines.
func (bs *BatchSubmitter) Start(ctx context.Context) {
	go bs.run(ctx)
}

// Stop signals the submitter to stop and waits for all workers to finish.
func (bs *BatchSubmitter) Stop() {
	close(bs.stopChan)
	<-bs.doneChan
}

// Close closes all submitter connections.
func (bs *BatchSubmitter) Close() error {
	var firstErr error
	for i, submitter := range bs.submitters {
		if err := submitter.Close(); err != nil {
			batchLogger.Errorf("Failed to close submitter %d: %v", i, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (bs *BatchSubmitter) run(ctx context.Context) {
	defer close(bs.doneChan)

	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < bs.numWorkers; i++ {
		wg.Add(1)
		go bs.worker(ctx, i, &wg)
	}

	// Wait for all workers to complete
	wg.Wait()
}

func (bs *BatchSubmitter) worker(ctx context.Context, workerID int, wg *sync.WaitGroup) {
	defer wg.Done()

	batchLogger.Debugf("Worker %d started", workerID)
	defer batchLogger.Debugf("Worker %d stopped", workerID)

	for {
		// Report queue size metric if callback is set
		if SetBatchSubmitterQueueSizeMetric != nil {
			SetBatchSubmitterQueueSizeMetric(len(bs.inputChan))
		}

		select {
		case <-bs.stopChan:
			return

		case end, ok := <-bs.inputChan:
			if !ok {
				return
			}
			if err := bs.submitOne(ctx, workerID, end); err != nil {
				batchLogger.Errorf("Worker %d: submit failed: %v", workerID, err)
			}
		}
	}
}

func (bs *BatchSubmitter) submitOne(ctx context.Context, workerID int, end sdk.Endorsement) error {
	// Check if we need to enforce a silence window (only when rate limiting is enabled)
	// This creates gaps for other processes to submit to the ordering service
	if bs.silenceConfig != nil {
		bs.enforceSilenceWindow(ctx)
	}

	// Wait for rate limiter to allow this submission (if enabled)
	// This enforces the aggregate rate limit across all workers
	// The burst size allows catching up after silence windows
	if bs.rateLimiter != nil {
		if err := bs.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	var txid string
	t0 := time.Now()
	err := bs.submitters[workerID].Submit(ctx, end)
	batchLogger.Debugf("[SUBMIT] worker=%d txid=%s submit_took=%v", workerID, txid, time.Since(t0))
	return err
}

// enforceSilenceWindow checks if it's time for a silence window and pauses if needed.
// Only one worker will trigger the silence window; others will wait if they arrive during it.
// This creates predictable gaps where other processes can submit to the ordering service.
// The rate limiter's burst capacity allows catching up after the pause.
func (bs *BatchSubmitter) enforceSilenceWindow(ctx context.Context) {
	now := time.Now()
	lastEnd := bs.lastSilenceEnd.Load().(time.Time)

	// Check if it's time for the next silence window
	timeSinceLastSilence := now.Sub(lastEnd)
	if timeSinceLastSilence >= SilenceInterval {
		// Try to claim this silence window using atomic compare-and-swap
		// This ensures only one worker triggers the silence
		expectedEnd := now.Add(SilenceDuration)
		if bs.lastSilenceEnd.CompareAndSwap(lastEnd, expectedEnd) {
			// We won the race - enforce the silence window
			batchLogger.Debugf("Silence window starting: duration=%v", SilenceDuration)
			select {
			case <-time.After(SilenceDuration):
				batchLogger.Debugf("Silence window ended")
			case <-ctx.Done():
				return
			}
		} else {
			// Another worker is handling the silence window
			// Wait if we're still within the silence period
			currentEnd := bs.lastSilenceEnd.Load().(time.Time)
			if now.Before(currentEnd) {
				waitDuration := currentEnd.Sub(now)
				batchLogger.Debugf("Waiting for ongoing silence window: remaining=%v", waitDuration)
				select {
				case <-time.After(waitDuration):
				case <-ctx.Done():
					return
				}
			}
		}
	}
}
