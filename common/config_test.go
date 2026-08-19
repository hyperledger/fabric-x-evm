/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkErr(t *testing.T, err error, wantErr string) {
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

func TestValidateListenAddress(t *testing.T) {
	tests := []struct {
		addr    string
		wantErr string
	}{
		{"0.0.0.0:8545", ""},
		{":8545", ""},
		{"localhost:8080", ""},
		{"127.0.0.1:0", ""},
		{"", "invalid listen address"},
		{"no-port", "invalid listen address"},
		{"host:99999", "port must be"},
		{"host:abc", "port must be"},
		{"host:-1", "port must be"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			checkErr(t, ValidateListenAddress(tt.addr), tt.wantErr)
		})
	}
}

func TestIdentityConfigValidate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.pem")
	_ = os.WriteFile(file, nil, 0o644)

	tests := []struct {
		name    string
		cfg     IdentityConfig
		wantErr string
	}{
		{"valid", IdentityConfig{MspID: "Org1MSP", MSPDir: dir}, ""},
		{"no msp-id", IdentityConfig{MspID: "", MSPDir: dir}, "msp-id"},
		{"no msp-dir", IdentityConfig{MspID: "Org1MSP", MSPDir: ""}, "msp-dir"},
		{"msp-dir not exist", IdentityConfig{MspID: "Org1MSP", MSPDir: "/no/such/dir"}, "msp-dir"},
		{"msp-dir is file", IdentityConfig{MspID: "Org1MSP", MSPDir: file}, "not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErr(t, tt.cfg.Validate(), tt.wantErr)
		})
	}
}

func TestTLSConfigValidate(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "cert.pem")
	_ = os.WriteFile(existing, nil, 0o644)
	missing := filepath.Join(dir, "missing.pem")

	tests := []struct {
		name    string
		cfg     TLSConfig
		wantErr string
	}{
		{"empty", TLSConfig{}, ""},
		{"all paths exist", TLSConfig{CertPath: existing, KeyPath: existing, CACertPaths: []string{existing}}, ""},
		{"missing cert", TLSConfig{CertPath: missing}, "tls.cert-path"},
		{"missing key", TLSConfig{KeyPath: missing}, "tls.key-path"},
		{"missing ca cert", TLSConfig{CACertPaths: []string{missing}}, "tls.ca-cert-paths"},
		{"one ca missing among multiple", TLSConfig{CACertPaths: []string{existing, missing}}, "tls.ca-cert-paths"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErr(t, tt.cfg.Validate(), tt.wantErr)
		})
	}
}

func TestClientConfigValidate(t *testing.T) {
	ep := &Endpoint{Host: "127.0.0.1", Port: 4001}
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	_ = os.WriteFile(cert, nil, 0o644)
	missing := filepath.Join(dir, "missing.pem")

	tests := []struct {
		name    string
		cfg     ClientConfig
		wantErr string
	}{
		{"valid", ClientConfig{Endpoint: ep}, ""},
		{"valid with tls", ClientConfig{Endpoint: ep, TLS: TLSConfig{CertPath: cert}}, ""},
		{"nil endpoint", ClientConfig{}, "endpoint"},
		{"missing cert file", ClientConfig{Endpoint: ep, TLS: TLSConfig{CertPath: missing}}, "tls.cert-path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErr(t, tt.cfg.Validate(), tt.wantErr)
		})
	}
}

func TestNormalizeProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		want     string
		wantErr  string
	}{
		{"empty defaults to fabric-x", "", ProtocolFabricX, ""},
		{"fabric passes through", ProtocolFabric, ProtocolFabric, ""},
		{"fabric-x passes through", ProtocolFabricX, ProtocolFabricX, ""},
		{"bogus errors", "bogus", "", "network.protocol must be"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeProtocol(tt.protocol)
			checkErr(t, err, tt.wantErr)
			if tt.wantErr == "" && got != tt.want {
				t.Errorf("NormalizeProtocol(%q) = %q, want %q", tt.protocol, got, tt.want)
			}
		})
	}
}

// ---- Endpoint.Address ----

func TestEndpointAddress(t *testing.T) {
	tests := []struct {
		name string
		ep   *Endpoint
		want string
	}{
		// localhost hits the special-case avoiding net.JoinHostPort's
		// ipv6 default for the "localhost" hostname.
		{"localhost stays literal", &Endpoint{Host: "localhost", Port: 8080}, "localhost:8080"},
		{"ipv4 host", &Endpoint{Host: "127.0.0.1", Port: 4001}, "127.0.0.1:4001"},
		{"ipv6 host bracketed", &Endpoint{Host: "::1", Port: 4001}, "[::1]:4001"},
		{"hostname", &Endpoint{Host: "endorser.example.com", Port: 443}, "endorser.example.com:443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ep.Address(); got != tt.want {
				t.Errorf("Address() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---- ClientConfig.ToPeerConf / ToOrdererConf ----

func fullClientConfig() ClientConfig {
	return ClientConfig{
		Endpoint: &Endpoint{Host: "127.0.0.1", Port: 4001},
		TLS: TLSConfig{
			Mode:        "mutual",
			CertPath:    "/tls/cert.pem",
			KeyPath:     "/tls/key.pem",
			CACertPaths: []string{"/tls/ca1.pem", "/tls/ca2.pem"},
			ServerName:  "endorser.example.com",
		},
	}
}

func TestClientConfig_ToPeerConf(t *testing.T) {
	c := fullClientConfig()
	got := c.ToPeerConf()

	if got.Address != "127.0.0.1:4001" {
		t.Errorf("Address = %q", got.Address)
	}
	if got.TLS.Mode != "mutual" {
		t.Errorf("TLS.Mode = %q", got.TLS.Mode)
	}
	if got.TLS.CertPath != "/tls/cert.pem" {
		t.Errorf("TLS.CertPath = %q", got.TLS.CertPath)
	}
	if got.TLS.KeyPath != "/tls/key.pem" {
		t.Errorf("TLS.KeyPath = %q", got.TLS.KeyPath)
	}
	if len(got.TLS.CACertPaths) != 2 {
		t.Errorf("TLS.CACertPaths len = %d, want 2", len(got.TLS.CACertPaths))
	}
	if got.TLS.ServerName != "endorser.example.com" {
		t.Errorf("TLS.ServerName = %q", got.TLS.ServerName)
	}
}

func TestClientConfig_ToOrdererConf(t *testing.T) {
	c := fullClientConfig()
	got := c.ToOrdererConf()

	if got.Address != "127.0.0.1:4001" {
		t.Errorf("Address = %q", got.Address)
	}
	if got.TLS.Mode != "mutual" {
		t.Errorf("TLS.Mode = %q", got.TLS.Mode)
	}
	if got.TLS.CertPath != "/tls/cert.pem" {
		t.Errorf("TLS.CertPath = %q", got.TLS.CertPath)
	}
	if got.TLS.KeyPath != "/tls/key.pem" {
		t.Errorf("TLS.KeyPath = %q", got.TLS.KeyPath)
	}
	if len(got.TLS.CACertPaths) != 2 {
		t.Errorf("TLS.CACertPaths len = %d, want 2", len(got.TLS.CACertPaths))
	}
	if got.TLS.ServerName != "endorser.example.com" {
		t.Errorf("TLS.ServerName = %q", got.TLS.ServerName)
	}
}
