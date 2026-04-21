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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
)

// NewTestServer creates an RPC server with test-only methods enabled.
// This wraps the production server and adds eth_accounts and eth_sendTransaction
// with server-side signing capabilities.
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
	if err := srv.RegisterName("web3", api.NewWeb3API()); err != nil {
		return nil, err
	}

	// Register test-only Hardhat and EVM namespaces for snapshot/revert functionality
	if err := srv.RegisterName("hardhat", NewHardhatAPI()); err != nil {
		return nil, err
	}
	if err := srv.RegisterName("evm", NewEvmAPI()); err != nil {
		return nil, err
	}

	return srv, nil
}

// HardhatAPI provides Hardhat-specific test methods
type HardhatAPI struct {
	snapshotCounter int
}

func NewHardhatAPI() *HardhatAPI {
	return &HardhatAPI{snapshotCounter: 0}
}

// SetCode sets the code at a given address (hardhat_setCode)
// STUB: Returns true but doesn't actually modify state
func (api *HardhatAPI) SetCode(address common.Address, code string) (bool, error) {
	// TODO: Implement actual code modification when needed
	return true, nil
}

// EvmAPI provides EVM-specific test methods
type EvmAPI struct {
	snapshotCounter int
}

func NewEvmAPI() *EvmAPI {
	return &EvmAPI{snapshotCounter: 0}
}

// Snapshot creates a snapshot of the current state (evm_snapshot)
// STUB: Returns incrementing snapshot ID but doesn't actually save state
func (api *EvmAPI) Snapshot() (string, error) {
	api.snapshotCounter++
	// Return snapshot ID as hex string
	return fmt.Sprintf("0x%x", api.snapshotCounter), nil
}

// Revert reverts to a previous snapshot (evm_revert)
// STUB: Returns true but doesn't actually restore state
func (api *EvmAPI) Revert(snapshotID string) (bool, error) {
	// TODO: Implement actual state restoration when needed
	return true, nil
}
