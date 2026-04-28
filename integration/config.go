/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"

	"github.com/hyperledger/fabric-x-evm/common"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-sdk/network"
)

// findProjectRoot walks up the directory tree to find the project root.
// It looks for a directory that has both go.mod and testdata/ subdirectory.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		// Check if this directory has both go.mod and testdata/
		hasGoMod := false
		hasTestdata := false

		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			hasGoMod = true
		}

		if info, err := os.Stat(filepath.Join(dir, "testdata")); err == nil && info.IsDir() {
			hasTestdata = true
		}

		if hasGoMod && hasTestdata {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (directory with go.mod and testdata not found)")
		}
		dir = parent
	}
}

// FabricSamplesConfig returns default configuration for a fabric-samples test network.
func FabricSamplesConfig(testdataDir string) config.Config {
	// If testdataDir is relative, make it absolute from project root
	if !filepath.IsAbs(testdataDir) {
		if projectRoot, err := findProjectRoot(); err == nil {
			testdataDir = filepath.Join(projectRoot, testdataDir)
		}
	}
	orgsDir := filepath.Join(testdataDir, "fabric-samples", "test-network", "organizations")
	org1 := path.Join(orgsDir, "peerOrganizations", "org1.example.com")
	org2 := path.Join(orgsDir, "peerOrganizations", "org2.example.com")
	orderer := path.Join(orgsDir, "ordererOrganizations", "example.com")

	endorser1 := path.Join(org1, "peers", "peer0.org1.example.com")
	endorser2 := path.Join(org2, "peers", "peer0.org2.example.com")
	user := path.Join(org1, "users", "User1@org1.example.com")
	x := rand.Int64()

	return config.Config{
		Network: common.Network{
			Protocol:  "fabric",
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   4011,
		},
		Gateway: config.Gateway{
			Orderers: []common.ClientConfig{{
				Endpoint: &common.Endpoint{Host: "localhost", Port: 7050},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeTLS,
					CACertPaths: []string{path.Join(orderer, "tlsca", "tlsca.example.com-cert.pem")},
				},
			}},
			Committer: common.ClientConfig{
				Endpoint: &common.Endpoint{Host: "localhost", Port: 7051},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeTLS,
					CACertPaths: []string{path.Join(endorser1, "tls", "ca.crt")},
				},
			},
			Identity: common.IdentityConfig{
				MspID:  "Org1MSP",
				MSPDir: path.Join(user, "msp"),
			},
			DbConnStr:  "file:gateway.db?mode=memory&cache=shared",
			TrieDBPath: filepath.Join(testdataDir, "triedb"),
		},
		Endorsers: []econf.Endorser{
			{
				Name: "org1",
				Committer: common.ClientConfig{
					Endpoint: &common.Endpoint{Host: "localhost", Port: 7051},
					TLS: common.TLSConfig{
						Mode:        network.TLSModeTLS,
						CACertPaths: []string{path.Join(endorser1, "tls", "ca.crt")},
					},
				},
				Identity: common.IdentityConfig{
					MspID:  "Org1MSP",
					MSPDir: filepath.Join(endorser1, "msp"),
				},
				DbConnStr: fmt.Sprintf("file:endorser1%d.db?mode=memory&cache=shared", x),
			},
			{
				Name: "org2",
				Committer: common.ClientConfig{
					Endpoint: &common.Endpoint{Host: "localhost", Port: 9051},
					TLS: common.TLSConfig{
						Mode:        network.TLSModeTLS,
						CACertPaths: []string{path.Join(endorser2, "tls", "ca.crt")},
					},
				},
				Identity: common.IdentityConfig{
					MspID:  "Org2MSP",
					MSPDir: filepath.Join(endorser2, "msp"),
				},
				DbConnStr: fmt.Sprintf("file:endorser2%d.db?mode=memory&cache=shared", x),
			},
		},
		Server: config.Server{
			Bind: "0.0.0.0:8545",
		},
	}
}

