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
		Short: "Check whether the gateway is accepting connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthcheck(addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:8545", "Address to probe")
	return cmd
}

func runHealthcheck(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("gateway unreachable at %s: %w", addr, err)
	}
	return conn.Close()
}
