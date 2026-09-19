/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

func TestNewHeads_WSRoundTripMatchesGetBlock(t *testing.T) {
	stored := &domain.Block{
		BlockNumber: 7,
		BlockHash:   bytes32(0x77),
		ParentHash:  bytes32(0x66),
		StateRoot:   bytes32(0x55),
		Timestamp:   99,
	}
	backend := &stubBackend{
		chainID:       big.NewInt(4011),
		blockNum:      7,
		blockByNumber: map[uint64]*domain.Block{7: stored},
	}
	filterAPI := filters.NewFilterAPI(backend)
	t.Cleanup(filterAPI.Close)

	srv, err := NewServer(backend, filterAPI)
	if err != nil {
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

	heads := make(chan *RPCBlock, 4)
	sub, err := client.Subscribe(ctx, "eth", heads, "newHeads")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	deadline := time.Now().Add(2 * time.Second)
	for filterAPI.HeadSubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := filterAPI.Handle(context.Background(), blocks.Block{
		Number: 7, Hash: bytes32(0x77), ParentHash: bytes32(0x66), Timestamp: 99,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case h := <-heads:
		want := RPCBlockFromDomain(stored)
		if h.Hash != want.Hash || h.StateRoot != want.StateRoot || h.Number != want.Number {
			t.Fatalf("newHeads=%+v want=%+v", h, want)
		}
		if uint64(h.Timestamp) != 99 {
			t.Fatalf("timestamp=%d", h.Timestamp)
		}
	case err := <-sub.Err():
		t.Fatalf("sub err: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for newHeads")
	}
}

func bytes32(b byte) []byte {
	h := make([]byte, 32)
	h[31] = b
	return h
}

func TestRPCBlockFromDomain_MatchesGetBlockShape(t *testing.T) {
	b := &domain.Block{
		BlockNumber: 3,
		BlockHash:   bytes32(0xab),
		ParentHash:  bytes32(0xcd),
		StateRoot:   bytes32(0xef),
		Timestamp:   42,
	}
	got := RPCBlockFromDomain(b)
	if got.Hash != common.BytesToHash(bytes32(0xab)) {
		t.Fatalf("hash=%s", got.Hash)
	}
	if got.StateRoot != common.BytesToHash(bytes32(0xef)) {
		t.Fatalf("stateRoot=%s", got.StateRoot)
	}
	if got.Number != hexutil.Uint64(3) {
		t.Fatalf("number=%d", got.Number)
	}
}
