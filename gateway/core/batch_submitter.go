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
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/gateway/metrics"
	sdk "github.com/hyperledger/fabric-x-sdk"
)

var batchLogger = flogging.MustGetLogger("gateway.core.batch_submitter")

// BatchSubmitter reads endorsements from a channel, optionally records them in the
// pending-tx cache (when cache != nil), then submits each one to the orderer.
// The cache is used by AllTxBatchDispatcher to correlate commit events with the
// originating Ethereum transaction.
type BatchSubmitter struct {
	submitter Submitter
	cache     *PendingTxCache // nil → skip cache; non-nil → store EthTxBytes keyed by FabricTxID
	inputChan chan sdk.Endorsement
	stopChan  chan struct{}
	doneChan  chan struct{}
}

// NewBatchSubmitter creates a new BatchSubmitter.
// If cache is non-nil, EthTxBytes are stored per-transaction before submission.
func NewBatchSubmitter(
	submitter Submitter,
	cache *PendingTxCache,
	inputChan chan sdk.Endorsement,
) *BatchSubmitter {
	return &BatchSubmitter{
		submitter: submitter,
		cache:     cache,
		inputChan: inputChan,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
	}
}

// Start begins the submission loop in a goroutine.
func (bs *BatchSubmitter) Start(ctx context.Context) {
	go bs.run(ctx)
}

// Stop signals the submitter to stop and waits for it to finish.
func (bs *BatchSubmitter) Stop() {
	close(bs.stopChan)
	<-bs.doneChan
}

func (bs *BatchSubmitter) run(ctx context.Context) {
	defer close(bs.doneChan)

	for {
		select {
		case <-bs.stopChan:
			return

		case end, ok := <-bs.inputChan:
			if !ok {
				return
			}
			if err := bs.submitOne(ctx, end); err != nil {
				batchLogger.Errorf("submit failed: %v", err)
			}
		}
	}
}

func (bs *BatchSubmitter) submitOne(ctx context.Context, end sdk.Endorsement) error {
	if bs.cache != nil {
		txid, err := extractTxIDFromProposal(end.Proposal)
		if err != nil {
			return fmt.Errorf("extract txid: %w", err)
		}
		ethTxBytes, err := extractEthTxBytes(end.Proposal)
		if err != nil {
			return fmt.Errorf("extract eth tx bytes: %w", err)
		}
		if err := bs.cache.Add(&TxCacheEntry{FabricTxID: txid, EthTxBytes: ethTxBytes}); err != nil {
			return fmt.Errorf("add to cache: %w", err)
		}
	}
	submitStart := time.Now()
	err := bs.submitter.Submit(ctx, end)
	metrics.GatewaySubmitDuration.Observe(time.Since(submitStart).Seconds())
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

	if cis.ChaincodeSpec == nil || cis.ChaincodeSpec.Input == nil || len(cis.ChaincodeSpec.Input.Args) < 2 {
		return nil, fmt.Errorf("invalid chaincode spec: missing args")
	}

	// Args[0] is ProposalTypeEVMTx, Args[1] is the Ethereum tx bytes
	return cis.ChaincodeSpec.Input.Args[1], nil
}
