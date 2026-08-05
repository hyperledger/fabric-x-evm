/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
)

// writeGarbage writes non-PEM content to a temp file and returns its path -
// the file exists and reads fine, but isn't valid certificate data.
func writeGarbage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "garbage.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDial_NoTLS_Succeeds(t *testing.T) {
	cfg := common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0}}

	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c == nil {
		t.Fatal("Client = nil, want non-nil")
	}
}

func TestDial_MissingCertFile_ReturnsLoadError(t *testing.T) {
	cfg := common.ClientConfig{
		Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0},
		TLS: common.TLSConfig{
			Mode:        "mtls",
			CertPath:    "/no/such/cert.pem",
			KeyPath:     "/no/such/key.pem",
			CACertPaths: []string{"/no/such/ca.pem"},
		},
	}

	_, err := Dial(cfg)
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
	if !strings.Contains(err.Error(), "load TLS credentials") {
		t.Errorf("error = %q, want it to mention loading TLS credentials", err.Error())
	}
}

// The cert files exist and are readable (so NewClientTLSCredentials succeeds),
// but their content isn't valid certificate data, so building the transport
// credentials from the loaded bytes fails.
func TestDial_InvalidCertContent_ReturnsBuildError(t *testing.T) {
	garbage := writeGarbage(t)
	cfg := common.ClientConfig{
		Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0},
		TLS: common.TLSConfig{
			Mode:        "mtls",
			CertPath:    garbage,
			KeyPath:     garbage,
			CACertPaths: []string{garbage},
		},
	}

	_, err := Dial(cfg)
	if err == nil {
		t.Fatal("expected error for invalid certificate content")
	}
	if !strings.Contains(err.Error(), "build transport credentials") {
		t.Errorf("error = %q, want it to mention building transport credentials", err.Error())
	}
}
