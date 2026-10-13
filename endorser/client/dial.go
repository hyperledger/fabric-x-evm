/*
Copyright IBM Corp. All Right Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/keepalive"

	"github.com/hyperledger/fabric-x-evm/common"
)

// callTimeout bounds an endorser call whose context carries no deadline.
const callTimeout = 10 * time.Second

// reconnectMaxDelay caps the backoff between connection attempts.
const reconnectMaxDelay = 3 * time.Second

// keepaliveTime is the client ping interval; endorser servers must permit it.
const keepaliveTime = 60 * time.Second

// Dial opens a gRPC connection to cfg's endpoint using cfg's TLS settings and
// returns a Client backed by it. The caller owns the returned Client and must close it.
func Dial(cfg common.ClientConfig) (*Client, error) {
	tlsCreds, err := connection.NewClientTLSCredentials(connection.TLSConfig{
		Mode:        cfg.TLS.Mode,
		CertPath:    cfg.TLS.CertPath,
		KeyPath:     cfg.TLS.KeyPath,
		CACertPaths: cfg.TLS.CACertPaths,
	})
	if err != nil {
		return nil, fmt.Errorf("load TLS credentials: %w", err)
	}
	creds, err := connection.NewClientGRPCTransportCredentials(tlsCreds)
	if err != nil {
		return nil, fmt.Errorf("build transport credentials: %w", err)
	}
	bo := backoff.DefaultConfig
	bo.MaxDelay = reconnectMaxDelay
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		// Queue calls across an endorser restart instead of failing fast.
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithChainUnaryInterceptor(defaultDeadline),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: bo, MinConnectTimeout: 5 * time.Second}),
		// Ping idle connections too: unary calls are short, so a dead peer would otherwise go unnoticed.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: keepaliveTime, Timeout: 20 * time.Second, PermitWithoutStream: true}),
	}
	// Verify the endorser's certificate against its configured name rather than
	// the dial address. Endorser certs are normally issued for a hostname, so
	// without this any deployment reached by IP fails the handshake.
	if cfg.TLS.ServerName != "" {
		opts = append(opts, grpc.WithAuthority(cfg.TLS.ServerName))
	}

	// passthrough re-resolves per connect; the dns resolver backs off up to 2m after a failed lookup.
	conn, err := grpc.NewClient("passthrough:///"+cfg.Endpoint.Address(), opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.Endpoint.Address(), err)
	}
	return New(conn), nil
}

// defaultDeadline applies callTimeout so WaitForReady cannot block forever.
func defaultDeadline(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}
