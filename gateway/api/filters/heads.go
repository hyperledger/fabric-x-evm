/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"

	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

func errSubscriptionLimit(format string, args ...any) error {
	return rpcerr.LimitExceeded(format, args...)
}

// HeadsBuffer is how many committed blocks a slow newHeads client may lag
// before further notifications for that subscriber are dropped.
const HeadsBuffer = 16

// BlockQuerier loads a persisted block for newHeads payloads.
type BlockQuerier interface {
	LogQuerier
	GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error)
}

type headSub struct {
	id   uint64
	ch   chan *domain.Block
	conn any // WS connection key; nil when not attributable
}

// HeadSubscription is a live newHeads consumer.
type HeadSubscription struct {
	api *FilterAPI
	id  uint64
	ch  <-chan *domain.Block
}

// Chan returns committed blocks for this subscriber.
func (s *HeadSubscription) Chan() <-chan *domain.Block { return s.ch }

// Unsubscribe removes this subscriber. Safe to call more than once.
func (s *HeadSubscription) Unsubscribe() {
	if s == nil || s.api == nil {
		return
	}
	s.api.unsubscribeHeads(s.id)
	s.api = nil
}

// SubscribeHeads registers a buffered newHeads consumer with no connection key
// (tests / internal). Still respects the global subscription cap.
func (api *FilterAPI) SubscribeHeads(buffer int) (*HeadSubscription, error) {
	return api.SubscribeHeadsForConn(nil, buffer)
}

// SubscribeHeadsForConn registers a buffered newHeads consumer attributed to
// conn (stable per WS socket; callers should pass PeerInfo-based keys, not a
// per-call Notifier pointer). After Close the returned channel is already closed.
func (api *FilterAPI) SubscribeHeadsForConn(conn any, buffer int) (*HeadSubscription, error) {
	if buffer < 1 {
		buffer = HeadsBuffer
	}
	ch := make(chan *domain.Block, buffer)

	api.mu.Lock()
	defer api.mu.Unlock()
	if api.closed {
		close(ch)
		return &HeadSubscription{ch: ch}, nil
	}
	if len(api.headSubs) >= api.limits.MaxSubscriptionsGlobal {
		close(ch)
		return nil, errSubscriptionLimit("global subscription limit of %d reached", api.limits.MaxSubscriptionsGlobal)
	}
	if conn != nil && api.connSubs[conn] >= api.limits.MaxSubscriptionsPerConn {
		close(ch)
		return nil, errSubscriptionLimit("per-connection subscription limit of %d reached", api.limits.MaxSubscriptionsPerConn)
	}

	api.nextHeadID++
	id := api.nextHeadID
	api.headSubs[id] = &headSub{id: id, ch: ch, conn: conn}
	if conn != nil {
		api.connSubs[conn]++
	}
	return &HeadSubscription{api: api, id: id, ch: ch}, nil
}

// HeadSubscriberCount is the number of active newHeads subscribers (tests).
func (api *FilterAPI) HeadSubscriberCount() int {
	api.mu.Lock()
	defer api.mu.Unlock()
	return len(api.headSubs)
}

func (api *FilterAPI) unsubscribeHeads(id uint64) {
	api.mu.Lock()
	sub, ok := api.headSubs[id]
	if ok {
		delete(api.headSubs, id)
		if sub.conn != nil {
			api.connSubs[sub.conn]--
			if api.connSubs[sub.conn] <= 0 {
				delete(api.connSubs, sub.conn)
			}
		}
	}
	api.mu.Unlock()
	if ok {
		close(sub.ch)
	}
}

func (api *FilterAPI) closeHeadSubsLocked() {
	for id, sub := range api.headSubs {
		close(sub.ch)
		delete(api.headSubs, id)
	}
	clear(api.connSubs)
}

func (api *FilterAPI) fanOutHeads(b *domain.Block) {
	for _, s := range api.headSubs {
		select {
		case s.ch <- b:
		default:
			filterLogger.Warnf("newHeads subscriber %d falling behind; dropping notification", s.id)
		}
	}
}

func (api *FilterAPI) domainBlockFor(ctx context.Context, b blocks.Block) *domain.Block {
	if bq, ok := api.logs.(BlockQuerier); ok {
		if db, err := bq.GetBlockByNumber(ctx, b.Number, false); err == nil && db != nil {
			return db
		}
	}
	return &domain.Block{
		BlockNumber: b.Number,
		BlockHash:   b.Hash,
		ParentHash:  b.ParentHash,
		Timestamp:   b.Timestamp,
	}
}
