/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/notification"
)

var notifLogger = flogging.MustGetLogger("gateway.core.notification")

// TxHandler defines the interface for handlers that process transaction notifications in batches.
// This is the new interface that handlers should implement to work with the notification system.
// It mirrors blocks.BlockHandler but for batches of transactions.
type TxHandler interface {
	HandleTx(ctx context.Context, notifs []TxNotification) error
}

// NotificationDispatcher implements notification.TxStatusHandler and dispatches
// transaction notifications to a chain of TxHandlers after augmenting them with
// cached data. This is analogous to how the synchronizer dispatches blocks to
// BlockHandlers.
type NotificationDispatcher struct {
	cache    *PendingTxCache
	handlers []TxHandler
}

// NewNotificationDispatcher creates a new dispatcher that will augment notifications
// with cached data and dispatch to the provided handlers in order.
// Similar to how NewSynchronizer takes BlockHandlers, this takes TxHandlers.
func NewNotificationDispatcher(cache *PendingTxCache, handlers ...TxHandler) *NotificationDispatcher {
	return &NotificationDispatcher{
		cache:    cache,
		handlers: handlers,
	}
}

// Handle implements notification.TxStatusHandler.
// It processes a batch of transaction status events by:
// 1. Looking up cached data for each transaction
// 2. Creating TxNotification objects
// 3. Dispatching the batch to each handler in sequence
func (d *NotificationDispatcher) Handle(ctx context.Context, events []notification.TxStatusEvent) error {
	notifLogger.Debugf("Received %d transaction status events", len(events))

	// Build batch of notifications
	notifs := make([]TxNotification, 0, len(events))
	for _, event := range events {
		// Lookup cached data
		entry := d.cache.Get(event.TxID)
		if entry == nil {
			// This should never happen - indicates a bug in the system
			notifLogger.Errorf("Cache miss for txid %s", event.TxID)
			panic(fmt.Sprintf("cache miss for txid %s - this indicates a bug", event.TxID))
		}

		notifLogger.Debugf("Processing notification for txid %s (block=%d, txnum=%d, status=%v)",
			event.TxID, event.BlockNum, event.TxNum, event.Status)

		// Create notification with combined data
		notif := TxNotification{
			BlockNum:   event.BlockNum,
			TxNum:      uint64(event.TxNum),
			FabricTxID: event.TxID,
			Status:     event.Status,
			EthTxBytes: entry.EthTxBytes,
			NsRWS:      entry.NsRWS,
			Events:     entry.Events,
		}
		notifs = append(notifs, notif)
	}

	// Dispatch batch to all handlers in sequence
	for _, handler := range d.handlers {
		if err := handler.HandleTx(ctx, notifs); err != nil {
			panic(fmt.Errorf("handler failed for batch: %w", err))
		}
	}

	return nil
}

// CleanupHandler removes completed transactions from the cache.
// This should be the last handler in the chain.
type CleanupHandler struct {
	cache *PendingTxCache
}

// NewCleanupHandler creates a new cleanup handler.
func NewCleanupHandler(cache *PendingTxCache) *CleanupHandler {
	return &CleanupHandler{cache: cache}
}

// HandleTx removes the transactions from the cache.
func (h *CleanupHandler) HandleTx(ctx context.Context, notifs []TxNotification) error {
	for _, notif := range notifs {
		h.cache.Delete(notif.FabricTxID)
	}
	return nil
}
