/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

func TestSubscribe_ReceiveAndUnsubscribe(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()

	sub := feed.Subscribe(4)
	if feed.SubscriberCount() != 1 {
		t.Fatalf("count=%d", feed.SubscriberCount())
	}

	_ = feed.Handle(context.Background(), testBlock(9, 0x99))
	select {
	case b := <-sub.Chan():
		if b.Number != 9 || b.Hash[31] != 0x99 {
			t.Fatalf("got %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for block")
	}

	sub.Unsubscribe()
	sub.Unsubscribe()
	if feed.SubscriberCount() != 0 {
		t.Fatalf("count after unsub=%d", feed.SubscriberCount())
	}
}

func TestSubscribe_BackpressureDoesNotBlockHandle(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()

	_ = feed.Subscribe(1)
	_ = feed.Handle(context.Background(), testBlock(1, 1))

	deadline := 50 * time.Millisecond
	for i := 0; i < 64; i++ {
		start := time.Now()
		if err := feed.Handle(context.Background(), testBlock(uint64(i+2), byte(i))); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > deadline {
			t.Fatalf("Handle took %v", took)
		}
	}
}

func TestSubscribe_CloseClearsSubscribers(t *testing.T) {
	feed := NewBlockFeed()
	sub := feed.Subscribe(2)
	if feed.SubscriberCount() != 1 {
		t.Fatal("expected one subscriber")
	}
	feed.Close()
	if feed.SubscriberCount() != 0 {
		t.Fatalf("count=%d after Close", feed.SubscriberCount())
	}
	select {
	case _, ok := <-sub.Chan():
		if ok {
			t.Fatal("channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
}

func TestHeadFromBlock(t *testing.T) {
	b := blocks.Block{
		Number:     42,
		Hash:       bytes32(0xab),
		ParentHash: bytes32(0xcd),
		Timestamp:  1_700_000_000,
	}
	h := headFromBlock(b)
	if h.Hash != common.BytesToHash(bytes32(0xab)) {
		t.Fatalf("hash=%s", h.Hash)
	}
	if (*big.Int)(h.Number).Uint64() != 42 {
		t.Fatalf("number=%s", h.Number)
	}
	if uint64(h.Timestamp) != 1_700_000_000 {
		t.Fatalf("ts=%d", h.Timestamp)
	}
}

func TestNewHeads_RequiresNotifier(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	_, err := api.NewHeads(context.Background())
	if err != rpc.ErrNotificationsUnsupported {
		t.Fatalf("err=%v", err)
	}
}

func TestNewHeads_WSRoundTrip(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()

	srv := rpc.NewServer()
	api := NewFilterAPI(feed, nil)
	defer api.Close()
	if err := srv.RegisterName("eth", api); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.WebsocketHandler([]string{"*"})}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := rpc.DialContext(ctx, "ws://"+ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	heads := make(chan *rpcHead, 4)
	sub, err := client.Subscribe(ctx, "eth", heads, "newHeads")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	deadline := time.Now().Add(2 * time.Second)
	for feed.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if feed.SubscriberCount() != 1 {
		t.Fatalf("subscriber count=%d", feed.SubscriberCount())
	}

	_ = feed.Handle(context.Background(), blocks.Block{
		Number:     7,
		Hash:       bytes32(0x77),
		ParentHash: bytes32(0x66),
		Timestamp:  99,
	})

	select {
	case h := <-heads:
		if h.Hash != common.BytesToHash(bytes32(0x77)) {
			t.Fatalf("hash=%s", h.Hash)
		}
		if (*big.Int)(h.Number).Uint64() != 7 {
			t.Fatalf("number=%v", h.Number)
		}
		if uint64(h.Timestamp) != 99 {
			t.Fatalf("timestamp=%d", h.Timestamp)
		}
	case err := <-sub.Err():
		t.Fatalf("sub err: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for newHeads")
	}

	sub.Unsubscribe()
	deadline = time.Now().Add(2 * time.Second)
	for feed.SubscriberCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if feed.SubscriberCount() != 0 {
		t.Fatalf("leaked subscribers: %d", feed.SubscriberCount())
	}
}

func TestNewHeads_ConnectionCloseUnsubscribes(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()

	srv := rpc.NewServer()
	api := NewFilterAPI(feed, nil)
	defer api.Close()
	if err := srv.RegisterName("eth", api); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.WebsocketHandler([]string{"*"})}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := rpc.DialContext(ctx, "ws://"+ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	heads := make(chan json.RawMessage)
	sub, err := client.Subscribe(ctx, "eth", heads, "newHeads")
	if err != nil {
		t.Fatal(err)
	}
	_ = sub

	deadline := time.Now().Add(2 * time.Second)
	for feed.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if feed.SubscriberCount() != 1 {
		t.Fatalf("count=%d", feed.SubscriberCount())
	}

	client.Close()

	deadline = time.Now().Add(2 * time.Second)
	for feed.SubscriberCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if feed.SubscriberCount() != 0 {
		t.Fatalf("subscriber leaked after WS close: %d", feed.SubscriberCount())
	}
}

func TestNewHeads_SlowClientDoesNotStallHandle(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, nil)
	defer api.Close()

	srv := rpc.NewServer()
	if err := srv.RegisterName("eth", api); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.WebsocketHandler([]string{"*"})}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := rpc.DialContext(ctx, "ws://"+ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Never drain: buffer will fill and Notify/drop path must not block Handle.
	heads := make(chan *rpcHead)
	sub, err := client.Subscribe(ctx, "eth", heads, "newHeads")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()

	deadline := time.Now().Add(2 * time.Second)
	for feed.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	var max atomic.Int64
	for i := 0; i < 40; i++ {
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
		t.Fatalf("Handle max latency %v with stalled subscriber", time.Duration(max.Load()))
	}
}
