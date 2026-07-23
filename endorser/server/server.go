/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package server exposes the endorser over gRPC by adapting the EvmEndorsement
// service onto the in-process endorser through the api.Service seam.
package server

import (
	"context"

	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
)

// Config is the gRPC server configuration (endpoint, mTLS, keep-alive,
// max-concurrent-streams, rate-limit).
//
// TODO: switch to fabric-x-common's serve once it is available there.
type Config = serve.Config

// Server adapts the EvmEndorsement gRPC service onto an api.Service.
type Server struct {
	endorsementpb.UnimplementedEvmEndorsementServer
	svc api.Service
}

// New returns a Server backed by the given endorser service.
func New(svc api.Service) *Server {
	return &Server{svc: svc}
}

// RegisterService registers the endorsement and health services on the gRPC server.
func (s *Server) RegisterService(servers serve.Servers) {
	endorsementpb.RegisterEvmEndorsementServer(servers.GRPC, s)
	healthpb.RegisterHealthServer(servers.GRPC, health.NewServer())
}

// Serve bootstraps the gRPC server (listen, mTLS, keep-alive, limits, health)
// from cfg and serves until ctx is done.
func (s *Server) Serve(ctx context.Context, cfg *Config) error {
	return serve.Serve(ctx, s, cfg)
}
