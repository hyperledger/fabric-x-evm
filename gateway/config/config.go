/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	endorser "github.com/hyperledger/fabric-x-evm/endorser/config"
)

// Config is the top-level configuration for the gateway application.
// It composes shared network configuration, gateway-specific configuration,
// endorser configurations, and server configuration.
type Config struct {
	Network   Network
	Endorsers []endorser.Endorser
	Gateway   Gateway
	Server    Server
}

// Network contains network details shared across components
// and network participants.
type Network struct {
	// Channel is the Fabric channel.
	Channel string

	// Namespace is the namespace for all token transactions.
	Namespace string

	// NsVersion is the version of the namespace, usually 1.0.
	NsVersion string

	// ChainID is the ethereum-style chain ID for this network.
	ChainID int64
}

// Gateway contains configuration for the gateway component.
type Gateway struct {
	SignerMSPDir     string
	SignerMSPID      string
	DbConnStr        string
	Orderers         []Orderer
	SubmitWaitTime   time.Duration
	SyncPeerAddr     string
	SyncPeerTLS      string
	SyncTimeout      time.Duration
	TestAccountsPath string            // Path to JSON file with test accounts for eth_accounts RPC
	TestAccounts     []string          // Loaded test account addresses
	TestAccountKeys  map[string]string // Map of address -> private key for signing
}

// Orderer contains configuration for an orderer node.
type Orderer struct {
	Address string
	TLSPath string
}

// Server contains HTTP server configuration.
type Server struct {
	Bind string
}

// TestAccount represents a test account with address and private key
type TestAccount struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
}

// TestAccountsFile represents the structure of the test accounts JSON file
type TestAccountsFile struct {
	Accounts []TestAccount `json:"accounts"`
}

// LoadTestAccounts loads test accounts from a JSON file if TestAccountsPath is set
func (c *Config) LoadTestAccounts() error {
	if c.Gateway.TestAccountsPath == "" {
		return nil // No test accounts file configured
	}

	data, err := os.ReadFile(c.Gateway.TestAccountsPath)
	if err != nil {
		return fmt.Errorf("failed to read test accounts file: %w", err)
	}

	var accountsFile TestAccountsFile
	if err := json.Unmarshal(data, &accountsFile); err != nil {
		return fmt.Errorf("failed to parse test accounts JSON: %w", err)
	}

	// Extract addresses and private keys
	c.Gateway.TestAccounts = make([]string, len(accountsFile.Accounts))
	c.Gateway.TestAccountKeys = make(map[string]string)
	for i, acc := range accountsFile.Accounts {
		c.Gateway.TestAccounts[i] = acc.Address
		c.Gateway.TestAccountKeys[acc.Address] = acc.PrivateKey
	}
	
	return nil
}