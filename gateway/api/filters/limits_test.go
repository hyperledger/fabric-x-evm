/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
)

func limitsWith(mutate func(*Limits)) Limits {
	l := DefaultLimits
	mutate(&l)
	return l
}

func TestFilterCap_GlobalConcurrent(t *testing.T) {
	api := NewFilterAPIWithLimits(nil, limitsWith(func(l *Limits) { l.MaxFilters = 2 }))
	t.Cleanup(api.Close)

	id1 := mustNewBlockFilter(t, api)
	id2 := mustNewBlockFilter(t, api)

	_, err := api.NewBlockFilter(context.Background())
	if err == nil {
		t.Fatal("expected filter limit error")
	}
	var rpcErr interface{ ErrorCode() int }
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != rpcerr.CodeLimitExceeded {
		t.Fatalf("want limit-exceeded RPC error, got %v", err)
	}

	if !api.UninstallFilter(id1) {
		t.Fatal("uninstall")
	}
	id3, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{})
	if err != nil {
		t.Fatalf("slot should free after uninstall: %v", err)
	}
	if id3 == id2 {
		t.Fatal("expected a new filter id")
	}
}

func TestFilterCap_DefaultAllowsNormalUse(t *testing.T) {
	api := NewFilterAPI(nil)
	t.Cleanup(api.Close)

	id := mustNewBlockFilter(t, api)
	if id == "" {
		t.Fatal("empty id")
	}
	if _, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{}); err != nil {
		t.Fatal(err)
	}
}

func TestFilterCap_ExpiryFreesSlot(t *testing.T) {
	api := NewFilterAPIWithTimeoutAndLimits(nil, 40*time.Millisecond, limitsWith(func(l *Limits) { l.MaxFilters = 1 }))
	t.Cleanup(api.Close)

	_ = mustNewBlockFilter(t, api)
	if _, err := api.NewBlockFilter(context.Background()); err == nil {
		t.Fatal("expected cap while filter live")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		n := len(api.filters)
		api.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := api.NewBlockFilter(context.Background()); err != nil {
		t.Fatalf("slot should free after expiry: %v", err)
	}
}

func TestFilterCap_ZeroMeansZero(t *testing.T) {
	api := NewFilterAPIWithLimits(nil, Limits{
		MaxFilters:              0,
		MaxSubscriptionsPerConn: DefaultLimits.MaxSubscriptionsPerConn,
		MaxSubscriptionsGlobal:  DefaultLimits.MaxSubscriptionsGlobal,
		MaxFilterBuffer:         DefaultLimits.MaxFilterBuffer,
	})
	t.Cleanup(api.Close)
	if _, err := api.NewBlockFilter(context.Background()); err == nil {
		t.Fatal("MaxFilters=0 should reject new filters")
	}
}

func TestFilterBuffer_DropsOldest(t *testing.T) {
	api := NewFilterAPIWithLimits(nil, limitsWith(func(l *Limits) { l.MaxFilterBuffer = 2 }))
	t.Cleanup(api.Close)

	id := mustNewBlockFilter(t, api)
	for i := byte(1); i <= 4; i++ {
		if err := api.Handle(context.Background(), testBlock(uint64(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := api.GetFilterChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	hashes, ok := got.([]common.Hash)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if len(hashes) != 2 {
		t.Fatalf("len=%d, want 2 (oldest dropped)", len(hashes))
	}
	if hashes[0][31] != 3 || hashes[1][31] != 4 {
		t.Fatalf("hashes=%v, want last two (3,4)", hashes)
	}
}

func TestSubscriptionCap_PerConnection(t *testing.T) {
	api := NewFilterAPIWithLimits(nil, limitsWith(func(l *Limits) {
		l.MaxSubscriptionsPerConn = 1
		l.MaxSubscriptionsGlobal = 10
	}))
	t.Cleanup(api.Close)

	connA := &struct{ name string }{"a"}
	connB := &struct{ name string }{"b"}

	sub1, err := api.SubscribeHeadsForConn(connA, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.SubscribeHeadsForConn(connA, 2)
	if err == nil {
		t.Fatal("expected per-connection limit error")
	}
	var rpcErr interface{ ErrorCode() int }
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != rpcerr.CodeLimitExceeded {
		t.Fatalf("want limit-exceeded RPC error, got %v", err)
	}

	sub2, err := api.SubscribeHeadsForConn(connB, 2)
	if err != nil {
		t.Fatalf("other connection should still subscribe: %v", err)
	}

	sub1.Unsubscribe()
	sub3, err := api.SubscribeHeadsForConn(connA, 2)
	if err != nil {
		t.Fatalf("slot should free after unsubscribe: %v", err)
	}
	sub2.Unsubscribe()
	sub3.Unsubscribe()
}

func TestSubscriptionCap_Global(t *testing.T) {
	api := NewFilterAPIWithLimits(nil, limitsWith(func(l *Limits) {
		l.MaxSubscriptionsPerConn = 5
		l.MaxSubscriptionsGlobal = 2
	}))
	t.Cleanup(api.Close)

	if _, err := api.SubscribeHeadsForConn("c1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := api.SubscribeHeadsForConn("c2", 1); err != nil {
		t.Fatal(err)
	}
	_, err := api.SubscribeHeadsForConn("c3", 1)
	if err == nil {
		t.Fatal("expected global subscription limit error")
	}
}

func TestSubscriptionCap_DefaultAllowsSingle(t *testing.T) {
	api := NewFilterAPI(nil)
	t.Cleanup(api.Close)
	sub, err := api.SubscribeHeads(2)
	if err != nil {
		t.Fatal(err)
	}
	sub.Unsubscribe()
}
