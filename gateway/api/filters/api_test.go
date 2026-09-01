/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"sync"
	"sync/atomic"
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

func waitDelivered(t *testing.T, api *FilterAPI, id rpc.ID, wantHashes int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		f := api.filters[id]
		n := 0
		if f != nil {
			n = len(f.hashes)
		}
		api.mu.Unlock()
		if n >= wantHashes {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d hashes on filter %s", wantHashes, id)
}

func TestBlockFilter_GetFilterChangesDrains(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, &stubLogs{head: 1})
	defer api.Close()

	id := api.NewBlockFilter(context.Background())
	_ = feed.Handle(context.Background(), testBlock(1, 0x11))
	waitDelivered(t, api, id, 1)

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
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	id1 := api.NewBlockFilter(context.Background())
	id2 := api.NewBlockFilter(context.Background())
	_ = feed.Handle(context.Background(), testBlock(2, 0x22))
	waitDelivered(t, api, id1, 1)
	waitDelivered(t, api, id2, 1)

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
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

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
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPIWithTimeout(feed, nil, 50*time.Millisecond)
	defer api.Close()

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
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, &stubLogs{head: 1})
	defer api.Close()

	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	id, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{
		Addresses: []common.Address{addr},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Directly inject matched/unmatched via onBlock with crafted logs by
	// temporarily using testDeliver is overkill; call match path through onBlock
	// with empty txs (no logs), then push via internal deliver of logs.
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
	feed := NewBlockFeed()
	defer feed.Close()
	wantAddr := []byte{0xaa}
	api := NewFilterAPI(feed, &stubLogs{
		head: 9,
		logs: []domain.Log{{
			Address:     wantAddr,
			BlockNumber: 3,
			BlockHash:   make([]byte, 32),
			TxHash:      make([]byte, 32),
		}},
	})
	defer api.Close()

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
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, &stubLogs{head: 1, err: context.DeadlineExceeded})
	defer api.Close()

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

	apiNil := NewFilterAPI(feed, nil)
	defer apiNil.Close()
	id2, _ := apiNil.NewFilter(context.Background(), gethfilters.FilterCriteria{})
	got, err := apiNil.GetFilterLogs(context.Background(), id2)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil backend: got %#v err %v", got, err)
	}
}

func TestBackpressure_HandleNeverBlocks(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	// Stall deliver by holding the filter mutex while Handle floods.
	var started sync.WaitGroup
	started.Add(1)
	go func() {
		api.mu.Lock()
		started.Done()
		time.Sleep(200 * time.Millisecond)
		api.mu.Unlock()
	}()
	started.Wait()

	deadline := 50 * time.Millisecond
	for i := 0; i < internalBuffer+8; i++ {
		start := time.Now()
		if err := feed.Handle(context.Background(), testBlock(uint64(i), byte(i))); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > deadline {
			t.Fatalf("Handle took %v, want <%v", took, deadline)
		}
	}
}

func TestShutdown_CloseExitsDrain(t *testing.T) {
	feed := NewBlockFeed()
	api := NewFilterAPI(feed, nil)

	done := make(chan struct{})
	go func() {
		feed.Close()
		api.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
}

func TestPanicIsolation(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	bad := api.NewBlockFilter(context.Background())
	good := api.NewBlockFilter(context.Background())

	api.mu.Lock()
	api.filters[bad].testDeliver = func(blocks.Block) { panic("boom") }
	api.mu.Unlock()

	_ = feed.Handle(context.Background(), testBlock(7, 0x77))
	waitDelivered(t, api, good, 1)

	got, err := api.GetFilterChanges(good)
	if err != nil {
		t.Fatal(err)
	}
	if hashes := got.([]common.Hash); len(hashes) != 1 {
		t.Fatalf("good filter affected: %#v", got)
	}

	// Bad filter still installed (panic isolated).
	api.mu.Lock()
	_, ok := api.filters[bad]
	api.mu.Unlock()
	if !ok {
		t.Fatal("panicking filter should remain installed")
	}
}

func TestLogMatchOffHotPath(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	// Many log filters with criteria; Handle must stay fast even if deliver is busy.
	for i := 0; i < 32; i++ {
		_, _ = api.NewFilter(context.Background(), gethfilters.FilterCriteria{
			Addresses: []common.Address{common.BytesToAddress([]byte{byte(i)})},
		})
	}

	var max atomic.Int64
	for i := 0; i < 20; i++ {
		start := time.Now()
		_ = feed.Handle(context.Background(), testBlock(uint64(i), byte(i)))
		took := time.Since(start).Nanoseconds()
		for {
			prev := max.Load()
			if took <= prev || max.CompareAndSwap(prev, took) {
				break
			}
		}
	}
	if time.Duration(max.Load()) > 50*time.Millisecond {
		t.Fatalf("Handle max latency %v with many log filters", time.Duration(max.Load()))
	}
}
