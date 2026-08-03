/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"google.golang.org/protobuf/proto"
)

// TxNotification contains data about a committed Ethereum transaction, reconstructed
// from a blocks.Transaction for use by components that need EVM-specific fields
// (e.g. EthTxHash, EthTxBytes) beyond what blocks.Transaction provides.
type TxNotification struct {
	BlockNum   uint64
	TxNum      uint64
	FabricTxID string
	Status     committerpb.Status
	EthTxBytes []byte
	EthTxHash  common.Hash
}

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
		if len(event.Metadata) == 0 {
			notifLogger.Debugf("Skipping tx %s: no metadata", event.TxID)
			continue
		}

		var input peer.ChaincodeInput
		if err := proto.Unmarshal(event.Metadata[0], &input); err != nil || len(input.Args) < 2 {
			notifLogger.Debugf("Skipping tx %s: cannot extract eth tx from metadata", event.TxID)
			continue
		}

		nsrws, events := namespacesToNsRWS(event.Namespaces)

		txs = append(txs, blocks.Transaction{
			ID:        event.TxID,
			Number:    int64(event.TxNum),
			InputArgs: input.Args,
			Valid:     event.Status == committerpb.Status_COMMITTED,
			Status:    int(event.Status),
			Events:    events,
			NsRWS:     nsrws,
		})
	}

	if len(txs) == 0 {
		return nil
	}

	notifLogger.Debugf("[NOTIFY] block=%d dispatching=%d/%d txs", batch.BlockNumber, len(txs), len(batch.Events))

	b := blocks.Block{
		Number:       batch.BlockNumber,
		Transactions: txs,
	}

	for _, h := range d.handlers {
		if err := h.Handle(ctx, b); err != nil {
			panic(fmt.Errorf("handler failed: %w", err))
		}
	}

	return nil
}

// namespacesToNsRWS converts applicationpb.TxNamespace slices (as delivered by
// AllTxStreamer) into the blocks.NsReadWriteSet format used internally.
// It also extracts the special _event_ key as raw event bytes.
func namespacesToNsRWS(namespaces []*applicationpb.TxNamespace) ([]blocks.NsReadWriteSet, []byte) {
	nsrws := make([]blocks.NsReadWriteSet, 0, len(namespaces))
	var events []byte

	for _, ns := range namespaces {
		rws := blocks.ReadWriteSet{
			Reads:  make([]blocks.KVRead, 0),
			Writes: make([]blocks.KVWrite, 0),
		}

		for _, r := range ns.ReadsOnly {
			kvRead := blocks.KVRead{Key: string(r.Key)}
			if r.Version != nil {
				kvRead.Version = &blocks.Version{BlockNum: *r.Version}
			}
			rws.Reads = append(rws.Reads, kvRead)
		}

		for _, rw := range ns.ReadWrites {
			kvRead := blocks.KVRead{Key: string(rw.Key)}
			if rw.Version != nil {
				kvRead.Version = &blocks.Version{BlockNum: *rw.Version}
			}
			rws.Reads = append(rws.Reads, kvRead)
			rws.Writes = append(rws.Writes, blocks.KVWrite{
				Key:   string(rw.Key),
				Value: rw.Value,
			})
		}

		for _, w := range ns.BlindWrites {
			key := string(w.Key)
			switch key {
			case "_event_":
				events = w.Value
			case "_input_":
				// skip
			default:
				rws.Writes = append(rws.Writes, blocks.KVWrite{Key: key, Value: w.Value})
			}
		}

		nsrws = append(nsrws, blocks.NsReadWriteSet{Namespace: ns.NsId, RWS: rws})
	}

	return nsrws, events
}
