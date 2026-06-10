/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"errors"
	"flag"
	"fmt"
	"time"
)

const (
	// TLSModeNone disables TLS on mock gRPC listeners.
	TLSModeNone = "none"
	// TLSModeMTLS enables mutual TLS on mock gRPC listeners.
	TLSModeMTLS = "mtls"
)

// Config defines listener, TLS, and batching settings for the mock backend.
type Config struct {
	OrdererListen     string
	CommitterListen   string
	TLSMode           string
	OrdererCert       string
	OrdererKey        string
	OrdererClientCA   string
	CommitterCert     string
	CommitterKey      string
	CommitterClientCA string
	MaxTxPerBlock     int
	BlockTimeout      time.Duration
	QueueSize         int
	RetainedBlocks    int
}

// DefaultConfig returns settings matching the integration Fabric-X defaults.
func DefaultConfig() Config {
	return Config{
		OrdererListen:     "127.0.0.1:7050",
		CommitterListen:   "127.0.0.1:4001",
		TLSMode:           TLSModeMTLS,
		OrdererCert:       "testdata/crypto/ordererOrganizations/orderer-org-1/orderers/party1/router.orderer-org-1/tls/server.crt",
		OrdererKey:        "testdata/crypto/ordererOrganizations/orderer-org-1/orderers/party1/router.orderer-org-1/tls/server.key",
		OrdererClientCA:   "testdata/crypto/peerOrganizations/org1.example.com/tlsca/tlsca.org1.example.com-cert.pem",
		CommitterCert:     "testdata/crypto/peerOrganizations/org1.example.com/peers/committer-sidecar.org1.example.com/tls/server.crt",
		CommitterKey:      "testdata/crypto/peerOrganizations/org1.example.com/peers/committer-sidecar.org1.example.com/tls/server.key",
		CommitterClientCA: "testdata/crypto/peerOrganizations/org1.example.com/tlsca/tlsca.org1.example.com-cert.pem",
		MaxTxPerBlock:     1,
		BlockTimeout:      0,
		QueueSize:         65536,
		RetainedBlocks:    5000,
	}
}

// Validate rejects unsupported TLS modes and invalid batching settings.
func (c Config) Validate() error {
	if c.MaxTxPerBlock <= 0 {
		return errors.New("max-tx-per-block must be positive")
	}
	if c.QueueSize <= 0 {
		return errors.New("queue-size must be positive")
	}
	if c.RetainedBlocks < 0 {
		return errors.New("retained-blocks must be non-negative")
	}
	if c.TLSMode != TLSModeNone && c.TLSMode != TLSModeMTLS {
		return fmt.Errorf("tls-mode must be %q or %q", TLSModeNone, TLSModeMTLS)
	}
	return nil
}

// BindFlags binds command-line flags onto Config fields.
func (c *Config) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.OrdererListen, "orderer-listen", c.OrdererListen, "orderer AtomicBroadcast listen address")
	fs.StringVar(&c.CommitterListen, "committer-listen", c.CommitterListen, "committer sidecar listen address")
	fs.StringVar(&c.TLSMode, "tls-mode", c.TLSMode, "TLS mode: mtls or none")
	fs.StringVar(&c.OrdererCert, "orderer-cert", c.OrdererCert, "orderer server TLS certificate path")
	fs.StringVar(&c.OrdererKey, "orderer-key", c.OrdererKey, "orderer server TLS key path")
	fs.StringVar(&c.OrdererClientCA, "orderer-client-ca", c.OrdererClientCA, "CA for orderer client certificates")
	fs.StringVar(&c.CommitterCert, "committer-cert", c.CommitterCert, "committer server TLS certificate path")
	fs.StringVar(&c.CommitterKey, "committer-key", c.CommitterKey, "committer server TLS key path")
	fs.StringVar(&c.CommitterClientCA, "committer-client-ca", c.CommitterClientCA, "CA for committer client certificates")
	fs.IntVar(&c.MaxTxPerBlock, "max-tx-per-block", c.MaxTxPerBlock, "maximum transactions per mock block")
	fs.DurationVar(&c.BlockTimeout, "block-timeout", c.BlockTimeout, "maximum time before cutting partial block")
	fs.IntVar(&c.QueueSize, "queue-size", c.QueueSize, "accepted envelope queue size")
	fs.IntVar(&c.RetainedBlocks, "retained-blocks", c.RetainedBlocks, "number of recent blocks and event batches to retain in memory; 0 keeps all history")
}
