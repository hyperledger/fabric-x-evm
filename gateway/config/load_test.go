/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config_test

import (
	"testing"

	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

func TestLoadFabXSampleConfig(t *testing.T) {
	cfg, err := config.Load("../../integration/fabx.yaml")
	if err != nil {
		t.Fatalf("Load fabx.yaml: %v", err)
	}
	if cfg.Network.Protocol != "fabric-x" {
		t.Errorf("expected protocol fabric-x, got %q", cfg.Network.Protocol)
	}
	if cfg.Network.ChainID != 4011 {
		t.Errorf("expected chain-id 4011, got %d", cfg.Network.ChainID)
	}
	if len(cfg.Endorsers) == 0 {
		t.Error("expected at least one endorser")
	}
	if cfg.Gateway.Listen == "" {
		t.Error("expected gateway.listen to be set")
	}
}

func TestLoadFabricSamplesSampleConfig(t *testing.T) {
	cfg, err := config.Load("../../integration/fablo.yaml")
	if err != nil {
		t.Fatalf("Load fabric-samples.yaml: %v", err)
	}
	if cfg.Network.Protocol != "fabric" {
		t.Errorf("expected protocol fabric, got %q", cfg.Network.Protocol)
	}
	if len(cfg.Endorsers) != 2 {
		t.Errorf("expected 2 endorsers, got %d", len(cfg.Endorsers))
	}
}
