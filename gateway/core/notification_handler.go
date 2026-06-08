/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
)

var notifLogger = flogging.MustGetLogger("gateway.core.notification")

// TxHandler defines the interface for handlers that process transaction notifications in batches.
type TxHandler interface {
	HandleTx(ctx context.Context, notifs []TxNotification) error
}

// AllTxBatchDispatcher implements notification.AllTxHandler. It bridges AllTxStreamer
// (which delivers every committed transaction) to the internal TxHandler chain.
//
// For each committed transaction event it:
//  1. Looks up the corresponding Ethereum tx bytes from the pending cache.
//  2. If not in cache (transaction not submitted by us), skips the event entirely.
//  3. Converts the event's Namespaces to NsRWS + Events.
//  4. Builds a TxNotification and dispatches it to all registered TxHandlers.
//
// NOTE: only transactions present in the cache (i.e. submitted by this gateway) are
// dispatched. In a multi-tenant namespace this means state updates from other
// participants are not forwarded to handlers such as LightKVS. For the current
// single-tenant perf-test use-case this is acceptable.
type AllTxBatchDispatcher struct {
	cache    *PendingTxCache
	handlers []TxHandler
}

// NewAllTxBatchDispatcher creates a dispatcher that augments AllTxBatch events
// with cached EthTxBytes and dispatches them as TxNotifications to the handler chain.
func NewAllTxBatchDispatcher(cache *PendingTxCache, handlers ...TxHandler) *AllTxBatchDispatcher {
	return &AllTxBatchDispatcher{cache: cache, handlers: handlers}
}

// HandleBatch implements notification.AllTxHandler.
func (d *AllTxBatchDispatcher) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	notifLogger.Debugf("[BLOCK] block=%d total_events=%d", batch.BlockNumber, len(batch.Events))

	notifs := make([]TxNotification, 0, len(batch.Events))
	for _, event := range batch.Events {
		ethTxBytes := d.cache.Get(event.TxID)
		if ethTxBytes == nil {
			notifLogger.Debugf("Skipping tx %s: not in cache (not submitted by us)", event.TxID)
			continue
		}

		var ethTx types.Transaction
		if err := ethTx.UnmarshalBinary(ethTxBytes); err != nil {
			return fmt.Errorf("unmarshal eth tx for %s: %w", event.TxID, err)
		}

		nsrws, events := namespacesToNsRWS(event.Namespaces)

		notifs = append(notifs, TxNotification{
			BlockNum:   event.BlockNum,
			TxNum:      uint64(event.TxNum),
			FabricTxID: event.TxID,
			Status:     event.Status,
			EthTxBytes: ethTxBytes,
			EthTxHash:  ethTx.Hash(),
			NsRWS:      nsrws,
			Events:     events,
		})
	}

	if len(notifs) == 0 {
		return nil
	}

	notifLogger.Debugf("[NOTIFY] block=%d dispatching=%d/%d our_txs", batch.BlockNumber, len(notifs), len(batch.Events))

	for _, h := range d.handlers {
		if err := h.HandleTx(ctx, notifs); err != nil {
			panic(fmt.Errorf("handler failed: %w", err))
		}
	}

	// Clean up processed transactions from cache
	for _, notif := range notifs {
		d.cache.Delete(notif.FabricTxID)
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
