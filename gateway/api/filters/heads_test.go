/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

type stubBlocks struct {
	stubLogs
	block *domain.Block
}

func (s *stubBlocks) GetBlockByNumber(context.Context, uint64, bool) (*domain.Block, error) {
	return s.block, s.err
}

func TestSubscribeHeads_ReceiveAndUnsubscribe(t *testing.T) {
	stored := &domain.Block{
		BlockNumber: 9,
		BlockHash:   bytes32(0x99),
		ParentHash:  bytes32(0x88),
		StateRoot:   bytes32(0x77),
		Timestamp:   123,
	}
	api := NewFilterAPI(&stubBlocks{stubLogs: stubLogs{head: 9}, block: stored})
	t.Cleanup(api.Close)

	sub := api.SubscribeHeads(4)
	if api.HeadSubscriberCount() != 1 {
		t.Fatalf("count=%d", api.HeadSubscriberCount())
	}

	if err := api.Handle(context.Background(), blocks.Block{
		Number: 9, Hash: bytes32(0x99), ParentHash: bytes32(0x88), Timestamp: 123,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case b := <-sub.Chan():
		if b.BlockNumber != 9 || b.StateRoot[31] != 0x77 {
			t.Fatalf("got %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	sub.Unsubscribe()
	sub.Unsubscribe()
	if api.HeadSubscriberCount() != 0 {
		t.Fatalf("count=%d", api.HeadSubscriberCount())
	}
}

func TestSubscribeHeads_AfterClose(t *testing.T) {
	api := NewFilterAPI(nil)
	api.Close()

	sub := api.SubscribeHeads(2)
	_, ok := <-sub.Chan()
	if ok {
		t.Fatal("channel should be closed")
	}
}

func TestSubscribeHeads_Backpressure(t *testing.T) {
	api := NewFilterAPI(&stubBlocks{stubLogs: stubLogs{head: 1}, block: &domain.Block{BlockNumber: 1, BlockHash: bytes32(1)}})
	t.Cleanup(api.Close)

	_ = api.SubscribeHeads(1)
	_ = api.Handle(context.Background(), testBlock(1, 1))

	for i := 0; i < 32; i++ {
		start := time.Now()
		if err := api.Handle(context.Background(), testBlock(uint64(i+2), byte(i))); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 50*time.Millisecond {
			t.Fatal("Handle blocked on slow subscriber")
		}
	}
}
