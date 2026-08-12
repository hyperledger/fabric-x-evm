/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package hybridx implements the two-phase startup strategy for fabric-x nodes.
//
// On startup both the delivery service and the notification service are started
// concurrently.  The notification service tracks the latest committed block it
// has seen (nNot).  The delivery service processes blocks and exposes its last
// processed block via nDel.  The switch from delivery to notification is decided
// inside the notification gate itself, in a single HandleBatch call, eliminating
// any gap between the two phases:
//
//  1. The gate receives a batch for block B and updates nNot = B.
//  2. If nDel >= nNot it atomically flips switched and forwards the batch.
//  3. If nDel < nNot it discards the batch content (delivery is still behind).
//
// Because the decision and the first forward happen in the same HandleBatch call
// there is no window in which a batch could be lost.
//
// HybridSynchronizer satisfies the app.Synchronizer interface (Start + Ready)
// and plugs directly into NewGatewaySynchronizer as the fabric-x implementation.
package hybridx

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"

	evmcommon "github.com/hyperledger/fabric-x-evm/common"
)

// deliverySyncer is the subset of *network.Synchronizer used by HybridSynchronizer.
// Extracted as an interface so tests can supply a fake without a real gRPC connection.
type deliverySyncer interface {
	Start(ctx context.Context) error
	Ready() error
	BlockHeight(ctx context.Context) (uint64, error)
}

// HybridSynchronizer implements the two-phase startup strategy described in
// the package doc.  It satisfies the app.Synchronizer interface.
type HybridSynchronizer struct {
	namespace string
	logger    sdk.Logger
	handlers  []blocks.BlockHandler

	delivery  deliverySyncer
	notifPeer notification.AllTxPeer
	notifReq  *notification.StreamAllRequest
}

// New constructs a HybridSynchronizer.
// handlers is the initial handler chain fed by both phases.
func New(
	db network.BlockHeightReader,
	channel, namespace string,
	conf network.PeerConf,
	signer sdk.Signer,
	logger sdk.Logger,
	handlers ...blocks.BlockHandler,
) (*HybridSynchronizer, error) {
	h := &HybridSynchronizer{
		namespace: namespace,
		logger:    logger,
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
	}

	delivery, err := nfabx.NewSynchronizer(db, channel, conf, signer, logger, &deliveryShim{h: h})
	if err != nil {
		return nil, fmt.Errorf("hybridx: create delivery synchronizer: %w", err)
	}

	notifPeer, err := nfabx.NewPeer(conf, channel, signer)
	if err != nil {
		return nil, fmt.Errorf("hybridx: create notification peer: %w", err)
	}

	h.delivery = delivery
	h.notifPeer = notifPeer
	h.notifReq = &notification.StreamAllRequest{
		FilterNamespaces:     []string{namespace},
		IncludeReadWriteSets: true,
		IncludeMetadata:      true,
	}
	return h, nil
}

// newWithDeps constructs a HybridSynchronizer from pre-built dependencies.
// Used by tests to inject fakes without needing a real gRPC connection.
func newWithDeps(
	delivery deliverySyncer,
	notifPeer notification.AllTxPeer,
	notifReq *notification.StreamAllRequest,
	logger sdk.Logger,
	handlers ...blocks.BlockHandler,
) *HybridSynchronizer {
	return &HybridSynchronizer{
		logger:    logger,
		handlers:  append([]blocks.BlockHandler(nil), handlers...),
		delivery:  delivery,
		notifPeer: notifPeer,
		notifReq:  notifReq,
	}
}

