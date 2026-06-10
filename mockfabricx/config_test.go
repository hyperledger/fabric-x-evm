/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfigMatchesFabXPortsAndBatchDefaults(t *testing.T) {
	cfg := DefaultConfig()

	require.Equal(t, "127.0.0.1:7050", cfg.OrdererListen)
	require.Equal(t, "127.0.0.1:4001", cfg.CommitterListen)
	require.Equal(t, TLSModeMTLS, cfg.TLSMode)
	require.Equal(t, 1, cfg.MaxTxPerBlock)
	require.Equal(t, time.Duration(0), cfg.BlockTimeout)
	require.Equal(t, 65536, cfg.QueueSize)
	require.Equal(t, 5000, cfg.RetainedBlocks)
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTxPerBlock = 0
	require.ErrorContains(t, cfg.Validate(), "max-tx-per-block")

	cfg = DefaultConfig()
	cfg.TLSMode = "bogus"
	require.ErrorContains(t, cfg.Validate(), "tls-mode")

	cfg = DefaultConfig()
	cfg.RetainedBlocks = -1
	require.ErrorContains(t, cfg.Validate(), "retained-blocks")
}
