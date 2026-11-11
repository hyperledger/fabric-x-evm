/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/state"
	"github.com/hyperledger/fabric-x-sdk/state/queryservice"
)

// fakeQueryClient serves one row and counts views.
type fakeQueryClient struct {
	begun, ended int
}

func (c *fakeQueryClient) BeginView(context.Context) (string, error) {
	c.begun++
	return "view", nil
}

func (c *fakeQueryClient) EndView(context.Context, string) error {
	c.ended++
	return nil
}

func (c *fakeQueryClient) GetRows(_ context.Context, _, _ string, keys [][]byte) ([]queryservice.Row, error) {
	return []queryservice.Row{{Key: keys[0], Value: []byte("v"), Version: 3}}, nil
}

func (c *fakeQueryClient) Close() error { return nil }

func TestQueryServiceKVSNewSnapshot(t *testing.T) {
	client := &fakeQueryClient{}
	kvs := NewQueryServiceKVS(client)

	snap, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := snap.Get("ns", "k")
	if err != nil || rec == nil || rec.Version != 3 {
		t.Fatalf("Get = %+v, %v; want version 3", rec, err)
	}
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if client.begun != 1 || client.ended != 1 {
		t.Fatalf("views begun/ended = %d/%d, want 1/1", client.begun, client.ended)
	}

	h := uint64(5)
	if _, err := kvs.NewSnapshot(&h); !errors.Is(err, state.ErrHistoryUnavailable) {
		t.Fatalf("NewSnapshot(&5) error = %v, want ErrHistoryUnavailable", err)
	}
	if client.begun != 1 {
		t.Fatalf("a historical snapshot opened a view")
	}
}
