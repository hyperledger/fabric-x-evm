/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// HeadsAPI registers eth_subscribe newHeads and encodes payloads with the same
// RPCBlock shape as eth_getBlockByNumber(..., false).
type HeadsAPI struct {
	filters *filters.FilterAPI
}

// NewHeadsAPI wraps filterAPI for eth_subscribe newHeads registration.
func NewHeadsAPI(filterAPI *filters.FilterAPI) *HeadsAPI {
	return &HeadsAPI{filters: filterAPI}
}

func (h *HeadsAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	feedSub := h.filters.SubscribeHeads(filters.HeadsBuffer)

	go func() {
		defer feedSub.Unsubscribe()
		for {
			select {
			case b, ok := <-feedSub.Chan():
				if !ok {
					return
				}
				if err := notifier.Notify(rpcSub.ID, headPayload(b)); err != nil {
					return
				}
			case <-rpcSub.Err():
				return
			}
		}
	}()

	return rpcSub, nil
}

func headPayload(b *domain.Block) any {
	if b == nil {
		return nil
	}
	return RPCBlockFromDomain(b)
}
