/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"google.golang.org/protobuf/proto"
)

var batchLogger = flogging.MustGetLogger("gateway.core.batch_submitter")

// BatchSubmitterConfig contains configuration for the batch submitter.
type BatchSubmitterConfig struct {
	// MaxBatchSize is the maximum number of transactions to batch together
	MaxBatchSize int

	// MaxWaitTime is the maximum time to wait before submitting a partial batch
	MaxWaitTime time.Duration

	// SubscriptionDelay is an optional delay after subscription before submission
	// This helps ensure the subscription is fully registered
	SubscriptionDelay time.Duration

	// EnableNotifications enables sending txIDs to the notification service
	// If false, transactions are submitted without notification subscription
	EnableNotifications bool
}

// DefaultBatchSubmitterConfig returns sensible defaults.
func DefaultBatchSubmitterConfig() BatchSubmitterConfig {
	return BatchSubmitterConfig{
		MaxBatchSize:        100,
		MaxWaitTime:         1 * time.Millisecond,
		SubscriptionDelay:   0 * time.Millisecond,
		EnableNotifications: false, // Disabled by default for backward compatibility
	}
}

// BatchSubmitter batches endorsements and coordinates notification subscription
// before submitting to the orderer.
type BatchSubmitter struct {
	config    BatchSubmitterConfig
	submitter Submitter
	cache     *PendingTxCache
	inputChan chan sdk.Endorsement
	txIDsChan chan []string // Channel to send txIDs to the notifier
	stopChan  chan struct{}
	doneChan  chan struct{}
}

// NewBatchSubmitter creates a new batch submitter.
// The input channel should be buffered to prevent blocking workers.
// The txIDsChan is used to send transaction IDs to the notification subscriber.
func NewBatchSubmitter(
	config BatchSubmitterConfig,
	submitter Submitter,
	cache *PendingTxCache,
	inputChan chan sdk.Endorsement,
	txIDsChan chan []string,
) *BatchSubmitter {
	return &BatchSubmitter{
		config:    config,
		submitter: submitter,
		cache:     cache,
		inputChan: inputChan,
		txIDsChan: txIDsChan,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
	}
}

// Start begins the batch submission loop in a goroutine.
func (bs *BatchSubmitter) Start(ctx context.Context) {
	go bs.run(ctx)
}

// Stop signals the batch submitter to stop and waits for it to finish.
func (bs *BatchSubmitter) Stop() {
	close(bs.stopChan)
	<-bs.doneChan
}

// run is the main batch submission loop.
func (bs *BatchSubmitter) run(ctx context.Context) {
	defer close(bs.doneChan)

	var batch []sdk.Endorsement
	timer := time.NewTimer(bs.config.MaxWaitTime)
	defer timer.Stop()

	for {
		var shouldSubmit bool
		var shouldExit bool

		select {
		case <-bs.stopChan:
			shouldSubmit = true
			shouldExit = true

		case end, ok := <-bs.inputChan:
			if !ok {
				shouldSubmit = true
				shouldExit = true
			} else {
				// Add to batch
				batch = append(batch, end)
				// Submit if batch is full
				shouldSubmit = len(batch) >= bs.config.MaxBatchSize
			}

		case <-timer.C:
			// Timer expired, submit partial batch if any and reset timer
			shouldSubmit = len(batch) > 0
			timer.Reset(bs.config.MaxWaitTime)
		}

		if shouldSubmit && len(batch) > 0 {
			if err := bs.submitBatch(ctx, batch); err != nil {
				logger.Errorf("Failed to submit batch: %v", err)
			}
			batch = nil
		}

		if shouldExit {
			return
		}
	}
}

