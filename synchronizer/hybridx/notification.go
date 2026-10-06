/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/metrics"
)

var notifLogger = flogging.MustGetLogger("synchronizer.hybridx.notification")

// BlockHandler defines the interface for handlers that
// process committed blocks delivered via the AllTxStreamer path
type BlockHandler interface {
	Handle(ctx context.Context, b blocks.Block) error
}

// AllTxBatchDispatcher implements notification.AllTxHandler. It bridges AllTxStreamer
// (which delivers every committed transaction) to the internal BlockHandler chain.
//
// For each committed block it:
//  1. Filters out non-EVM transactions (those without Ethereum tx bytes in InputArgs).
//  2. Assembles a blocks.Block from the remaining events.
//  3. Dispatches the block to all registered BlockHandler.
//
// Decoding the wire format is the SDK's job, not ours: each CommittedTxEvent embeds a
// blocks.Transaction that the network boundary has already populated with the same
// DecodeMetadata/DecodeNamespaces pair the block parser uses on the delivery path. A
// block therefore looks identical whichever path delivered it, which matters because
// under hybridx both paths feed one handler chain and one trie.
type AllTxBatchDispatcher struct {
	handlers []BlockHandler
}

// NewAllTxBatchDispatcher creates a dispatcher that filters EVM transactions from
// AllTxStreamer events and dispatches them as a blocks.Block to the handler chain.
func NewAllTxBatchDispatcher(handlers ...BlockHandler) *AllTxBatchDispatcher {
	return &AllTxBatchDispatcher{handlers: handlers}
}

// HandleBatch implements notification.AllTxHandler.
func (d *AllTxBatchDispatcher) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	notifLogger.Debugf("[BLOCK] block=%d total_events=%d", batch.BlockNumber, len(batch.Events))

	txs := make([]blocks.Transaction, 0, len(batch.Events))
	// ethHashes collects the Ethereum tx hashes for every EVM event in this
	// block so we can close per-tx latency windows after all handlers run.
	ethHashes := make([]ethcommon.Hash, 0, len(batch.Events))

	// STEP 15: record per-tx outcomes as each EVM event arrives from the orderer.
	// Unmarshal the Ethereum tx hash from InputArgs[1] to:
	//   - stamp the notification time (starts the ordering→notification window),
	//   - count outcomes by Fabric commit status.
	// ObserveTxComplete is deferred to STEP 17 so tx_post_order covers the full
	// handler-dispatch leg, not just the time up to the first handler.
	for _, event := range batch.Events {
		// InputArgs is empty when the transaction carried no metadata at all and
		// when it carried no args, so this one check covers both.
		if len(event.InputArgs) < 2 {
			notifLogger.Debugf("Skipping tx %s: no ethereum tx in metadata", event.ID)
			continue
		}

		if !bytes.Equal(event.InputArgs[0], []byte{byte(common.ProposalTypeEVMTx)}) {
			notifLogger.Debugf("Skipping tx %s: not an EVM transaction", event.ID)
			continue
		}

		// Recover the Ethereum tx hash from the raw RLP bytes in InputArgs[1].
		ethTx := new(ethtypes.Transaction)
		if err := ethTx.UnmarshalBinary(event.InputArgs[1]); err == nil {
			hash := ethTx.Hash()
			// Stamp STEP 15 and record tx_ordering latency (STEP 14→15).
			metrics.Default().RecordTxNotified(hash)
			ethHashes = append(ethHashes, hash)
			// Count outcome by Fabric commit status.
			switch event.Status {
			case blocks.StatusCommitted:
				metrics.Default().TxCommitted.Inc()
			case blocks.StatusMVCCConflict:
				metrics.Default().TxMVCCConflict.Inc()
			case blocks.StatusInvalidSignature:
				metrics.Default().TxInvalidSignature.Inc()
			default:
				if event.Status != blocks.StatusUnknown {
					metrics.Default().TxOtherFailure.Inc()
				}
			}
		}

		txs = append(txs, event.Transaction)
	}

	if len(txs) == 0 {
		return nil
	}

	notifLogger.Debugf("[NOTIFY] block=%d dispatching=%d/%d txs", batch.BlockNumber, len(txs), len(batch.Events))

	// TODO: StreamAllTransactions carries no block header, so there's no real hash to
	// use here. Replace with the real hash once
	// https://github.com/hyperledger/fabric-x-committer/issues/773 is implemented.
	var parentNum uint64
	if batch.BlockNumber > 0 {
		parentNum = batch.BlockNumber - 1
	}
	b := blocks.Block{
		Number:       batch.BlockNumber,
		Hash:         blockNumberHash(batch.BlockNumber),
		ParentHash:   blockNumberHash(parentNum),
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
	}

	// STEP 16: dispatch to each registered handler, measuring per-handler latency.
	// The handler type name is used as a Prometheus label so individual steps can be
	// distinguished (e.g. "gateway.Gateway" vs "gateway/storage.Chain").
	for _, h := range d.handlers {
		handlerName := reflect.TypeOf(h).String()
		t16 := metrics.Now()
		if err := h.Handle(ctx, b); err != nil {
			// graceful shutdown
			if ctx.Err() != nil {
				return err
			}

			panic(fmt.Errorf("handler failed: %w", err))
		}
		metrics.Default().StepLatency.WithLabelValues(metrics.StepHandler + ":" + handlerName).Observe(metrics.Since(t16).Seconds())
	}
	// STEP 17: block fully dispatched to all handlers – close the per-tx latency
	// windows, recording e2e and tx_post_order for each Ethereum tx in this block.
	for _, hash := range ethHashes {
		metrics.Default().ObserveTxComplete(hash)
	}

	return nil
}

// blockNumberHash encodes n as a 32-byte big-endian hash-shaped placeholder.
func blockNumberHash(n uint64) []byte {
	h := make([]byte, 32)
	binary.BigEndian.PutUint64(h[24:], n)
	return h
}
