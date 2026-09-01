/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

func TestRPC_NewBlockFilterRoundTrip(t *testing.T) {
	feed := filters.NewBlockFeed()
	defer feed.Close()

	// Register FilterAPI the same way NewServer does: merge under "eth".
	srv := rpc.NewServer()
	apiInst := filters.NewFilterAPI(feed, nil)
	defer apiInst.Close()
	if err := srv.RegisterName("eth", apiInst); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(srv)

	var id string
	if err := client.Call(&id, "eth_newBlockFilter"); err != nil {
		t.Fatalf("eth_newBlockFilter: %v", err)
	}
	if id == "" {
		t.Fatal("empty filter id")
	}

	h := make([]byte, 32)
	h[31] = 0xab
	_ = feed.Handle(context.Background(), blocks.Block{Number: 3, Hash: h})

	deadline := time.Now().Add(2 * time.Second)
	var changes []common.Hash
	for time.Now().Before(deadline) {
		if err := client.Call(&changes, "eth_getFilterChanges", id); err != nil {
			t.Fatal(err)
		}
		if len(changes) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(changes) != 1 || changes[0][31] != 0xab {
		t.Fatalf("changes = %#v", changes)
	}

	var ok bool
	if err := client.Call(&ok, "eth_uninstallFilter", id); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("uninstall returned false")
	}
}