// submitBatch subscribes to notifications, populates cache, and submits the batch to the orderer.
func (bs *BatchSubmitter) submitBatch(ctx context.Context, batch []sdk.Endorsement) error {
	if len(batch) == 0 {
		return nil
	}

	batchLogger.Debugf("Submitting batch of %d endorsements (notifications=%v)", len(batch), bs.config.EnableNotifications)

	// Only extract txids and populate cache if notifications are enabled
	if bs.config.EnableNotifications {
		// Extract Fabric transaction IDs and populate cache
		txids := make([]string, len(batch))
		for i, end := range batch {
			// Extract txid from the proposal header
			txid, err := extractTxIDFromProposal(end.Proposal)
			if err != nil {
				panic(fmt.Sprintf("Failed to extract txid from proposal: %v", err))
			}
			txids[i] = txid

			// Extract data from endorsement response and populate cache
			entry, err := extractCacheEntry(end, txid)
			if err != nil {
				panic(fmt.Sprintf("Failed to extract cache entry for txid %s: %v", txid, err))
			}
			if err := bs.cache.Add(entry); err != nil {
				panic(fmt.Sprintf("Failed to add entry to cache for txid %s: %v", txid, err))
			}
		}

		// Send txIDs to the notification subscriber BEFORE submitting to orderer
		if bs.txIDsChan != nil {
			batchLogger.Debugf("Sending %d txIDs to notification subscriber", len(txids))
			select {
			case bs.txIDsChan <- txids:
				batchLogger.Debugf("Successfully sent txIDs for subscription")
			case <-ctx.Done():
				return fmt.Errorf("context canceled while subscribing: %w", ctx.Err())
			}

			// Optional delay to ensure subscription is registered
			if bs.config.SubscriptionDelay > 0 {
				batchLogger.Debugf("Waiting %v for subscription to register", bs.config.SubscriptionDelay)
				time.Sleep(bs.config.SubscriptionDelay)
			}
		}
	}

	// Submit all endorsements to the orderer
	for i, end := range batch {
		if err := bs.submitter.Submit(ctx, end); err != nil {
			// Log error but continue with remaining submissions
			batchLogger.Errorf("Failed to submit endorsement %d/%d: %v", i+1, len(batch), err)
		}
	}

	batchLogger.Debugf("Successfully submitted batch of %d transactions to orderer", len(batch))
	return nil
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

// extractCacheEntry extracts the data needed for the cache from an endorsement.
func extractCacheEntry(end sdk.Endorsement, txid string) (*TxCacheEntry, error) {
	// Extract ethereum tx bytes from proposal args
	ethTxBytes, err := extractEthTxBytes(end.Proposal)
	if err != nil {
		return nil, fmt.Errorf("extract eth tx bytes: %w", err)
	}

	// Extract NsRWS and Events from the first response
	if len(end.Responses) == 0 {
		return nil, fmt.Errorf("no responses in endorsement")
	}

	nsrws, events, err := extractRWSAndEvents(end.Responses[0])
	if err != nil {
		return nil, fmt.Errorf("extract rws and events: %w", err)
	}

	return &TxCacheEntry{
		EthTxBytes: ethTxBytes,
		NsRWS:      nsrws,
		Events:     events,
		FabricTxID: txid,
	}, nil
}

// extractEthTxBytes extracts the ethereum transaction bytes from proposal args.
func extractEthTxBytes(proposal *peer.Proposal) ([]byte, error) {
	cpp, err := protoutil.UnmarshalChaincodeProposalPayload(proposal.Payload)
	if err != nil {
		return nil, fmt.Errorf("unmarshal chaincode proposal payload: %w", err)
	}

	cis, err := protoutil.UnmarshalChaincodeInvocationSpec(cpp.Input)
	if err != nil {
		return nil, fmt.Errorf("unmarshal chaincode invocation spec: %w", err)
	}

	if cis.ChaincodeSpec == nil || cis.ChaincodeSpec.Input == nil || len(cis.ChaincodeSpec.Input.Args) < 2 {
		return nil, fmt.Errorf("invalid chaincode spec: missing args")
	}

	// Args[0] is ProposalTypeEVMTx, Args[1] is the ethereum tx bytes
	return cis.ChaincodeSpec.Input.Args[1], nil
}

// extractRWSAndEvents extracts the read-write set and events from a proposal response.
func extractRWSAndEvents(resp *peer.ProposalResponse) ([]blocks.NsReadWriteSet, []byte, error) {
	// Unmarshal the payload which contains the marshaled applicationpb.Tx (RWSet)
	var tx applicationpb.Tx
	if err := proto.Unmarshal(resp.Payload, &tx); err != nil {
		return nil, nil, fmt.Errorf("unmarshal tx from payload: %w", err)
	}

	if len(tx.Namespaces) == 0 {
		return nil, nil, fmt.Errorf("no namespaces in tx")
	}

	// Convert from applicationpb.TxNamespace to blocks.NsReadWriteSet
	nsrws := make([]blocks.NsReadWriteSet, 0, len(tx.Namespaces))
	var events []byte

	for _, ns := range tx.Namespaces {
		rws := blocks.ReadWriteSet{
			Reads:  make([]blocks.KVRead, 0),
			Writes: make([]blocks.KVWrite, 0),
		}

		// Convert reads
		for _, r := range ns.ReadsOnly {
			kvRead := blocks.KVRead{
				Key: string(r.Key),
			}
			if r.Version != nil {
				kvRead.Version = &blocks.Version{BlockNum: *r.Version}
			}
			rws.Reads = append(rws.Reads, kvRead)
		}

		// Convert read-writes
		for _, rw := range ns.ReadWrites {
			kvRead := blocks.KVRead{
				Key: string(rw.Key),
			}
			if rw.Version != nil {
				kvRead.Version = &blocks.Version{BlockNum: *rw.Version}
			}
			rws.Reads = append(rws.Reads, kvRead)

			rws.Writes = append(rws.Writes, blocks.KVWrite{
				Key:   string(rw.Key),
				Value: rw.Value,
			})
		}

		// Convert blind writes and extract events
		for _, w := range ns.BlindWrites {
			key := string(w.Key)

			// Check if this is the events key
			if key == "_event_" {
				events = w.Value
				continue // Don't add to writes
			}

			// Skip the input key as well
			if key == "_input_" {
				continue
			}

			rws.Writes = append(rws.Writes, blocks.KVWrite{
				Key:   key,
				Value: w.Value,
			})
		}

		nsrws = append(nsrws, blocks.NsReadWriteSet{
			Namespace: ns.NsId,
			RWS:       rws,
		})
	}

	return nsrws, events, nil
}
