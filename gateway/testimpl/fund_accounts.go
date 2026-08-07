/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// DefaultTestAccountBalance is Hardhat Network's default genesis balance per
// account: 10_000 ETH. Matches @nomicfoundation/hardhat-network-helpers and
// testdata/openzeppelin-contracts/test/helpers/account.js.
var DefaultTestAccountBalance = new(big.Int).Mul(big.NewInt(10_000), big.NewInt(params.Ether))

// FundTestAccounts seeds native ETH balances for the given addresses into the
// endorser KVS. Intended for test-RPC / testnode startup only: production
// accounts correctly start at zero.
//
// Balances are applied through the normal StateDB write path and committed with
// KVS.Handle on a synthetic block, so versions and keys match real commits and
// any KVS backend that implements Handle works (not only LightKVS in place).
func FundTestAccounts(ctx context.Context, kvs estorage.KVS, namespace string, addresses []common.Address, balance *big.Int) error {
	if kvs == nil {
		return fmt.Errorf("fund test accounts: nil KVS")
	}
	if balance == nil || balance.Sign() <= 0 {
		return nil
	}
	if len(addresses) == 0 {
		return nil
	}

	// Latest snapshot (block 0 means latest on current KVS APIs).
	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		return fmt.Errorf("fund test accounts: snapshot: %w", err)
	}
	defer reader.Close()

	stateDB, err := execution.NewStateDB(ctx, reader, namespace, 0, true)
	if err != nil {
		return fmt.Errorf("fund test accounts: statedb: %w", err)
	}

	amount := uint256.MustFromBig(balance)
	for _, addr := range addresses {
		stateDB.CreateAccount(addr)
		stateDB.AddBalance(addr, amount, tracing.BalanceChangeUnspecified)
	}

	rws := stateDB.Result()
	if len(rws.Writes) == 0 {
		return fmt.Errorf("fund test accounts: statedb produced no writes")
	}

	blockNum, err := kvs.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("fund test accounts: block number: %w", err)
	}

	// Synthetic block through the same Handle path real commits use, so Update
	// assigns versions and any BlockHandler-backed KVS accepts the writes.
	block := blocks.Block{
		Number: blockNum,
		Transactions: []blocks.Transaction{
			{
				ID:     "test-account-funding",
				Number: 0,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{
					{
						Namespace: namespace,
						RWS:       rws,
					},
				},
			},
		},
	}
	if err := kvs.Handle(ctx, block); err != nil {
		return fmt.Errorf("fund test accounts: handle: %w", err)
	}
	return nil
}
