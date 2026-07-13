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
	"os/signal"
	"syscall"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/grpclog"
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
	root.AddCommand(newTestNodeCmd())
	root.AddCommand(newHealthcheckCmd())

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newStartCmd starts the gateway and endorsers in a single process (combined mode).
func newStartCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the EVM gateway with embedded endorsers (single-process mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd.Context(), configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to the YAML configuration file")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func runStart(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config %q: %w", configPath, err)
	}
	flogging.Init(flogging.Config{
		Format:  cfg.Logging.Format,
		LogSpec: cfg.Logging.Spec,
	})

	application, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}

	return application.Run(ctx)
}

// newTestNodeCmd returns the command to start a test node with test RPC enabled.
// This is a test-only mode that should NEVER be used in production.
//
// With no --config, it's fully self-contained: an in-process fabrictest network,
// ephemeral identities, embedded test accounts — no file, no docker/Fablo backend.
// With --config, it behaves as before: test RPC bolted onto a real ordering backend.
func newTestNodeCmd() *cobra.Command {
	var configPath string
	var testAccountsPath string
	var listen string
	var chainID int64
	var protocol string

	cmd := &cobra.Command{
		Use:   "testnode",
		Short: "Start a test node with test RPC enabled (UNSAFE - for testing only)",
		Long: `Start a test node with test RPC methods enabled.

		WARNING: This mode enables server-side transaction signing and other
		test-only features that are UNSAFE for production use. Only use this
		for development and testing with Hardhat, OpenZeppelin tests, etc.

		This mode automatically:
		- Enables test RPC methods (eth_accounts, eth_sendTransaction)
		- Returns test-friendly gas estimates

		NEVER use this in production environments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return runSelfContainedTestNode(cmd.Context(), listen, chainID, protocol, testAccountsPath)
			}
			return runTestNodeWithConfig(cmd.Context(), configPath, testAccountsPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to a YAML config for a real ordering backend (omit for a self-contained in-process node)")
	cmd.Flags().StringVar(&testAccountsPath, "test-accounts-path", "", "Path to JSON file containing test accounts with private keys (default: embedded Hardhat accounts)")
	cmd.Flags().StringVar(&listen, "listen", ":8545", "HTTP listen address (self-contained mode only)")
	cmd.Flags().Int64Var(&chainID, "chain-id", 31337, "Ethereum chain ID (self-contained mode only)")
	cmd.Flags().StringVar(&protocol, "protocol", "fabric-x", "Network protocol: fabric or fabric-x (self-contained mode only)")

	// --listen/--chain-id/--protocol only apply to the self-contained path; reject them
	// alongside --config instead of silently ignoring them.
	cmd.MarkFlagsMutuallyExclusive("config", "listen")
	cmd.MarkFlagsMutuallyExclusive("config", "chain-id")
	cmd.MarkFlagsMutuallyExclusive("config", "protocol")

	return cmd
}

// runSelfContainedTestNode starts a testnode with no external dependencies: an
// in-process fabrictest network, ephemeral local identities, in-memory storage.
func runSelfContainedTestNode(ctx context.Context, listen string, chainID int64, protocol, testAccountsPath string) error {
	application, err := app.NewTestNode(ctx, app.TestNodeConfig{
		Listen:           listen,
		ChainID:          chainID,
		Protocol:         protocol,
		TestAccountsPath: testAccountsPath,
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

// runTestNodeWithConfig starts a testnode against a real ordering backend
// (Fablo/fabric-x) described by a config file, with test RPC forced on.
func runTestNodeWithConfig(ctx context.Context, configPath, testAccountsPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config %q: %w", configPath, err)
	}

	application, err := app.NewTestNodeWithConfig(ctx, cfg, testAccountsPath)
	if err != nil {
		return err
	}
	return application.Run(ctx)
}
