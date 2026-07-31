/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
)

// DefaultTestAccountBalance is Hardhat Network's default genesis balance per
// account: 10_000 ETH. Matches @nomicfoundation/hardhat-network-helpers and
// testdata/openzeppelin-contracts/test/helpers/account.js.
var DefaultTestAccountBalance = new(big.Int).Mul(big.NewInt(10_000), big.NewInt(params.Ether))

// FundTestAccounts seeds native ETH balances for the given addresses into the
// endorser KVS. Intended for test-RPC / testnode startup only: production
// accounts correctly start at zero.
//
// Writes land in the current snapshot in place (no history advance), so the
// funded balances look like genesis state and survive later Handle/Update
// clones as well as reverts that restore pre-funding block numbers.
//
// kvs must be *estorage.LightKVS or *estorage.RevertibleLightKVS (memory DB).
func FundTestAccounts(kvs estorage.KVS, namespace string, addresses []common.Address, balance *big.Int) error {
	if balance == nil || balance.Sign() <= 0 {
		return nil
	}
	if len(addresses) == 0 {
		return nil
	}

	snap, err := currentSnapshot(kvs)
	if err != nil {
		return err
	}

	balBytes := balance.Bytes()
	for _, addr := range addresses {
		// Key layout matches LightKVS.Reader.Get / collectWrites:
		// fullKey = namespace + ":" + accKey(addr, "bal")
		// accKey  = "acc:" + addr.Hex() + ":bal"
		key := namespace + ":acc:" + addr.Hex() + ":bal"
		// Copy bytes so callers can reuse the balance buffer later.
		val := append([]byte(nil), balBytes...)
		snap.Data[key] = &estorage.ValueVersion{
			Value:    val,
			BlockNum: snap.BlockNumber,
			TxNum:    0,
			Version:  0,
			TxID:     "test-account-funding",
		}
	}
	return nil
}

func currentSnapshot(kvs estorage.KVS) (*estorage.Snapshot, error) {
	switch k := kvs.(type) {
	case *estorage.RevertibleLightKVS:
		if k.LightKVS == nil {
			return nil, fmt.Errorf("fund test accounts: RevertibleLightKVS has nil LightKVS")
		}
		snap := k.Current.Load()
		if snap == nil {
			return nil, fmt.Errorf("fund test accounts: KVS has no current snapshot")
		}
		return snap, nil
	case *estorage.LightKVS:
		snap := k.Current.Load()
		if snap == nil {
			return nil, fmt.Errorf("fund test accounts: KVS has no current snapshot")
		}
		return snap, nil
	default:
		return nil, fmt.Errorf("fund test accounts: KVS type %T does not support in-place funding (need memory LightKVS)", kvs)
	}
}
