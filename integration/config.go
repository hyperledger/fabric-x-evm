/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
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
	orgsDir := filepath.Join(testdataDir, "fabric-samples/test-network/organizations")

	x := rand.Int64()

	return config.Config{
		Network: config.Network{
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   31337,
		},
		Gateway: config.Gateway{
			SignerMSPDir: filepath.Join(orgsDir, "peerOrganizations/org1.example.com/users/User1@org1.example.com/msp"),
			SignerMSPID:  "Org1MSP",
			DbConnStr:    "file:blocks?mode=memory&cache=shared",
			TrieDBPath:   "",
			// DbConnStr:      "file:../testdata/blocks.db",
			SubmitWaitTime: 2200 * time.Millisecond,
			SyncPeerAddr:   "localhost:7051",
			SyncPeerTLS:    filepath.Join(orgsDir, "peerOrganizations/org1.example.com/tlsca/tlsca.org1.example.com-cert.pem"),
			SyncTimeout:    10 * time.Second,
			Orderers: []config.Orderer{
				{
					Address: "localhost:7050",
					TLSPath: filepath.Join(orgsDir, "ordererOrganizations/example.com/tlsca/tlsca.example.com-cert.pem"),
				},
			},
		},
		Endorsers: []econf.Endorser{
			{
				Name:      "org1",
				PeerAddr:  "localhost:7051",
				PeerTLS:   filepath.Join(orgsDir, "peerOrganizations/org1.example.com/tlsca/tlsca.org1.example.com-cert.pem"),
				MspDir:    filepath.Join(orgsDir, "peerOrganizations/org1.example.com/peers/peer0.org1.example.com/msp"),
				MspID:     "Org1MSP",
				DbConnStr: fmt.Sprintf("file:endorser1%d?mode=memory&cache=shared", x),
				// DbConnStr: "file:../testdata/endorser1.db",
			},
			{
				Name:      "org2",
				PeerAddr:  "localhost:9051",
				PeerTLS:   filepath.Join(orgsDir, "peerOrganizations/org2.example.com/tlsca/tlsca.org2.example.com-cert.pem"),
				MspDir:    filepath.Join(orgsDir, "peerOrganizations/org2.example.com/peers/peer0.org2.example.com/msp"),
				MspID:     "Org2MSP",
				DbConnStr: fmt.Sprintf("file:endorser2%d?mode=memory&cache=shared", x),
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
	var orgsDir string
	if testdataDir := os.Getenv("TESTDATA"); testdataDir != "" {
		orgsDir = testdataDir
	} else {
		// Find project root and construct path to testdata/crypto
		projectRoot, err := findProjectRoot()
		if err != nil {
			// Fallback to relative path (works from integration/ directory)
			orgsDir = "../testdata/crypto"
		} else {
			orgsDir = filepath.Join(projectRoot, "testdata", "crypto")
		}
	}

	org1 := filepath.Join(orgsDir, "peerOrganizations", "org1.example.com")

	return config.Config{
		Network: config.Network{
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   31337,
		},
		Gateway: config.Gateway{
			SignerMSPDir:   filepath.Join(org1, "users", "User1@org1.example.com", "msp"),
			SignerMSPID:    "Org1MSP",
			DbConnStr:      "file:blocks?mode=memory&cache=shared",
			SubmitWaitTime: 200 * time.Millisecond,
			SyncPeerAddr:   "127.0.0.1:4001",
			SyncPeerTLS:    "", // No TLS for X test committer
			SyncTimeout:    10 * time.Second,
			Orderers: []config.Orderer{{
				Address: "127.0.0.1:7050",
				TLSPath: "", // No TLS path for X test committer
			}},
		},
		Endorsers: []econf.Endorser{
			{
				Name:      "org1",
				PeerAddr:  "127.0.0.1:4001",
				PeerTLS:   "", // No TLS for X test committer
				MspDir:    filepath.Join(org1, "peers", "endorser.org1.example.com", "msp"),
				MspID:     "Org1MSP",
				DbConnStr: "file:db1?mode=memory&cache=shared",
				// DbConnStr: "endorser.sqlite",
			},
		},
		Server: config.Server{
			Bind: "0.0.0.0:8545",
		},
	}
}
