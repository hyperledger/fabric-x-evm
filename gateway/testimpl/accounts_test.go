/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// The accounts JSON is embedded, so the default set needs no file on disk.
func TestDefaultTestAccounts_AreEmbedded(t *testing.T) {
	mgr, err := DefaultTestAccounts()
	if err != nil {
		t.Fatalf("DefaultTestAccounts: %v", err)
	}
	if len(mgr.Addresses) == 0 {
		t.Fatal("no accounts")
	}
	if len(mgr.PrivateKeys) != len(mgr.Addresses) {
		t.Fatalf("got %d keys for %d addresses", len(mgr.PrivateKeys), len(mgr.Addresses))
	}
	// Every address must be the one its own key derives to, or signing lookups miss.
	for _, addr := range mgr.Addresses {
		key, ok := mgr.PrivateKeys[addr]
		if !ok {
			t.Fatalf("no key for %s", addr.Hex())
		}
		if got := crypto.PubkeyToAddress(key.PublicKey); got != addr {
			t.Errorf("key for %s derives to %s", addr.Hex(), got.Hex())
		}
	}
}

func TestLoadTestAccounts(t *testing.T) {
	// A written-out file, so the test carries its own fixture.
	good := filepath.Join(t.TempDir(), "accounts.json")
	const key = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	contents := `{"accounts":[{"address":"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266","privateKey":"0x` + key + `"}]}`
	if err := os.WriteFile(good, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	malformed := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(malformed, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		want    int
		wantErr bool
	}{
		{name: "from file", path: good, want: 1},
		{name: "empty path falls back to embedded", path: "", want: len(mustDefaultAccounts(t).Addresses)},
		{name: "missing file", path: filepath.Join(t.TempDir(), "nope.json"), wantErr: true},
		{name: "malformed file", path: malformed, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := LoadTestAccounts(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadTestAccounts(%q) err = nil, want error", tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadTestAccounts(%q): %v", tt.path, err)
			}
			if len(mgr.Addresses) != tt.want {
				t.Errorf("got %d accounts, want %d", len(mgr.Addresses), tt.want)
			}
		})
	}
}

func mustDefaultAccounts(t *testing.T) *TestAccountManager {
	t.Helper()
	mgr, err := DefaultTestAccounts()
	if err != nil {
		t.Fatalf("DefaultTestAccounts: %v", err)
	}
	return mgr
}
