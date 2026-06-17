/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	fc "github.com/hyperledger/fabric-x-evm/common"
	sdk "github.com/hyperledger/fabric-x-sdk"
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
type BatchSubmitter struct {
	submitters []Submitter     // One submitter per worker for parallel submission
	cache      *PendingTxCache // nil → skip cache; non-nil → store EthTxBytes keyed by FabricTxID
	inputChan  chan sdk.Endorsement
	stopChan   chan struct{}
	doneChan   chan struct{}
	numWorkers int
}

const DefaultNumWorkers = 16

// NewBatchSubmitter creates a new BatchSubmitter.
// If cache is non-nil, EthTxBytes are stored per-transaction before submission.
// numWorkers specifies the number of parallel submission goroutines (default: 16).
// Creates one submitter instance per worker for optimal parallel performance.
func NewBatchSubmitter(
	submitters []Submitter,
	cache *PendingTxCache,
	inputChan chan sdk.Endorsement,
	numWorkers int,
) *BatchSubmitter {
	if numWorkers <= 0 {
		numWorkers = DefaultNumWorkers
	}

	// Ensure we have enough submitters for the workers
	if len(submitters) < numWorkers {
		panic(fmt.Sprintf("Only %d submitters provided for %d workers.", len(submitters), numWorkers))
	}

	return &BatchSubmitter{
		submitters: submitters,
		cache:      cache,
		inputChan:  inputChan,
		stopChan:   make(chan struct{}),
		doneChan:   make(chan struct{}),
		numWorkers: numWorkers,
	}
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
	var txid string
	if bs.cache != nil {
		var err error
		txid, err = extractTxIDFromProposal(end.Proposal)
		if err != nil {
			return fmt.Errorf("extract txid: %w", err)
		}
		ethTxBytes, err := extractEthTxBytes(end.Proposal)
		if err != nil {
			return fmt.Errorf("extract eth tx bytes: %w", err)
		}
		bs.cache.Add(txid, ethTxBytes)

		// Record T2 timestamp if tracking is enabled
		if SubmissionTimestamps != nil {
			// Parse eth tx to get hash
			ethTx := new(types.Transaction)
			if err := ethTx.UnmarshalBinary(ethTxBytes); err == nil {
				SubmissionTimestampsMu.Lock()
				SubmissionTimestamps[ethTx.Hash()] = time.Now() // T2: submitted to orderer
				SubmissionTimestampsMu.Unlock()
			}
		}
	}
	t0 := time.Now()
	err := bs.submitters[workerID].Submit(ctx, end)
	batchLogger.Debugf("[SUBMIT] worker=%d txid=%s submit_took=%v", workerID, txid, time.Since(t0))
	return err
}

// extractTxIDFromProposal extracts the transaction ID from a proposal.
func extractTxIDFromProposal(proposal *peer.Proposal) (string, error) {
	hdr, err := protoutil.UnmarshalHeader(proposal.Header)
	if err != nil {
		return "", fmt.Errorf("unmarshal header: %w", err)
	}

	chdr, err := protoutil.UnmarshalChannelHeader(hdr.ChannelHeader)
	if err != nil {
		return "", fmt.Errorf("unmarshal channel header: %w", err)
	}

	return chdr.TxId, nil
}

// extractEthTxBytes extracts the RLP-encoded Ethereum transaction from proposal args.
func extractEthTxBytes(proposal *peer.Proposal) ([]byte, error) {
	cpp, err := protoutil.UnmarshalChaincodeProposalPayload(proposal.Payload)
	if err != nil {
		return nil, fmt.Errorf("unmarshal chaincode proposal payload: %w", err)
	}

	cis, err := protoutil.UnmarshalChaincodeInvocationSpec(cpp.Input)
	if err != nil {
		return nil, fmt.Errorf("unmarshal chaincode invocation spec: %w", err)
	}

	if cis.ChaincodeSpec == nil || cis.ChaincodeSpec.Input == nil || len(cis.ChaincodeSpec.Input.Args) != 2 {
		return nil, fmt.Errorf("invalid chaincode spec: missing args")
	}

	// Validate that Args[0] is ProposalTypeEVMTx
	if len(cis.ChaincodeSpec.Input.Args[0]) != 1 || cis.ChaincodeSpec.Input.Args[0][0] != byte(fc.ProposalTypeEVMTx) {
		return nil, fmt.Errorf("invalid proposal type: expected ProposalTypeEVMTx")
	}

	// Args[0] is ProposalTypeEVMTx, Args[1] is the Ethereum tx bytes
	return cis.ChaincodeSpec.Input.Args[1], nil
}
