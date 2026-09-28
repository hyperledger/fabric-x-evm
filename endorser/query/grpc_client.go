/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package query

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/network"

	"github.com/hyperledger/fabric-x-evm/common"
)

// Connections is how many connections a GRPCClient opens. Concurrent reads on
// one HTTP/2 connection queue behind each other; a few connections spread them.
const Connections = 8

// readTimeout bounds a single call.
const readTimeout = 5 * time.Second

// GRPCClient reads from the query service, spreading reads over its connections.
type GRPCClient struct {
	peers   []*network.Peer
	clients []committerpb.QueryServiceClient
	next    atomic.Uint32
}

// NewGRPCClient opens conns connections to the query service at cfg.
func NewGRPCClient(cfg common.ClientConfig, conns int) (*GRPCClient, error) {
	c := &GRPCClient{}
	for range conns {
		p, err := network.NewPeer(cfg.ToPeerConf())
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		c.peers = append(c.peers, p)
		c.clients = append(c.clients, committerpb.NewQueryServiceClient(p.Connection()))
	}
	return c, nil
}

// BeginView opens a repeatable-read view: a consistent snapshot, taken at the
// view's first read. The query service may share that snapshot with views
// opened shortly before (view-aggregation-window), so keep the window short.
func (c *GRPCClient) BeginView(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	view, err := c.pick().BeginView(ctx, &committerpb.ViewParameters{IsoLevel: committerpb.IsoLevel_REPEATABLE_READ})
	if err != nil {
		return "", err
	}
	return view.GetId(), nil
}

// GetRows reads keys in the view, or the latest state if viewID is empty. A
// view lives on the server, so any connection can read it.
func (c *GRPCClient) GetRows(ctx context.Context, viewID, ns string, keys [][]byte) ([]Row, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	q := &committerpb.Query{Namespaces: []*committerpb.QueryNamespace{{NsId: ns, Keys: keys}}}
	if viewID != "" {
		q.View = &committerpb.View{Id: viewID}
	}
	res, err := c.pick().GetRows(ctx, q)
	if err != nil {
		return nil, err
	}

	var rows []Row
	for _, rns := range res.GetNamespaces() {
		if rns.GetNsId() != ns {
			continue
		}
		for _, r := range rns.GetRows() {
			rows = append(rows, Row{Key: r.GetKey(), Value: r.GetValue(), Version: r.GetVersion()})
		}
	}
	return rows, nil
}

// EndView closes a view.
func (c *GRPCClient) EndView(ctx context.Context, viewID string) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	_, err := c.pick().EndView(ctx, &committerpb.View{Id: viewID})
	return err
}

// pick returns the next connection's client, round-robin.
func (c *GRPCClient) pick() committerpb.QueryServiceClient {
	return c.clients[c.next.Add(1)%uint32(len(c.clients))]
}

// Close closes all connections.
func (c *GRPCClient) Close() error {
	var errs []error
	for _, p := range c.peers {
		errs = append(errs, p.Close())
	}
	return errors.Join(errs...)
}
