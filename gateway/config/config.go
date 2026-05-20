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

// Config is the top-level configuration for the combined (embedded-endorsers) deployment.
type Config struct {
	Network   common.Network      `mapstructure:"network"   yaml:"network"`
	Gateway   Gateway             `mapstructure:"gateway"   yaml:"gateway"`
	Endorsers []endorser.Endorser `mapstructure:"endorsers" yaml:"endorsers"`
}

// Gateway contains configuration for the gateway component.
type Gateway struct {
	Listen string `mapstructure:"listen" yaml:"listen"` // HTTP listen address for the Ethereum JSON-RPC API

	Identity common.IdentityConfig `mapstructure:"identity" yaml:"identity"`

	Database DB `mapstructure:"database" yaml:"database"`

	Orderers  []common.ClientConfig `mapstructure:"orderers"  yaml:"orderers"`
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`

	SyncTimeout time.Duration `mapstructure:"sync-timeout" yaml:"sync-timeout"`

	TestAccountsPath string `mapstructure:"test-accounts-path" yaml:"test-accounts-path"` // Path to JSON file with test accounts for eth_accounts RPC
	EnableTestRPC    bool   `mapstructure:"enable-test-rpc"    yaml:"enable-test-rpc"`    // Enable test-only RPC methods (eth_accounts, eth_sendTransaction) - UNSAFE for production

	WorkerCount int `mapstructure:"worker-count" yaml:"worker-count"` // number of worker goroutines; defaults to 1 if not set
}

// DB holds the database paths for the gateway.
type DB struct {
	ConnString string `mapstructure:"connection-string" yaml:"connection-string"` // SQLite connection string for blocks, transactions, and logs
	TriePath   string `mapstructure:"trie-path"         yaml:"trie-path"`         // PebbleDB directory for state root trie; empty = in-memory
}
