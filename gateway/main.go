/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
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
	protocol := flag.String("protocol", "fabric-x", "Protocol to use: fabric-x or fabric")
	flag.Parse()

	var cfg config.Config
	switch *protocol {
	case "fabric-x", "":
		cfg = integration.XTestCommitterConfig()
	case "fabric":
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg = integration.FabricSamplesConfig(path.Join(cwd, "..", "testdata"))
	default:
		return errors.New("start with --protocol fabric-x or --protocol fabric")
	}

	application, err := app.New(cfg)
	if err != nil {
		return err
	}

	return application.Run(context.Background())
}
