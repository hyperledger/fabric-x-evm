/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package query_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"

	"github.com/hyperledger/fabric-x-evm/endorser/query"
)

// fakeClient serves rows from a map and records reads and views.
type fakeClient struct {
	rows      map[string]query.Row // key -> row, for namespace "evm"
	reads     int
	readViews []string
	begun     int
	ended     []string
	closed    bool
}

func (c *fakeClient) BeginView(context.Context) (string, error) {
	c.begun++
	return fmt.Sprintf("view-%d", c.begun), nil
}

func (c *fakeClient) EndView(_ context.Context, viewID string) error {
	c.ended = append(c.ended, viewID)
	return nil
}

func (c *fakeClient) GetRows(_ context.Context, viewID, ns string, keys [][]byte) ([]query.Row, error) {
	c.reads++
	c.readViews = append(c.readViews, viewID)
	var out []query.Row
	for _, k := range keys {
		if r, ok := c.rows[string(k)]; ok && ns == "evm" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (c *fakeClient) Close() error { c.closed = true; return nil }

func TestKVSGet(t *testing.T) {
	client := &fakeClient{rows: map[string]query.Row{"k1": {Key: []byte("k1"), Value: []byte("v1"), Version: 3}}}
	kvs := query.NewKVS(client, "evm")

	rec, err := kvs.Get("evm", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || string(rec.Value) != "v1" || rec.Version != 3 {
		t.Fatalf("Get k1 = %+v, want v1 at version 3", rec)
	}

	rec, err = kvs.Get("evm", "absent")
	if err != nil || rec != nil {
		t.Fatalf("Get absent = %+v, %v; want nil, nil", rec, err)
	}
	if client.begun != 0 || client.readViews[0] != "" {
		t.Errorf("Get used a view (begun %d, read view %q); a single read needs none", client.begun, client.readViews[0])
	}
}

func TestKVSSnapshotReadsThroughOneView(t *testing.T) {
	client := &fakeClient{rows: map[string]query.Row{"k1": {Key: []byte("k1"), Value: []byte("v1")}}}
	kvs := query.NewKVS(client, "evm")

	snap, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"k1", "k2"} {
		if _, err := snap.Get("evm", key); err != nil {
			t.Fatal(err)
		}
	}
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}

	if client.begun != 1 {
		t.Fatalf("views begun = %d, want 1", client.begun)
	}
	for i, v := range client.readViews {
		if v != "view-1" {
			t.Errorf("read %d used view %q, want view-1", i, v)
		}
	}
	if len(client.ended) != 1 || client.ended[0] != "view-1" {
		t.Errorf("views ended = %v, want [view-1]", client.ended)
	}
}

func TestKVSSnapshotRejectsSpecificBlock(t *testing.T) {
	client := &fakeClient{}
	n := uint64(3)
	if _, err := query.NewKVS(client, "evm").NewSnapshot(&n); err == nil {
		t.Fatal("NewSnapshot at block 3 succeeded; want an error")
	}
	if client.begun != 0 {
		t.Errorf("a view was opened for a rejected snapshot")
	}
}

func TestKVSSnapshotCachesReads(t *testing.T) {
	client := &fakeClient{rows: map[string]query.Row{"k1": {Key: []byte("k1"), Value: []byte("v1")}}}
	kvs := query.NewKVS(client, "evm")

	snap, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := snap.Get("evm", "k1"); err != nil {
			t.Fatal(err)
		}
		if _, err := snap.Get("evm", "absent"); err != nil {
			t.Fatal(err)
		}
	}
	if client.reads != 2 {
		t.Errorf("client reads = %d, want 2 (one per key)", client.reads)
	}
}

func TestKVSBlockNumber(t *testing.T) {
	kvs := query.NewKVS(&fakeClient{}, "evm")
	ctx := context.Background()

	kvs.SetBlockNumber(5)
	for _, tc := range []struct{ deliver, want uint64 }{
		{5, 5},
		{3, 5}, // an older block must not move the height back
		{7, 7},
	} {
		if err := kvs.Handle(ctx, blocks.Block{Number: tc.deliver}); err != nil {
			t.Fatal(err)
		}
		if got, _ := kvs.BlockNumber(ctx); got != tc.want {
			t.Errorf("after block %d: BlockNumber = %d, want %d", tc.deliver, got, tc.want)
		}
	}
}

func TestKVSCloseClosesClient(t *testing.T) {
	client := &fakeClient{}
	if err := query.NewKVS(client, "evm").Close(); err != nil {
		t.Fatal(err)
	}
	if !client.closed {
		t.Error("client not closed")
	}
}
