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
	conn, err := grpc.NewClient(cfg.Endpoint.Address(), grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.Endpoint.Address(), err)
	}
	return New(conn), nil
}
