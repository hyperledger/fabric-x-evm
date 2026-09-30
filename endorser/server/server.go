/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package server exposes the endorser over gRPC by adapting the EvmEndorsement
// service onto the in-process endorser through the api.Service seam.
package server

import (
	"context"
	"time"

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
	svc    api.Service
	ready  func() error
	health *health.Server
}

// New returns a Server backed by the given endorser service. ready backs the
// health service (typically the synchronizer's Ready); nil means always serving.
func New(svc api.Service, ready func() error) *Server {
	return &Server{svc: svc, ready: ready, health: health.NewServer()}
}

// RegisterService registers the endorsement and health services on the gRPC server.
func (s *Server) RegisterService(servers serve.Servers) {
	endorsementpb.RegisterEvmEndorsementServer(servers.GRPC, s)
	healthpb.RegisterHealthServer(servers.GRPC, s.health)
}

// Serve bootstraps the gRPC server (listen, mTLS, keep-alive, limits, health)
// from cfg and serves until ctx is done.
func (s *Server) Serve(ctx context.Context, cfg *Config) error {
	s.updateHealth()
	go s.trackHealth(ctx)
	return serve.Serve(ctx, s, withKeepalivePolicy(cfg))
}

// healthPoll is how often the health status is refreshed from ready.
const healthPoll = time.Second

// trackHealth keeps the health status in line with ready, and reports
// NOT_SERVING once ctx is done so clients drain before the server stops.
func (s *Server) trackHealth(ctx context.Context) {
	t := time.NewTicker(healthPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.health.Shutdown()
			return
		case <-t.C:
			s.updateHealth()
		}
	}
}

func (s *Server) updateHealth() {
	status := healthpb.HealthCheckResponse_SERVING
	if s.ready != nil && s.ready() != nil {
		status = healthpb.HealthCheckResponse_NOT_SERVING
	}
	s.health.SetServingStatus("", status)
}

// minPingInterval must stay at or below the endorser client's keepalive time.
const minPingInterval = 30 * time.Second

// withKeepalivePolicy returns cfg with a default enforcement policy that admits
// endorser client pings; gRPC's own default (5m, streams only) would reject them.
func withKeepalivePolicy(cfg *Config) *Config {
	ka := cfg.GRPC.KeepAlive
	if ka != nil && ka.EnforcementPolicy != nil {
		return cfg
	}
	out := *cfg
	out.GRPC.KeepAlive = &serve.ServerKeepAliveConfig{
		EnforcementPolicy: &serve.ServerKeepAliveEnforcementPolicyConfig{MinTime: minPingInterval, PermitWithoutStream: true},
	}
	if ka != nil {
		out.GRPC.KeepAlive.Params = ka.Params
	}
	return &out
}

// ServeEndorser serves svc over gRPC per cfg until ctx is done; ready backs the health service.
func ServeEndorser(ctx context.Context, svc api.Service, ready func() error, cfg *serve.ServerConfig) error {
	return New(svc, ready).Serve(ctx, &Config{GRPC: *cfg})
}
