/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"google.golang.org/protobuf/proto"
)

// TxNotification contains all data needed to process a transaction notification.
// EthTxBytes and EthTxHash come from the cache; NsRWS and Events come from the AllTxStreamer event.
type TxNotification struct {
	// From notification service
	BlockNum   uint64
	TxNum      uint64
	FabricTxID string
	Status     committerpb.Status

	// From cache
	EthTxBytes []byte
	EthTxHash  common.Hash // pre-computed; handlers that only need the hash skip UnmarshalBinary

	// From AllTxStreamer (IncludeReadWriteSets must be true)
	NsRWS  []blocks.NsReadWriteSet
	Events []byte
}

var notifLogger = flogging.MustGetLogger("gateway.core.notification")

// TxHandler defines the interface for handlers that process transaction notifications in batches.
type TxHandler interface {
	HandleTx(ctx context.Context, notifs []TxNotification) error
}

// AllTxBatchDispatcher implements notification.AllTxHandler. It bridges AllTxStreamer
// (which delivers every committed transaction) to the internal TxHandler chain.
//
// For each committed transaction event it:
//  1. Extracts the Ethereum transaction bytes from the event metadata.
//  2. Converts the event's Namespaces to NsRWS + Events.
//  3. Builds a TxNotification and dispatches it to all registered TxHandlers.
type AllTxBatchDispatcher struct {
	handlers []TxHandler
}

// NewAllTxBatchDispatcher creates a dispatcher that extracts EthTxBytes from event metadata
// and dispatches them as TxNotifications to the handler chain.
func NewAllTxBatchDispatcher(handlers ...TxHandler) *AllTxBatchDispatcher {
	// cache parameter is ignored (kept for API compatibility)
	return &AllTxBatchDispatcher{handlers: handlers}
}

// HandleBatch implements notification.AllTxHandler.
func (d *AllTxBatchDispatcher) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	notifLogger.Debugf("[BLOCK] block=%d total_events=%d", batch.BlockNumber, len(batch.Events))

	notifs := make([]TxNotification, 0, len(batch.Events))
	for _, event := range batch.Events {
		// Extract Ethereum transaction from metadata
		ethTxBytes, err := extractEthTxFromMetadata(event.Metadata)
		if err != nil {
			notifLogger.Debugf("Skipping tx %s: %v", event.TxID, err)
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

	notifLogger.Debugf("[NOTIFY] block=%d dispatching=%d/%d txs", batch.BlockNumber, len(notifs), len(batch.Events))

	for _, h := range d.handlers {
		if err := h.HandleTx(ctx, notifs); err != nil {
			panic(fmt.Errorf("handler failed: %w", err))
		}
	}

	return nil
}

// extractEthTxFromMetadata extracts the Ethereum transaction bytes from the event metadata.
// Metadata[0] contains the marshaled ChaincodeInput, which has Args[1] = eth tx bytes.
func extractEthTxFromMetadata(metadata [][]byte) ([]byte, error) {
	if len(metadata) == 0 {
		return nil, fmt.Errorf("no metadata")
	}

	var input peer.ChaincodeInput
	if err := proto.Unmarshal(metadata[0], &input); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	if len(input.Args) < 2 {
		return nil, fmt.Errorf("insufficient args: %d", len(input.Args))
	}

	return input.Args[1], nil
}

// NamespaceMultiplexer implements notification.AllTxHandler and routes notifications
// to the appropriate handlers based on namespace.
type NamespaceMultiplexer struct {
	handlers map[string][]TxHandler // namespace -> handlers
}

// NewNamespaceMultiplexer creates a new multiplexer for routing notifications by namespace.
func NewNamespaceMultiplexer() *NamespaceMultiplexer {
	return &NamespaceMultiplexer{
		handlers: make(map[string][]TxHandler),
	}
}

// RegisterHandlers registers handlers for a specific namespace.
func (m *NamespaceMultiplexer) RegisterHandlers(namespace string, handlers ...TxHandler) {
	m.handlers[namespace] = append(m.handlers[namespace], handlers...)
}

// HandleBatch implements notification.AllTxHandler by routing notifications to namespace-specific handlers.
func (m *NamespaceMultiplexer) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	notifLogger.Debugf("[MULTIPLEXER] block=%d total_events=%d", batch.BlockNumber, len(batch.Events))

	// Group notifications by namespace
	byNamespace := make(map[string][]TxNotification)

	for _, event := range batch.Events {
		// Extract Ethereum transaction from metadata
		ethTxBytes, err := extractEthTxFromMetadata(event.Metadata)
		if err != nil {
			notifLogger.Debugf("Skipping tx %s: %v", event.TxID, err)
			continue
		}

		var ethTx types.Transaction
		if err := ethTx.UnmarshalBinary(ethTxBytes); err != nil {
			return fmt.Errorf("unmarshal eth tx for %s: %w", event.TxID, err)
		}

		// Extract namespace from first namespace in event
		if len(event.Namespaces) == 0 {
			notifLogger.Debugf("Skipping tx %s: no namespaces", event.TxID)
			continue
		}
		ns := event.Namespaces[0].NsId

		nsrws, events := namespacesToNsRWS(event.Namespaces)

		notif := TxNotification{
			BlockNum:   event.BlockNum,
			TxNum:      uint64(event.TxNum),
			FabricTxID: event.TxID,
			Status:     event.Status,
			EthTxBytes: ethTxBytes,
			EthTxHash:  ethTx.Hash(),
			NsRWS:      nsrws,
			Events:     events,
		}

		byNamespace[ns] = append(byNamespace[ns], notif)
	}

	// Dispatch to handlers per namespace
	for ns, notifs := range byNamespace {
		if handlers, ok := m.handlers[ns]; ok {
			notifLogger.Debugf("[MULTIPLEXER] Dispatching %d notifs to namespace %s", len(notifs), ns)
			for _, h := range handlers {
				if err := h.HandleTx(ctx, notifs); err != nil {
					return fmt.Errorf("handler failed for ns %s: %w", ns, err)
				}
			}
		} else {
			notifLogger.Debugf("[MULTIPLEXER] No handlers registered for namespace %s", ns)
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
