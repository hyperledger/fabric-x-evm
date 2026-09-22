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
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"

	"github.com/hyperledger/fabric-x-evm/common"
)

var notifLogger = flogging.MustGetLogger("evm.notification")

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
	for _, event := range batch.Events {
		// InputArgs is empty when the transaction carried no metadata at all and when
		// its ChaincodeInput failed to parse, so this one check covers both.
		if len(event.InputArgs) < 2 {
			notifLogger.Debugf("Skipping tx %s: no ethereum tx in metadata", event.ID)
			continue
		}

		if !bytes.Equal(event.InputArgs[0], []byte{byte(common.ProposalTypeEVMTx)}) {
			notifLogger.Debugf("Skipping tx %s: not an EVM transaction", event.ID)
			continue
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

	for _, h := range d.handlers {
		if err := h.Handle(ctx, b); err != nil {
			// graceful shutdown
			if ctx.Err() != nil {
				return err
			}

			panic(fmt.Errorf("handler failed: %w", err))
		}
	}

	return nil
}

// blockNumberHash encodes n as a 32-byte big-endian hash-shaped placeholder.
func blockNumberHash(n uint64) []byte {
	h := make([]byte, 32)
	binary.BigEndian.PutUint64(h[24:], n)
	return h
}
