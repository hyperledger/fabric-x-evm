/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyperledger/fabric-x-evm/mockfabricx"
	"google.golang.org/grpc/grpclog"
)

func main() {
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := mockfabricx.DefaultConfig()
	cfg.BindFlags(flag.CommandLine)
	flag.Parse()

	server, err := mockfabricx.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mock-fabric-x listening orderer=%s committer=%s tls=%s\n", cfg.OrdererListen, cfg.CommitterListen, cfg.TLSMode)
	if err := server.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
