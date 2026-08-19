/*
Copyright IBM Corp. All Right Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"fmt"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-evm/common"
)

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
	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	// Verify the endorser's certificate against its configured name rather than
	// the dial address. Endorser certs are normally issued for a hostname, so
	// without this any deployment reached by IP fails the handshake.
	if cfg.TLS.ServerName != "" {
		opts = append(opts, grpc.WithAuthority(cfg.TLS.ServerName))
	}

	conn, err := grpc.NewClient(cfg.Endpoint.Address(), opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.Endpoint.Address(), err)
	}
	return New(conn), nil
}
