/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"testing"

	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

// applyConfigOverrides navigates dotted keys via reflection, and Config.Gateway
// is a pointer (nil for an endorser-only process) — these guard the two shapes
// that trips: a nested field through an already-set pointer, and through a nil
// one that needs allocating along the way.
func TestApplyConfigOverrides(t *testing.T) {
	t.Run("nested field through a non-nil pointer", func(t *testing.T) {
		cfg := &config.Config{Gateway: &config.Gateway{}}
		if err := applyConfigOverrides(cfg, map[string]any{"Gateway.WorkerCount": 7}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Gateway.WorkerCount != 7 {
			t.Errorf("Gateway.WorkerCount = %d, want 7", cfg.Gateway.WorkerCount)
		}
	})

	t.Run("nested field through a nil pointer allocates it", func(t *testing.T) {
		cfg := &config.Config{}
		if err := applyConfigOverrides(cfg, map[string]any{"Gateway.WorkerCount": 3}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Gateway == nil {
			t.Fatal("Gateway is still nil after override")
		}
		if cfg.Gateway.WorkerCount != 3 {
			t.Errorf("Gateway.WorkerCount = %d, want 3", cfg.Gateway.WorkerCount)
		}
	})

	t.Run("top-level field", func(t *testing.T) {
		cfg := &config.Config{}
		if err := applyConfigOverrides(cfg, map[string]any{"Network": cfg.Network}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown field errors", func(t *testing.T) {
		cfg := &config.Config{Gateway: &config.Gateway{}}
		err := applyConfigOverrides(cfg, map[string]any{"Gateway.NoSuchField": 1})
		if err == nil {
			t.Fatal("expected error for unknown field")
		}
	})
}
