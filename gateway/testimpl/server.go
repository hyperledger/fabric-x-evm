/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
)

// NewTestServer creates an RPC server with test-only methods enabled.
// This wraps the production server and adds eth_accounts and eth_sendTransaction
// with server-side signing capabilities, plus Hardhat-specific helper methods.
//
// SECURITY WARNING: This server performs server-side transaction signing,
// which is inherently insecure. Use ONLY for development and testing.
// NEVER use in production environments.
func NewTestServer(b api.Backend, testAccounts []common.Address, testAccountKeys map[common.Address]*ecdsa.PrivateKey) (*rpc.Server, error) {
	srv := rpc.NewServer()

	// Create production API
	prodAPI := api.NewEthAPI(b)

	// Wrap with test API that adds unsafe methods
	testAPI := NewTestEthAPI(prodAPI, b, testAccounts, testAccountKeys)

	// Register the test-enabled API
	if err := srv.RegisterName("eth", testAPI); err != nil {
		return nil, err
	}

	// Register other standard APIs
	chainID, err := b.ChainID(context.TODO())
	if err != nil {
		return nil, err
	}
	if err := srv.RegisterName("net", api.NewNetAPI(chainID.String())); err != nil {
		return nil, err
	}

	// Register Web3 API with Hardhat-compatible client version
	if err := srv.RegisterName("web3", NewTestWeb3API()); err != nil {
		return nil, err
	}

	// Register Hardhat helper APIs for test compatibility
	if err := srv.RegisterName("hardhat", NewHardhatAPI()); err != nil {
		return nil, err
	}
	if err := srv.RegisterName("evm", NewEvmAPI()); err != nil {
		return nil, err
	}

	return srv, nil
}

// TestWeb3API provides Web3 API with Hardhat-compatible client version.
type TestWeb3API struct{}

// NewTestWeb3API creates a new test Web3 API instance.
func NewTestWeb3API() *TestWeb3API {
	return &TestWeb3API{}
}

// ClientVersion returns a Hardhat-compatible client version string.
// This helps Hardhat recognize the node and enable its network helpers.
func (api *TestWeb3API) ClientVersion() string {
	return "HardhatNetwork/fabric-evm/0.1.0"
}