// XTestCommitterConfig returns configuration for the Fabric X test committer.
// This configuration is used for integration testing with a local test network.
func XTestCommitterConfig() config.Config {
	// Use TESTDATA environment variable if set, otherwise find project root
	testdataDir := cmp.Or(os.Getenv("TESTDATA"), "testdata")
	// If testdataDir is relative, make it absolute from project root
	if !filepath.IsAbs(testdataDir) {
		if projectRoot, err := findProjectRoot(); err == nil {
			testdataDir = filepath.Join(projectRoot, testdataDir)
		}
	}
	// COMMITTER_HOST overrides the committer hostname (useful when running in Docker Compose).
	committerHost := cmp.Or(os.Getenv("COMMITTER_HOST"), "127.0.0.1")

	org1 := path.Join(testdataDir, "crypto", "peerOrganizations", "Org1")
	committer := path.Join(org1, "peers", "committer.org1.example.com")
	endorser := path.Join(org1, "peers", "endorser.org1.example.com")
	user := path.Join(org1, "users", "User1@org1.example.com")

	return config.Config{
		Network: common.Network{
			Protocol:  "fabric-x",
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   4011,
		},
		Gateway: config.Gateway{
			Orderers: []common.ClientConfig{{
				Endpoint: &common.Endpoint{Host: committerHost, Port: 7050},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeMTLS,
					CertPath:    path.Join(user, "tls", "client.crt"),
					KeyPath:     path.Join(user, "tls", "client.key"),
					CACertPaths: []string{path.Join(committer, "tls", "ca.crt")},
				},
			}},
			Committer: common.ClientConfig{
				Endpoint: &common.Endpoint{Host: committerHost, Port: 4001},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeMTLS,
					CertPath:    path.Join(user, "tls", "client.crt"),
					KeyPath:     path.Join(user, "tls", "client.key"),
					CACertPaths: []string{path.Join(committer, "tls", "ca.crt")},
				},
			},
			Identity: common.IdentityConfig{
				MspID:  "Org1MSP",
				MSPDir: path.Join(user, "msp"),
			},
			DbConnStr:  "file:gateway.db?mode=memory&cache=shared",
			TrieDBPath: filepath.Join(testdataDir, "triedb"),
			// DbConnStr:      "file:../testdata/blocks.db",
		},
		Endorsers: []econf.Endorser{
			{
				Name: "org1",
				Committer: common.ClientConfig{
					Endpoint: &common.Endpoint{
						Host: committerHost,
						Port: 4001,
					},
					TLS: common.TLSConfig{
						Mode:        network.TLSModeMTLS,
						CertPath:    path.Join(user, "tls", "client.crt"),
						KeyPath:     path.Join(user, "tls", "client.key"),
						CACertPaths: []string{path.Join(committer, "tls", "ca.crt")},
					},
				},
				Identity: common.IdentityConfig{
					MspID:  "Org1MSP",
					MSPDir: filepath.Join(endorser, "msp"),
				},
				DbConnStr: "file:endorser1.db?mode=memory&cache=shared",
			},
		},
		Server: config.Server{
			Bind: "0.0.0.0:8545",
		},
	}
}

// FabloConfig returns default configuration for a Fablo-managed Fabric test network.
// Fablo generates crypto material under fablo-target/fabric-config/crypto-config/
// and uses different port assignments than fabric-samples.
func FabloConfig(testdataDir string) config.Config {
	if !filepath.IsAbs(testdataDir) {
		if projectRoot, err := findProjectRoot(); err == nil {
			testdataDir = filepath.Join(projectRoot, testdataDir)
		}
	}
	cryptoDir := filepath.Join(testdataDir, "fablo-target", "fabric-config", "crypto-config")
	org1 := path.Join(cryptoDir, "peerOrganizations", "org1.example.com")
	org2 := path.Join(cryptoDir, "peerOrganizations", "org2.example.com")
	// Fablo places orderer crypto under peerOrganizations (not ordererOrganizations)
	orderer := path.Join(cryptoDir, "peerOrganizations", "orderer.example.com")

	endorser1 := path.Join(org1, "peers", "peer0.org1.example.com")
	endorser2 := path.Join(org2, "peers", "peer0.org2.example.com")
	user := path.Join(org1, "users", "User1@org1.example.com")
	x := rand.Int64()

	return config.Config{
		Network: common.Network{
			Protocol:  "fabric",
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   31337,
		},
		Gateway: config.Gateway{
			Orderers: []common.ClientConfig{{
				Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 7030},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeTLS,
					ServerName:  "orderer0.group1.orderer.example.com",
					CACertPaths: []string{path.Join(orderer, "tlsca", "tlsca.orderer.example.com-cert.pem")},
				},
			}},
			Committer: common.ClientConfig{
				Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 7041},
				TLS: common.TLSConfig{
					Mode:        network.TLSModeTLS,
					ServerName:  "peer0.org1.example.com",
					CACertPaths: []string{path.Join(endorser1, "tls", "ca.crt")},
				},
			},
			Identity: common.IdentityConfig{
				MspID:  "Org1MSP",
				MSPDir: path.Join(user, "msp"),
			},
			DbConnStr:  "file:gateway.db?mode=memory&cache=shared",
			TrieDBPath: filepath.Join(testdataDir, "triedb"),
		},
		Endorsers: []econf.Endorser{
			{
				Name: "org1",
				Committer: common.ClientConfig{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 7041},
					TLS: common.TLSConfig{
						Mode:        network.TLSModeTLS,
						ServerName:  "peer0.org1.example.com",
						CACertPaths: []string{path.Join(endorser1, "tls", "ca.crt")},
					},
				},
				Identity: common.IdentityConfig{
					MspID:  "Org1MSP",
					MSPDir: filepath.Join(endorser1, "msp"),
				},
				DbConnStr: fmt.Sprintf("file:endorser1%d.db?mode=memory&cache=shared", x),
			},
			{
				Name: "org2",
				Committer: common.ClientConfig{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 7061},
					TLS: common.TLSConfig{
						Mode:        network.TLSModeTLS,
						ServerName:  "peer0.org2.example.com",
						CACertPaths: []string{path.Join(endorser2, "tls", "ca.crt")},
					},
				},
				Identity: common.IdentityConfig{
					MspID:  "Org2MSP",
					MSPDir: filepath.Join(endorser2, "msp"),
				},
				DbConnStr: fmt.Sprintf("file:endorser2%d.db?mode=memory&cache=shared", x),
			},
		},
		Server: config.Server{
			Bind: "0.0.0.0:8545",
		},
	}
}
