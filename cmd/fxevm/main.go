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
	root.AddCommand(newTestNodeCmd())

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
		// Pass relative path - FabricSamplesConfig will resolve it from project root
		cfg = integration.FabricSamplesConfig("testdata")
	default:
		return errors.New("start with --protocol fabric-x or --protocol fabric")
	}

	application, err := app.New(cfg)
	if err != nil {
		return err
	}

	return application.Run(ctx)
}

// newTestNodeCmd returns the command to start a test node with test RPC enabled.
// This is a test-only mode that should NEVER be used in production.
func newTestNodeCmd() *cobra.Command {
	var protocol string
	var testAccountsPath string

	cmd := &cobra.Command{
		Use:   "testnode",
		Short: "Start a test node with test RPC enabled (UNSAFE - for testing only)",
		Long: `Start a test node with test RPC methods enabled.

WARNING: This mode enables server-side transaction signing and other
test-only features that are UNSAFE for production use. Only use this
for development and testing with Hardhat, OpenZeppelin tests, etc.

This mode automatically:
- Enables test RPC methods (eth_accounts, eth_sendTransaction)
- Uses in-memory trie DB to avoid stale state between runs
- Returns test-friendly gas estimates

NEVER use this in production environments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTestNode(cmd.Context(), protocol, testAccountsPath)
		},
	}

	cmd.Flags().StringVar(&protocol, "protocol", "fabric-x", "Protocol to use: fabric-x or fabric")
	cmd.Flags().StringVar(&testAccountsPath, "test-accounts-path", "testdata/test_accounts.json",
		"Path to JSON file containing test accounts with private keys")

	return cmd
}

func runTestNode(ctx context.Context, protocol string, testAccountsPath string) error {
	// Use test-specific configuration with in-memory trie DB
	cfg := integration.TestNodeConfig(protocol, "testdata")
	if cfg.Network.Protocol == "" {
		return fmt.Errorf("testnode with --protocol fabric-x or --protocol fabric")
	}

	// Override test accounts path if specified
	if testAccountsPath != "" {
		cfg.Gateway.TestAccountsPath = testAccountsPath
	}

	fmt.Println("========================================")
	fmt.Println("WARNING: Test node mode enabled")
	fmt.Println("WARNING: Test RPC methods enabled")
	fmt.Println("WARNING: Server-side signing is UNSAFE")
	fmt.Println("WARNING: Using in-memory trie DB")
	fmt.Println("WARNING: NEVER use in production")
	fmt.Println("========================================")

	application, err := app.New(cfg)
	if err != nil {
		return err
	}

	return application.Run(ctx)
}
