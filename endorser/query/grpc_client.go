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

// readTimeout bounds a single read.
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

// GetRows reads without a view. A view is a snapshot that can be up to the
// query service's aggregation window old, so a key committed just before would
// read back at its old version and the transaction would fail MVCC.
func (c *GRPCClient) GetRows(ctx context.Context, ns string, keys [][]byte) ([]Row, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	client := c.clients[c.next.Add(1)%uint32(len(c.clients))]
	res, err := client.GetRows(ctx, &committerpb.Query{
		Namespaces: []*committerpb.QueryNamespace{{NsId: ns, Keys: keys}},
	})
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

// Close closes all connections.
func (c *GRPCClient) Close() error {
	var errs []error
	for _, p := range c.peers {
		errs = append(errs, p.Close())
	}
	return errors.Join(errs...)
}
