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
	"github.com/hyperledger/fabric-x-evm/endorser/config"
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

func validEndorser(t *testing.T) config.Endorser {
	t.Helper()
	mspDir := t.TempDir()
	cert, err := os.CreateTemp(t.TempDir(), "*.pem")
	if err != nil {
		t.Fatal(err)
	}
	cert.Close()
	return config.Endorser{
		Name:     "org1",
		Identity: common.IdentityConfig{MspID: "Org1MSP", MSPDir: mspDir},
		Committer: common.ClientConfig{
			Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 4001},
		},
		Database: config.DB{Database: "sqlite", ConnString: "file:endorser.db"},
	}
}

func TestEndorserValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*config.Endorser)
		wantErr string
	}{
		{"valid", nil, ""},
		{"valid with database path", func(e *config.Endorser) {
			e.Database = config.DB{Database: "/some/path"}
		}, ""},
		{"missing name", func(e *config.Endorser) { e.Name = "" }, "name"},
		{"missing msp-id", func(e *config.Endorser) { e.Identity.MspID = "" }, "msp-id"},
		{"missing msp-dir", func(e *config.Endorser) { e.Identity.MSPDir = "" }, "msp-dir"},
		{"msp-dir not exist", func(e *config.Endorser) { e.Identity.MSPDir = "/no/such/dir" }, "msp-dir"},
		{"nil committer endpoint", func(e *config.Endorser) { e.Committer.Endpoint = nil }, "endpoint"},
		{"missing committer key", func(e *config.Endorser) { e.Committer.TLS.KeyPath = "/no/key" }, "tls.key-path"},
		{"no db at all", func(e *config.Endorser) {
			e.Database.Database = ""
			e.Database.ConnString = ""
		}, "database"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEndorser(t)
			if tt.modify != nil {
				tt.modify(&e)
			}
			checkErr(t, e.Validate(), tt.wantErr)
		})
	}
}
