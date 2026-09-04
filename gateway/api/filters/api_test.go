/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

type stubLogs struct {
	head uint64
	logs []domain.Log
	err  error
}

func (s *stubLogs) GetLogs(context.Context, domain.LogFilter) ([]domain.Log, error) {
	return s.logs, s.err
}

func (s *stubLogs) BlockNumber(context.Context) (uint64, error) {
	return s.head, s.err
}

func testBlock(num uint64, hash byte) blocks.Block {
	h := make([]byte, 32)
	h[31] = hash
	return blocks.Block{Number: num, Hash: h}
}

func newTestSystem(t *testing.T, logs LogQuerier) (*BlockFeed, *FilterAPI) {
	t.Helper()
	api := NewFilterAPI(logs)
	feed := NewBlockFeed(api)
	t.Cleanup(feed.Close)
	return feed, api
}

func TestBlockFilter_GetFilterChangesDrains(t *testing.T) {
	feed, api := newTestSystem(t, &stubLogs{head: 1})

	id := api.NewBlockFilter(context.Background())
	if err := feed.Handle(context.Background(), testBlock(1, 0x11)); err != nil {
		t.Fatal(err)
	}

	got, err := api.GetFilterChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	hashes, ok := got.([]common.Hash)
	if !ok || len(hashes) != 1 || hashes[0][31] != 0x11 {
		t.Fatalf("first poll = %#v", got)
	}

	got, err = api.GetFilterChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	hashes, ok = got.([]common.Hash)
	if !ok || len(hashes) != 0 {
		t.Fatalf("second poll want empty, got %#v", got)
	}
}

func TestBlockFilter_MultipleFiltersIndependent(t *testing.T) {
	feed, api := newTestSystem(t, nil)

	id1 := api.NewBlockFilter(context.Background())
	id2 := api.NewBlockFilter(context.Background())
	if err := feed.Handle(context.Background(), testBlock(2, 0x22)); err != nil {
		t.Fatal(err)
	}

	for _, id := range []rpc.ID{id1, id2} {
		got, err := api.GetFilterChanges(id)
		if err != nil {
			t.Fatal(err)
		}
		hashes := got.([]common.Hash)
		if len(hashes) != 1 || hashes[0][31] != 0x22 {
			t.Fatalf("filter %s: %#v", id, got)
		}
	}
}

func TestUninstallFilter(t *testing.T) {
	_, api := newTestSystem(t, nil)

	id := api.NewBlockFilter(context.Background())
	if !api.UninstallFilter(id) {
		t.Fatal("expected uninstall true")
	}
	if api.UninstallFilter(id) {
		t.Fatal("second uninstall should be false")
	}
	if _, err := api.GetFilterChanges(id); err == nil {
		t.Fatal("expected filter not found")
	}
}

func TestFilterExpiry(t *testing.T) {
	api := NewFilterAPIWithTimeout(nil, 50*time.Millisecond)
	feed := NewBlockFeed(api)
	t.Cleanup(feed.Close)

	id := api.NewBlockFilter(context.Background())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		_, ok := api.filters[id]
		api.mu.Unlock()
		if !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("filter was not expired")
}

func TestLogFilter_MatchAndMiss(t *testing.T) {
	_, api := newTestSystem(t, &stubLogs{head: 1})

	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	id, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{
		Addresses: []common.Address{addr},
	})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	f := api.filters[id]
	matched := matchLogs([]*types.Log{
		{Address: addr, BlockNumber: 1, Topics: nil},
		{Address: common.HexToAddress("0xbb"), BlockNumber: 1},
	}, f.crit)
	f.logs = append(f.logs, matched...)
	api.mu.Unlock()

	got, err := api.GetFilterChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	logs := got.([]*types.Log)
	if len(logs) != 1 || logs[0].Address != addr {
		t.Fatalf("got %#v", got)
	}
}

func TestGetFilterLogs_Historical(t *testing.T) {
	wantAddr := []byte{0xaa}
	_, api := newTestSystem(t, &stubLogs{
		head: 9,
		logs: []domain.Log{{
			Address:     wantAddr,
			BlockNumber: 3,
			BlockHash:   make([]byte, 32),
			TxHash:      make([]byte, 32),
		}},
	})

	id, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{
		Addresses: []common.Address{common.BytesToAddress(wantAddr)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.GetFilterLogs(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Address != common.BytesToAddress(wantAddr) {
		t.Fatalf("got %#v", got)
	}
}

func TestGetFilterLogs_Errors(t *testing.T) {
	_, api := newTestSystem(t, &stubLogs{head: 1, err: context.DeadlineExceeded})

	blockID := api.NewBlockFilter(context.Background())
	if _, err := api.GetFilterLogs(context.Background(), blockID); err == nil {
		t.Fatal("block filter should reject GetFilterLogs")
	}
	if _, err := api.GetFilterLogs(context.Background(), rpc.NewID()); err == nil {
		t.Fatal("missing filter should error")
	}

	id, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.GetFilterLogs(context.Background(), id); err == nil {
		t.Fatal("backend error should surface")
	}

	apiNil := NewFilterAPI(nil)
	feedNil := NewBlockFeed(apiNil)
	t.Cleanup(feedNil.Close)
	id2, _ := apiNil.NewFilter(context.Background(), gethfilters.FilterCriteria{})
	got, err := apiNil.GetFilterLogs(context.Background(), id2)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil backend: got %#v err %v", got, err)
	}
}

func TestHandle_UpdatesSynchronously(t *testing.T) {
	feed, api := newTestSystem(t, nil)
	id := api.NewBlockFilter(context.Background())

	if err := feed.Handle(context.Background(), testBlock(3, 0x33)); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	n := len(api.filters[id].hashes)
	api.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected hash buffered before Handle returned, got %d", n)
	}
}

func TestShutdown_CloseReturns(t *testing.T) {
	api := NewFilterAPI(nil)
	feed := NewBlockFeed(api)

	done := make(chan struct{})
	go func() {
		feed.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
}

func TestPanicIsolation(t *testing.T) {
	feed, api := newTestSystem(t, nil)

	bad := api.NewBlockFilter(context.Background())
	good := api.NewBlockFilter(context.Background())

	api.mu.Lock()
	api.filters[bad].testDeliver = func(blocks.Block) { panic("boom") }
	api.mu.Unlock()

	if err := feed.Handle(context.Background(), testBlock(7, 0x77)); err != nil {
		t.Fatal(err)
	}

	got, err := api.GetFilterChanges(good)
	if err != nil {
		t.Fatal(err)
	}
	if hashes := got.([]common.Hash); len(hashes) != 1 {
		t.Fatalf("good filter affected: %#v", got)
	}

	api.mu.Lock()
	_, ok := api.filters[bad]
	api.mu.Unlock()
	if !ok {
		t.Fatal("panicking filter should remain installed")
	}
}

func TestOnBlock_EmptyFiltersReturnsEarly(t *testing.T) {
	feed, _ := newTestSystem(t, nil)
	if err := feed.Handle(context.Background(), testBlock(1, 1)); err != nil {
		t.Fatal(err)
	}
}
