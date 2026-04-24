/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/grpclog"

	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/integration"
)

func main() {
	// silence GRPC logging
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := &cobra.Command{
		Use:   "fxevm",
		Short: "fxevm - Fabric-X EVM gateway and endorser",
	}

	root.AddCommand(newStartCmd())

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newStartCmd returns the command to start a gateway with embedded
// endorsers in the same process. The --protocol flag will be removed
// once we load the configuration from yaml files.
func newStartCmd() *cobra.Command {
	var protocol string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the EVM gateway with embedded endorsers (single-process mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd.Context(), protocol)
		},
	}

	cmd.Flags().StringVar(&protocol, "protocol", "fabric-x", "Protocol to use: fabric-x or fabric")
	return cmd
}

func runStart(ctx context.Context, protocol string) error {
	var cfg config.Config
	switch protocol {
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

	return application.Run(ctx)
}
