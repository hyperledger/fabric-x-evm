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
	logs    LogQuerier
	timeout time.Duration

	mu      sync.Mutex
	filters map[rpc.ID]*filter

	quit chan struct{}
	wg   sync.WaitGroup
}

// NewFilterAPI starts the expiry loop. Pair with NewBlockFeed(api) for the handler.
func NewFilterAPI(logs LogQuerier) *FilterAPI {
	return newFilterAPI(logs, defaultTimeout)
}

// NewFilterAPIWithTimeout is for tests that need a short expiry.
func NewFilterAPIWithTimeout(logs LogQuerier, timeout time.Duration) *FilterAPI {
	return newFilterAPI(logs, timeout)
}

func newFilterAPI(logs LogQuerier, timeout time.Duration) *FilterAPI {
	api := &FilterAPI{
		logs:    logs,
		timeout: timeout,
		filters: make(map[rpc.ID]*filter),
		quit:    make(chan struct{}),
	}
	api.wg.Add(1)
	go api.timeoutLoop()
	return api
}

// Close stops the expiry loop.
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

// onBlock updates every installed filter under the API lock.
func (api *FilterAPI) onBlock(b blocks.Block) {
	api.mu.Lock()
	defer api.mu.Unlock()

	if len(api.filters) == 0 {
		return
	}

	blockLogs := logsFromBlock(b)
	hash := common.BytesToHash(b.Hash)
	for id := range api.filters {
		api.deliverOneLocked(id, b, hash, blockLogs)
	}
}

func (api *FilterAPI) deliverOneLocked(id rpc.ID, b blocks.Block, hash common.Hash, blockLogs []*types.Log) {
	defer func() {
		if rec := recover(); rec != nil {
			feedLogger.Warnf("filter %s panicked during deliver: %v", id, rec)
		}
	}()

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

func (api *FilterAPI) install(typ Type, crit gethfilters.FilterCriteria) rpc.ID {
	id := rpc.NewID()
	f := &filter{
		typ:      typ,
		deadline: time.NewTimer(api.timeout),
	}
	switch typ {
	case BlocksSubscription:
		f.hashes = make([]common.Hash, 0)
	case LogsSubscription:
		f.crit = crit
		f.logs = make([]*types.Log, 0)
	}
	api.filters[id] = f
	return id
}

// NewBlockFilter creates a filter that notifies on new block hashes.
func (api *FilterAPI) NewBlockFilter(ctx context.Context) rpc.ID {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.install(BlocksSubscription, gethfilters.FilterCriteria{})
}

// NewFilter creates a log filter. Criteria matching runs on each committed block.
func (api *FilterAPI) NewFilter(ctx context.Context, crit gethfilters.FilterCriteria) (rpc.ID, error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.install(LogsSubscription, crit), nil
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
		out[i] = DomainLogToTypes(l)
	}
	return out, nil
}

// DomainLogToTypes converts a store log to the geth RPC shape.
func DomainLogToTypes(l domain.Log) *types.Log {
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
