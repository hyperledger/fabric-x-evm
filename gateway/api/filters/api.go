/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

const defaultTimeout = 5 * time.Minute

// Type of polling filter.
type Type byte

const (
	UnknownSubscription Type = iota
	LogsSubscription
	BlocksSubscription
)

var errFilterNotFound = fmt.Errorf("filter not found")

// LogQuerier is the historical log path used by eth_getFilterLogs.
// Satisfied by gateway/api.Backend without importing that package.
type LogQuerier interface {
	GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error)
	BlockNumber(ctx context.Context) (uint64, error)
}

type filter struct {
	typ      Type
	deadline *time.Timer
	hashes   []common.Hash
	crit     gethfilters.FilterCriteria
	logs     []*types.Log

	// testDeliver, if set, runs inside the per-filter recover scope instead of
	// the normal deliver path. Used to prove panic isolation.
	testDeliver func(b blocks.Block)
}

// FilterAPI exposes the eth_*Filter JSON-RPC methods.
type FilterAPI struct {
	feed    *BlockFeed
	logs    LogQuerier
	timeout time.Duration

	mu      sync.Mutex
	filters map[rpc.ID]*filter

	quit chan struct{}
	wg   sync.WaitGroup
}

// NewFilterAPI wires the feed sink and starts the expiry loop.
func NewFilterAPI(feed *BlockFeed, logs LogQuerier) *FilterAPI {
	return newFilterAPI(feed, logs, defaultTimeout)
}

// NewFilterAPIWithTimeout is for tests that need a short expiry.
func NewFilterAPIWithTimeout(feed *BlockFeed, logs LogQuerier, timeout time.Duration) *FilterAPI {
	return newFilterAPI(feed, logs, timeout)
}

func newFilterAPI(feed *BlockFeed, logs LogQuerier, timeout time.Duration) *FilterAPI {
	api := &FilterAPI{
		feed:    feed,
		logs:    logs,
		timeout: timeout,
		filters: make(map[rpc.ID]*filter),
		quit:    make(chan struct{}),
	}
	feed.SetSink(api)
	api.wg.Add(1)
	go api.timeoutLoop()
	return api
}

// Close stops the expiry loop. BlockFeed.Close is separate (App.Shutdown).
func (api *FilterAPI) Close() {
	select {
	case <-api.quit:
	default:
		close(api.quit)
	}
	api.wg.Wait()
}

func (api *FilterAPI) timeoutLoop() {
	defer api.wg.Done()
	ticker := time.NewTicker(api.timeout)
	defer ticker.Stop()
	for {
		select {
		case <-api.quit:
			return
		case <-api.feed.Done():
			return
		case <-ticker.C:
			api.mu.Lock()
			for id, f := range api.filters {
				select {
				case <-f.deadline.C:
					f.deadline.Stop()
					delete(api.filters, id)
				default:
				}
			}
			api.mu.Unlock()
		}
	}
}

// onBlock is called from BlockFeed's drain goroutine.
func (api *FilterAPI) onBlock(b blocks.Block) {
	api.mu.Lock()
	ids := make([]rpc.ID, 0, len(api.filters))
	for id := range api.filters {
		ids = append(ids, id)
	}
	api.mu.Unlock()

	blockLogs := logsFromBlock(b)
	hash := common.BytesToHash(b.Hash)

	for _, id := range ids {
		api.deliverOne(id, b, hash, blockLogs)
	}
}

func (api *FilterAPI) deliverOne(id rpc.ID, b blocks.Block, hash common.Hash, blockLogs []*types.Log) {
	defer func() {
		if rec := recover(); rec != nil {
			feedLogger.Warnf("filter %s panicked during deliver: %v", id, rec)
		}
	}()

	api.mu.Lock()
	defer api.mu.Unlock()
	f, ok := api.filters[id]
	if !ok {
		return
	}
	if f.testDeliver != nil {
		f.testDeliver(b)
		return
	}
	switch f.typ {
	case BlocksSubscription:
		f.hashes = append(f.hashes, hash)
	case LogsSubscription:
		matched := matchLogs(blockLogs, f.crit)
		if len(matched) > 0 {
			f.logs = append(f.logs, matched...)
		}
	}
}