// dispatch calls Handle on every handler in the chain.
// Called by both the deliveryShim and the notifGate.
func (h *HybridSynchronizer) dispatch(ctx context.Context, b blocks.Block) error {
	for _, bh := range h.handlers {
		if err := bh.Handle(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

// Start runs the two-phase startup and then blocks until ctx is cancelled.
//
// Both services start concurrently.  A notifGate sits between the streamer and
// the handler chain.  It owns nDel (written by a delivery-watcher goroutine
// after each Ready() transition) and nNot (written on every incoming batch).
// The switch is decided inside HandleBatch — atomically with forwarding the first
// live batch — so no gap can arise between the two phases.
func (h *HybridSynchronizer) Start(ctx context.Context) error {
	// switched flips 0→1 exactly once, inside HandleBatch, when nDel >= nNot.
	var switched atomic.Uint32

	// nDel is the last block number processed by the delivery synchronizer.
	// Written by the delivery-watcher goroutine; read by the notif gate.
	var nDel atomic.Uint64

	// ── Notification service ────────────────────────────────────────────────
	gate := &notifGate{
		nDel:       &nDel,
		switched:   &switched,
		hybrid:     h,
		dispatcher: evmcommon.NewAllTxBatchDispatcher(&hybridAdapter{h: h}),
		logger:     h.logger,
	}
	streamer := notification.NewAllTxStreamer(h.notifPeer, []notification.AllTxHandler{gate}, h.logger)

	notifErrCh := make(chan error, 1)
	go func() {
		const retryDelay = time.Second
		for {
			if ctx.Err() != nil {
				notifErrCh <- nil
				return
			}
			if err := streamer.Stream(ctx, h.notifReq); err != nil && ctx.Err() == nil {
				h.logger.Warnf("hybridx: notification stream error: %v — restarting in %s", err, retryDelay)
				select {
				case <-ctx.Done():
					notifErrCh <- nil
					return
				case <-time.After(retryDelay):
				}
			}
		}
	}()

	// ── Delivery service ────────────────────────────────────────────────────
	deliveryCtx, deliveryCancel := context.WithCancel(ctx)
	defer deliveryCancel()

	deliveryDone := make(chan struct{})
	go func() {
		defer func() {
			h.delivery = nil // allow GC of the peer and all delivery resources
			close(deliveryDone)
		}()
		if err := h.delivery.Start(deliveryCtx); err != nil && deliveryCtx.Err() == nil {
			h.logger.Warnf("hybridx: delivery error: %v", err)
		}
	}()

	// Delivery-watcher: once the delivery synchronizer reaches Ready, poll its
	// BlockHeight to keep nDel current so the gate can compare accurately.
	// When the gate has already switched we cancel the delivery synchronizer.
	go func() {
		// Wait for the delivery synchronizer to reach Ready.
		for {
			select {
			case <-ctx.Done():
				return
			case <-deliveryDone:
				return
			case <-time.After(100 * time.Millisecond):
			}
			if h.delivery != nil && h.delivery.Ready() == nil {
				break
			}
		}

		// Keep nDel updated until the gate has switched.
		for {
			select {
			case <-ctx.Done():
				return
			case <-deliveryDone:
				return
			case <-time.After(100 * time.Millisecond):
			}
			if switched.Load() == 1 {
				deliveryCancel()
				return
			}
			if h.delivery == nil {
				return
			}
			height, err := h.delivery.BlockHeight(ctx)
			if err == nil && height > 0 {
				nDel.Store(height - 1) // BlockHeight = last+1, so last = height-1
			}
		}
	}()

	<-ctx.Done()
	return <-notifErrCh
}

// Ready proxies to the delivery synchronizer until the switch; afterwards
// returns nil since the notification stream is always considered ready.
//
// Note: h.delivery is set to nil from the delivery goroutine in Start; since
// there is no mutex on this field the read here may race in theory, but the
// only consequence is an extra call to delivery.Ready() which is benign.
func (h *HybridSynchronizer) Ready() error {
	if h.delivery == nil {
		return nil
	}
	return h.delivery.Ready()
}

// deliveryShim is a blocks.BlockHandler registered with the delivery synchronizer.
// It delegates to h.dispatch so that the live handler chain (including any
// handlers added or removed via AddHandler/RemoveHandler) is always used.
type deliveryShim struct{ h *HybridSynchronizer }

func (s *deliveryShim) Handle(ctx context.Context, b blocks.Block) error {
	return s.h.dispatch(ctx, b)
}

// notifGate is a notification.AllTxHandler that gates the handler chain.
//
// For each incoming batch it:
//  1. If switched: converts the batch to a blocks.Block and dispatches it.
//  2. If not switched but nDel >= batch.BlockNumber: flips switched and dispatches
//     — this is the gap-free handoff point.
//  3. Otherwise: discards the batch (delivery is still behind).
type notifGate struct {
	nDel       *atomic.Uint64
	switched   *atomic.Uint32
	hybrid     *HybridSynchronizer
	dispatcher *evmcommon.AllTxBatchDispatcher
	logger     sdk.Logger
}

// hybridAdapter adapts HybridSynchronizer.dispatch to evmcommon.BlockHandler.
type hybridAdapter struct{ h *HybridSynchronizer }

func (a *hybridAdapter) Handle(ctx context.Context, b blocks.Block) error {
	return a.h.dispatch(ctx, b)
}

func (g *notifGate) HandleBatch(ctx context.Context, batch notification.AllTxBatch) error {
	if g.switched.Load() == 1 {
		return g.dispatchBatch(ctx, batch)
	}

	// Check whether delivery has caught up with this batch's block number.
	if g.nDel.Load() >= batch.BlockNumber {
		g.switched.Store(1)
		g.logger.Infof("hybridx: switching to notification at block %d", batch.BlockNumber)
		return g.dispatchBatch(ctx, batch)
	}

	return nil // delivery is still behind; discard
}

// dispatchBatch converts an AllTxBatch to a blocks.Block via AllTxBatchDispatcher
// and dispatches it through the hybrid's handler chain.
// The dispatcher is allocated once per gate in Start and stored here.
func (g *notifGate) dispatchBatch(ctx context.Context, batch notification.AllTxBatch) error {
	return g.dispatcher.HandleBatch(ctx, batch)
}
