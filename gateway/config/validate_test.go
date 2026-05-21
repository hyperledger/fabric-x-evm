/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
	endorsercfg "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

func checkValidateErr(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr == "" {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantErr)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("error %q does not contain %q", err.Error(), wantErr)
	}
}

func newTmpFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.pem")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func validConfig(t *testing.T) config.Config {
	t.Helper()
	mspDir := t.TempDir()
	cert, key, ca := newTmpFile(t), newTmpFile(t), newTmpFile(t)
	tlsCfg := common.TLSConfig{CertPath: cert, KeyPath: key, CACertPaths: []string{ca}}
	ep := &common.Endpoint{Host: "127.0.0.1", Port: 4001}
	client := common.ClientConfig{Endpoint: ep, TLS: tlsCfg}
	identity := common.IdentityConfig{MspID: "Org1MSP", MSPDir: mspDir}
	return config.Config{
		Network: common.Network{Channel: "mychannel", Namespace: "basic"},
		Gateway: config.Gateway{
			Listen:    "0.0.0.0:8545",
			Identity:  identity,
			Database:  config.DB{ConnString: "file:gw.db"},
			Committer: client,
			Orderers:  []common.ClientConfig{client},
		},
		Endorsers: []endorsercfg.Endorser{
			{
				Name:      "org1",
				Identity:  identity,
				Committer: client,
				Database:  endorsercfg.DB{Database: "sqlite", ConnString: "file:e.db"},
			},
		},
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*config.Config)
		wantErr string
	}{
		{"valid", nil, ""},
		{"missing channel", func(c *config.Config) { c.Network.Channel = "" }, "network.channel"},
		{"missing namespace", func(c *config.Config) { c.Network.Namespace = "" }, "network.namespace"},
		{"bad protocol", func(c *config.Config) { c.Network.Protocol = "ethereum" }, "network.protocol"},
		{"missing listen", func(c *config.Config) { c.Gateway.Listen = "" }, "gateway.listen"},
		{"bad listen format", func(c *config.Config) { c.Gateway.Listen = "not-an-address" }, "invalid listen address"},
		{"bad listen port", func(c *config.Config) { c.Gateway.Listen = "0.0.0.0:99999" }, "port must be"},
		{"missing identity msp-id", func(c *config.Config) { c.Gateway.Identity.MspID = "" }, "msp-id"},
		{"missing identity msp-dir", func(c *config.Config) { c.Gateway.Identity.MSPDir = "" }, "msp-dir"},
		{"msp-dir not exist", func(c *config.Config) { c.Gateway.Identity.MSPDir = "/no/such/dir" }, "msp-dir"},
		{"missing db conn-string", func(c *config.Config) { c.Gateway.Database.ConnString = "" }, "gateway.database.connection-string"},
		{"nil committer endpoint", func(c *config.Config) { c.Gateway.Committer.Endpoint = nil }, "endpoint"},
		{"missing committer cert", func(c *config.Config) { c.Gateway.Committer.TLS.CertPath = "/no/cert" }, "tls.cert-path"},
		{"no orderers", func(c *config.Config) { c.Gateway.Orderers = nil }, "gateway.orderers"},
		{"orderer nil endpoint", func(c *config.Config) { c.Gateway.Orderers[0].Endpoint = nil }, "endpoint"},
		{"orderer missing ca cert", func(c *config.Config) { c.Gateway.Orderers[0].TLS.CACertPaths = []string{"/no/ca"} }, "tls.ca-cert-paths"},
		{"no endorsers", func(c *config.Config) { c.Endorsers = nil }, "endorsers"},
		{"endorser missing name", func(c *config.Config) { c.Endorsers[0].Name = "" }, "name"},
		{"endorser missing msp-dir", func(c *config.Config) { c.Endorsers[0].Identity.MSPDir = "" }, "msp-dir"},
		{"endorser missing db", func(c *config.Config) {
			c.Endorsers[0].Database.ConnString = ""
			c.Endorsers[0].Database.Database = ""
		}, "database"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			if tt.modify != nil {
				tt.modify(&cfg)
			}
			checkValidateErr(t, cfg.Validate(), tt.wantErr)
		})
	}
}
