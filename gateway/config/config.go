/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"time"

	"github.com/hyperledger/fabric-x-evm/common"
	endorser "github.com/hyperledger/fabric-x-evm/endorser/config"
)

// Config is the top-level configuration for the gateway application.
// It composes shared network configuration, gateway-specific configuration,
// endorser configurations, and server configuration.
type Config struct {
	Network   common.Network `mapstructure:"network" yaml:"network"`
	Endorsers []endorser.Endorser
	Gateway   Gateway
	Server    Server
}

// Gateway contains configuration for the gateway component.
type Gateway struct {
	Identity common.IdentityConfig `mapstructure:"identity" yaml:"identity"`

	DbConnStr  string // path to the sqlite database for blocks and transactions
	TrieDBPath string // path to PebbleDB trie database; empty = in-memory (dev/test only)

	Orderers       []common.ClientConfig `mapstructure:"orderers" yaml:"orderers"`
	SubmitWaitTime time.Duration         `mapstructure:"submit-wait-time" yaml:"submit-wait-time"`

	Committer   common.ClientConfig `mapstructure:"committer" yaml:"committer"`
	SyncTimeout time.Duration       `mapstructure:"sync-timemout" yaml:"sync-timeout"`

	TestAccountsPath string `mapstructure:"test-accounts-path" yaml:"test-accounts-path"` // Path to JSON file with test accounts for eth_accounts RPC
	EnableTestRPC    bool   `mapstructure:"enable-test-rpc" yaml:"enable-test-rpc"`       // Enable test-only RPC methods (eth_accounts, eth_sendTransaction) - UNSAFE for production
}

// Orderer contains configuration for an orderer node.
type Orderer struct {
	Address string
	TLS     common.TLSConfig
}

// Server contains HTTP server configuration.
type Server struct {
	Bind string
}
