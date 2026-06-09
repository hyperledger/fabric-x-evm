/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Server runs mock orderer and committer gRPC services against one ledger.
type Server struct {
	cfg     Config
	ledger  *Ledger
	orderer *ordererService
}

// NewServer validates cfg and constructs a mock Fabric-X server.
func NewServer(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ledger := NewLedger()
	return &Server{
		cfg:    cfg,
		ledger: ledger,
		orderer: newOrdererService(ledger, ordererServiceConfig{
			maxTxPerBlock: cfg.MaxTxPerBlock,
			blockTimeout:  cfg.BlockTimeout,
			queueSize:     cfg.QueueSize,
		}),
	}, nil
}

// Run serves orderer and committer endpoints until ctx is canceled or serving fails.
func (s *Server) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.orderer.run(ctx) })
	g.Go(func() error { return s.serveOrderer(ctx) })
	g.Go(func() error { return s.serveCommitter(ctx) })
	err := g.Wait()
	if errors.Is(err, grpc.ErrServerStopped) || (err == nil && ctx.Err() != nil) {
		return nil
	}
	return err
}

func (s *Server) serveOrderer(ctx context.Context) error {
	opts, err := serverOptions(s.cfg.TLSMode, s.cfg.OrdererCert, s.cfg.OrdererKey, s.cfg.OrdererClientCA)
	if err != nil {
		return err
	}
	return s.serve(ctx, s.cfg.OrdererListen, opts, func(grpcServer *grpc.Server) {
		RegisterOrdererServices(grpcServer, s.orderer)
	})
}

func (s *Server) serveCommitter(ctx context.Context) error {
	opts, err := serverOptions(s.cfg.TLSMode, s.cfg.CommitterCert, s.cfg.CommitterKey, s.cfg.CommitterClientCA)
	if err != nil {
		return err
	}
	return s.serve(ctx, s.cfg.CommitterListen, opts, func(grpcServer *grpc.Server) {
		RegisterCommitterServices(grpcServer, s.ledger)
	})
}

func (s *Server) serve(ctx context.Context, listen string, opts []grpc.ServerOption, register func(*grpc.Server)) error {
	lis, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(opts...)
	register(grpcServer)
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()
	return grpcServer.Serve(lis)
}

func serverOptions(mode, certPath, keyPath, clientCAPath string) ([]grpc.ServerOption, error) {
	if mode == TLSModeNone {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}
	caPEM, err := os.ReadFile(clientCAPath)
	if err != nil {
		return nil, fmt.Errorf("read client CA %s: %w", clientCAPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse client CA %s", clientCAPath)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(cfg))}, nil
}
