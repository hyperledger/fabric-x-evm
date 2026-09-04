/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"google.golang.org/protobuf/proto"
)

var notifLogger = flogging.MustGetLogger("evm.notification")

// AllTxBatchDispatcher implements notification.AllTxHandler. It bridges AllTxStreamer
// (which delivers every committed transaction) to the internal BlockHandler chain,
// assembling a blocks.Block from a batch's events and dispatching it.
type AllTxBatchDispatcher struct {
	handlers []blocks.BlockHandler
}

// NewAllTxBatchDispatcher creates a dispatcher that assembles AllTxStreamer events
// into a blocks.Block and dispatches it to the handler chain.
func NewAllTxBatchDispatcher(handlers ...blocks.BlockHandler) *AllTxBatchDispatcher {
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

		nsrws := namespacesToNsRWS(event.Namespaces)

		// In fabric-x format, events are in Metadata[1] (not in BlindWrites).
		var txEvents []byte
		if len(event.Metadata) > 1 {
			txEvents = event.Metadata[1]
		}

		txs = append(txs, blocks.Transaction{
			ID:        event.TxID,
			Number:    int64(event.TxNum),
			InputArgs: input.Args,
			Valid:     event.Status == committerpb.Status_COMMITTED,
			Status:    int(event.Status),
			Events:    txEvents,
			NsRWS:     nsrws,
		})
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

// namespacesToNsRWS converts applicationpb.TxNamespace slices (as delivered by
// AllTxStreamer) into the blocks.NsReadWriteSet format used internally.
func namespacesToNsRWS(namespaces []*applicationpb.TxNamespace) []blocks.NsReadWriteSet {
	nsrws := make([]blocks.NsReadWriteSet, 0, len(namespaces))

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
			rws.Writes = append(rws.Writes, blocks.KVWrite{Key: string(w.Key), Value: w.Value})
		}

		nsrws = append(nsrws, blocks.NsReadWriteSet{Namespace: ns.NsId, RWS: rws})
	}

	return nsrws
}
