/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"
)

func newHealthcheckCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Check whether the process is accepting connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthcheck(addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:8545", "Address to probe -- a gateway's HTTP listener, or (pass its own) a standalone endorser's gRPC listener")
	return cmd
}

// runHealthcheck is a bare TCP connect against addr (either gateway
// JSON API or the endorser GRPC endpoint). It doesn't test readiness.
func runHealthcheck(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("unreachable at %s: %w", addr, err)
	}
	return conn.Close()
}
