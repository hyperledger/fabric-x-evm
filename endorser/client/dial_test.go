/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"strings"
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
)

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
