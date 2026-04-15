/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestAccount represents a test account with address and private key
type TestAccount struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
}

// TestAccountsFile represents the structure of the test accounts JSON file
type TestAccountsFile struct {
	Accounts []TestAccount `json:"accounts"`
}

// TestAccountManager manages test accounts for development/testing
type TestAccountManager struct {
	Addresses   []common.Address
	PrivateKeys map[common.Address]*ecdsa.PrivateKey
}

// LoadTestAccounts loads test accounts from a JSON file and pre-converts private keys
func LoadTestAccounts(path string) (*TestAccountManager, error) {
	if path == "" {
		// Return empty manager if no path configured
		return &TestAccountManager{
			Addresses:   []common.Address{},
			PrivateKeys: make(map[common.Address]*ecdsa.PrivateKey),
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test accounts file: %w", err)
	}

	var accountsFile TestAccountsFile
	if err := json.Unmarshal(data, &accountsFile); err != nil {
		return nil, fmt.Errorf("failed to parse test accounts JSON: %w", err)
	}

	manager := &TestAccountManager{
		Addresses:   make([]common.Address, len(accountsFile.Accounts)),
		PrivateKeys: make(map[common.Address]*ecdsa.PrivateKey),
	}

	// Pre-convert private keys to ECDSA
	for i, acc := range accountsFile.Accounts {
		addr := common.HexToAddress(acc.Address)
		manager.Addresses[i] = addr

		// Convert private key hex to ECDSA private key
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(acc.PrivateKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid private key for address %s: %w", acc.Address, err)
		}
		manager.PrivateKeys[addr] = privateKey
	}

	return manager, nil
}
