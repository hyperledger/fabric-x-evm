/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package query_test

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/query"
)

// fakeQS serves "k1" in namespace "evm" and records each call's view and
// client address.
type fakeQS struct {
	committerpb.UnimplementedQueryServiceServer

	mu      sync.Mutex
	views   []*committerpb.View
	callers map[string]int
}

func (f *fakeQS) GetRows(ctx context.Context, q *committerpb.Query) (*committerpb.Rows, error) {
	f.mu.Lock()
	f.views = append(f.views, q.GetView())
	if p, ok := peer.FromContext(ctx); ok {
		f.callers[p.Addr.String()]++
	}
	f.mu.Unlock()

	out := &committerpb.Rows{}
	for _, ns := range q.GetNamespaces() {
		rns := &committerpb.RowsNamespace{NsId: ns.GetNsId()}
		for _, k := range ns.GetKeys() {
			if ns.GetNsId() == "evm" && string(k) == "k1" {
				rns.Rows = append(rns.Rows, &committerpb.Row{Key: k, Value: []byte("v1"), Version: 5})
			}
		}
		out.Namespaces = append(out.Namespaces, rns)
	}
	return out, nil
}

func startFakeQS(t *testing.T) (*fakeQS, common.ClientConfig) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	qs := &fakeQS{callers: map[string]int{}}
	srv := grpc.NewServer()
	committerpb.RegisterQueryServiceServer(srv, qs)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	host, port, _ := net.SplitHostPort(lis.Addr().String())
	p, _ := strconv.Atoi(port)
	return qs, common.ClientConfig{Endpoint: &common.Endpoint{Host: host, Port: p}}
}

func TestGRPCClientGetRows(t *testing.T) {
	qs, cfg := startFakeQS(t)
	c, err := query.NewGRPCClient(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	rows, err := c.GetRows(context.Background(), "evm", [][]byte{[]byte("k1"), []byte("absent")})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].Value) != "v1" || rows[0].Version != 5 {
		t.Fatalf("rows = %+v, want one row v1 at version 5", rows)
	}
	if qs.views[0] != nil {
		t.Errorf("read used view %v, want no view", qs.views[0])
	}
}

func TestGRPCClientSpreadsReadsOverConnections(t *testing.T) {
	qs, cfg := startFakeQS(t)
	c, err := query.NewGRPCClient(cfg, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for range 4 {
		if _, err := c.GetRows(context.Background(), "evm", [][]byte{[]byte("k1")}); err != nil {
			t.Fatal(err)
		}
	}
	if len(qs.callers) != 2 {
		t.Fatalf("reads came from %d connections, want 2: %v", len(qs.callers), qs.callers)
	}
	for addr, n := range qs.callers {
		if n != 2 {
			t.Errorf("connection %s served %d reads, want 2", addr, n)
		}
	}
}
