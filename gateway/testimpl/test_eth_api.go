/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments. These methods perform server-side
transaction signing which is inherently insecure and should only be used
for development and testing purposes.
*/

package testimpl

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
)

// TestEthAPI wraps the production EthAPI and adds test-only RPC methods.
// This wrapper provides eth_accounts and eth_sendTransaction with server-side signing.
//
// SECURITY WARNING: Server-side signing is inherently insecure and should NEVER
// be used in production. This is only for development/testing with Hardhat, etc.
type TestEthAPI struct {
	*api.EthAPI
	backend         api.Backend
	testAccounts    []common.Address
	testAccountKeys map[common.Address]*ecdsa.PrivateKey
	fence           *txFence
}

// NewTestEthAPI creates a test-enabled Ethereum API wrapper.
// It embeds the production API and adds unsafe test-only methods.
func NewTestEthAPI(prodAPI *api.EthAPI, backend api.Backend, accounts []common.Address, keys map[common.Address]*ecdsa.PrivateKey, fence *txFence) *TestEthAPI {
	return &TestEthAPI{
		EthAPI:          prodAPI,
		backend:         backend,
		testAccounts:    accounts,
		testAccountKeys: keys,
		fence:           fence,
	}
}

// Accounts returns the list of test accounts (eth_accounts).
// This is a test-only method that exposes server-managed accounts.
func (api *TestEthAPI) Accounts(ctx context.Context) ([]common.Address, error) {
	return api.testAccounts, nil
}

// SendTransaction signs and sends a transaction using server-side keys (eth_sendTransaction).
// This is UNSAFE and should only be used for testing.
//
// SECURITY WARNING: This method performs server-side transaction signing,
// which means the server has access to private keys. This is acceptable for
// development/testing but is a critical security vulnerability in production.
func (api *TestEthAPI) SendTransaction(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	txHash, err := api.signAndEnqueue(ctx, args)
	if err != nil {
		return common.Hash{}, err
	}
	return txHash, api.awaitCommit(ctx, txHash)
}

// signAndEnqueue signs args and hands the transaction to the gateway. The fence
// spans the nonce read as well, so a rewind can't land in between and leave us
// signing against a nonce that no longer exists.
func (api *TestEthAPI) signAndEnqueue(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	return api.fence.submit(func() (common.Hash, error) {
		return api.signAndEnqueueLocked(ctx, args)
	})
}

func (api *TestEthAPI) signAndEnqueueLocked(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	// Validate from address
	if args.From == nil {
		return common.Hash{}, fmt.Errorf("missing 'from' field")
	}

	// Get private key for this address
	privateKey, ok := api.testAccountKeys[*args.From]
	if !ok {
		return common.Hash{}, fmt.Errorf("no private key available for address %s", args.From.Hex())
	}

	// Set defaults for unspecified fields
	args.setDefaults()

	// Get nonce if not specified
	var nonce uint64
	if args.Nonce != nil {
		nonce = uint64(*args.Nonce)
	} else {
		var err error
		nonce, err = api.backend.NonceAt(ctx, *args.From, nil)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to get nonce: %w", err)
		}
	}

	// Get chainID
	chainID, err := api.backend.ChainID(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get chainID: %w", err)
	}

	// Build transaction
	var tx *types.Transaction
	data := args.data()
	gasLimit := uint64(*args.Gas)
	value := (*big.Int)(args.Value)
	gasPrice := (*big.Int)(args.GasPrice)

	if args.To != nil {
		// Contract call or transfer
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			To:       args.To,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	} else {
		// Contract deployment
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			To:       nil,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	}

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send signed transaction using raw transaction
	txBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to marshal transaction: %w", err)
	}

	// Enqueue only: the caller waits for the commit outside the fence.
	return api.EthAPI.SendRawTransaction(ctx, hexutil.Bytes(txBytes))
}

// SendRawTransaction overrides the base implementation to make it synchronous for Hardhat compatibility.
// It sends the transaction and polls until it's committed to a block, mimicking Hardhat's auto-mining behavior.
func (api *TestEthAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	txHash, err := api.fence.submit(func() (common.Hash, error) {
		return api.EthAPI.SendRawTransaction(ctx, input)
	})
	if err != nil {
		return common.Hash{}, err
	}
	return txHash, api.awaitCommit(ctx, txHash)
}

// commitPollInterval paces the wait below; sleeping rather than spinning keeps a
// hot loop from starving the gateway workers we are waiting on.
const commitPollInterval = time.Millisecond

// commitTimeout bounds the wait for a single transaction. The fence is already
// released by now, so a transaction that never commits delays only its own caller.
const commitTimeout = 30 * time.Second

// awaitCommit blocks until the transaction reaches a block, mimicking Hardhat's
// auto-mining. Deliberately outside the fence; see txFence.
func (api *TestEthAPI) awaitCommit(ctx context.Context, txHash common.Hash) error {
	deadline := time.NewTimer(commitTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(commitPollInterval)
	defer poll.Stop()

	for {
		tx, err := api.backend.TransactionByHash(ctx, txHash)
		switch {
		case err != nil:
			// Not visible yet; keep waiting.
			hardhatLogger.Debugf("polling TransactionByHash for %s: %v", txHash, err)
		case tx == nil:
			// Gone from both the queue and the store: the gateway worker failed it and
			// dropped it (gateway/core.Gateway.worker), so no block will ever carry it.
			// TODO: delete once a worker-failed transaction gets a terminal state of its
			// own in gateway/core, rather than becoming indistinguishable from unknown.
			return fmt.Errorf("transaction %s was dropped before it committed", txHash)
		case tx.BlockNumber > 0:
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("transaction %s did not commit within %s", txHash, commitTimeout)
		case <-poll.C:
		}
	}
}
