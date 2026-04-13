/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
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
	DbConnStr        string // path to the sqlite database for blocks and transactions
	TrieDBPath       string // path to PebbleDB trie database; empty = in-memory (dev/test only)
	Orderers         []Orderer
	SubmitWaitTime   time.Duration
	SyncPeerAddr     string
	SyncPeerTLS      string
	SyncTimeout      time.Duration
	TestAccountsPath string // Path to JSON file with test accounts for eth_accounts RPC
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