// NewHeads creates a subscription that pushes a header each time a block commits.
// go-ethereum's rpc.Server reflects this method and exposes eth_subscribe/eth_unsubscribe.
func (api *FilterAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	feedSub := api.feed.Subscribe(16)

	go func() {
		defer feedSub.Unsubscribe()
		for {
			select {
			case b, ok := <-feedSub.Chan():
				if !ok {
					return
				}
				if err := notifier.Notify(rpcSub.ID, headFromBlock(b)); err != nil {
					return
				}
			case <-rpcSub.Err():
				return
			}
		}
	}()

	return rpcSub, nil
}

// NewBlockFilter creates a filter that notifies on new block hashes.
func (api *FilterAPI) NewBlockFilter(ctx context.Context) rpc.ID {
	api.mu.Lock()
	defer api.mu.Unlock()
	id := rpc.NewID()
	api.filters[id] = &filter{
		typ:      BlocksSubscription,
		deadline: time.NewTimer(api.timeout),
		hashes:   make([]common.Hash, 0),
	}
	return id
}

// NewFilter creates a log filter. Criteria matching runs on each committed block.
func (api *FilterAPI) NewFilter(ctx context.Context, crit gethfilters.FilterCriteria) (rpc.ID, error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	id := rpc.NewID()
	api.filters[id] = &filter{
		typ:      LogsSubscription,
		deadline: time.NewTimer(api.timeout),
		crit:     crit,
		logs:     make([]*types.Log, 0),
	}
	return id, nil
}

// UninstallFilter removes a filter by id. Returns true if it existed.
func (api *FilterAPI) UninstallFilter(id rpc.ID) bool {
	api.mu.Lock()
	defer api.mu.Unlock()
	f, ok := api.filters[id]
	if !ok {
		return false
	}
	delete(api.filters, id)
	f.deadline.Stop()
	return true
}

// GetFilterChanges drains buffered hashes or logs since the last poll.
func (api *FilterAPI) GetFilterChanges(id rpc.ID) (interface{}, error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	f, ok := api.filters[id]
	if !ok {
		return []interface{}{}, errFilterNotFound
	}
	if !f.deadline.Stop() {
		select {
		case <-f.deadline.C:
		default:
		}
	}
	f.deadline.Reset(api.timeout)

	switch f.typ {
	case BlocksSubscription:
		hashes := f.hashes
		f.hashes = nil
		if hashes == nil {
			hashes = []common.Hash{}
		}
		return hashes, nil
	case LogsSubscription:
		logs := f.logs
		f.logs = nil
		if logs == nil {
			logs = []*types.Log{}
		}
		return logs, nil
	default:
		return []interface{}{}, errFilterNotFound
	}
}

// GetFilterLogs returns historical logs for a log filter's criteria.
func (api *FilterAPI) GetFilterLogs(ctx context.Context, id rpc.ID) ([]*types.Log, error) {
	api.mu.Lock()
	f, ok := api.filters[id]
	if !ok || f.typ != LogsSubscription {
		api.mu.Unlock()
		return nil, errFilterNotFound
	}
	crit := f.crit
	api.mu.Unlock()

	if api.logs == nil {
		return []*types.Log{}, nil
	}
	head, err := api.logs.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	query := CriteriaToLogFilter(crit, head)
	domainLogs, err := api.logs.GetLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Log, len(domainLogs))
	for i, l := range domainLogs {
		out[i] = domainLogToTypes(l)
	}
	return out, nil
}

func domainLogToTypes(l domain.Log) *types.Log {
	topics := make([]common.Hash, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = common.BytesToHash(t)
	}
	return &types.Log{
		Address:        common.BytesToAddress(l.Address),
		Topics:         topics,
		Data:           l.Data,
		BlockNumber:    l.BlockNumber,
		BlockHash:      common.BytesToHash(l.BlockHash),
		BlockTimestamp: uint64(l.Timestamp),
		TxHash:         common.BytesToHash(l.TxHash),
		TxIndex:        uint(l.TxIndex),
		Index:          uint(l.LogIndex),
	}
}
