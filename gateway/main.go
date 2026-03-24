/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/integration"
	"google.golang.org/grpc/grpclog"
)

func main() {
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	if err := start(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func start() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg := integration.FabricSamplesConfig(path.Join(cwd, "..", "testdata"))

	application, err := app.New(cfg)
	if err != nil {
		return err
	}

	return application.Run(context.Background())
}
