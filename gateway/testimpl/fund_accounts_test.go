/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
)

const testNS = "basic"

func TestDefaultTestAccountBalance_IsHardhatDefault(t *testing.T) {
	want := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(params.Ether))
	if DefaultTestAccountBalance.Cmp(want) != 0 {
		t.Fatalf("DefaultTestAccountBalance = %s, want %s", DefaultTestAccountBalance, want)
	}
}

func TestFundTestAccounts_SeedsBalancesReadableViaStateDB(t *testing.T) {
	kvs := estorage.NewRevertibleLightKVS(estorage.NewLightKVS(8))
	addrs := []common.Address{
		common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
		common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
	}
	balance := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(params.Ether))

	if err := FundTestAccounts(kvs, testNS, addrs, balance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	// History must not advance — funding is genesis-like, not a new block.
	if n := kvs.NextIndex.Load(); n != 0 {
		t.Fatalf("NextIndex = %d after fund, want 0 (no history advance)", n)
	}

	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer reader.Close()

	stateDB, err := execution.NewStateDB(context.Background(), reader, testNS, 0, true)
	if err != nil {
		t.Fatalf("NewStateDB: %v", err)
	}

	for _, addr := range addrs {
		got := stateDB.GetBalance(addr).ToBig()
		if got.Cmp(balance) != 0 {
			t.Errorf("balance of %s = %s, want %s", addr.Hex(), got, balance)
		}
		if !stateDB.Exist(addr) {
			t.Errorf("account %s should Exist after funding", addr.Hex())
		}
	}

	// Unfunded address stays at zero.
	other := common.HexToAddress("0x0000000000000000000000000000000000000001")
	if got := stateDB.GetBalance(other).ToBig(); got.Sign() != 0 {
		t.Errorf("unfunded account balance = %s, want 0", got)
	}
}

func TestFundTestAccounts_DefaultAccounts(t *testing.T) {
	kvs := estorage.NewLightKVS(4)
	mgr, err := DefaultTestAccounts()
	if err != nil {
		t.Fatalf("DefaultTestAccounts: %v", err)
	}
	if len(mgr.Addresses) == 0 {
		t.Fatal("expected Hardhat test accounts, got none")
	}

	if err := FundTestAccounts(kvs, testNS, mgr.Addresses, DefaultTestAccountBalance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer reader.Close()

	// Spot-check via KVS Get (namespace-qualified key path used by StateDB).
	for _, addr := range mgr.Addresses {
		rec, err := reader.Get(testNS, "acc:"+addr.Hex()+":bal")
		if err != nil {
			t.Fatalf("Get bal for %s: %v", addr.Hex(), err)
		}
		if rec == nil || len(rec.Value) == 0 {
			t.Fatalf("missing balance for %s", addr.Hex())
		}
		got := new(big.Int).SetBytes(rec.Value)
		if got.Cmp(DefaultTestAccountBalance) != 0 {
			t.Errorf("%s balance = %s, want %s", addr.Hex(), got, DefaultTestAccountBalance)
		}
	}
}

func TestFundTestAccounts_NoopOnZeroOrEmpty(t *testing.T) {
	kvs := estorage.NewLightKVS(2)
	addr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	if err := FundTestAccounts(kvs, testNS, []common.Address{addr}, big.NewInt(0)); err != nil {
		t.Fatalf("zero balance: %v", err)
	}
	if err := FundTestAccounts(kvs, testNS, nil, DefaultTestAccountBalance); err != nil {
		t.Fatalf("empty addrs: %v", err)
	}
	if err := FundTestAccounts(kvs, testNS, []common.Address{addr}, nil); err != nil {
		t.Fatalf("nil balance: %v", err)
	}

	snap := kvs.Current.Load()
	if len(snap.Data) != 0 {
		t.Fatalf("expected no keys written, got %d", len(snap.Data))
	}
}

func TestFundTestAccounts_SurvivesLaterUpdate(t *testing.T) {
	kvs := estorage.NewLightKVS(4)
	addr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	if err := FundTestAccounts(kvs, testNS, []common.Address{addr}, DefaultTestAccountBalance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	// Simulate a later block write (unrelated key) — funded balance must clone through.
	if err := kvs.Update([]estorage.KeyValueVersion{{
		Key:      testNS + ":str:" + addr.Hex() + ":0x00",
		Value:    []byte{1},
		BlockNum: 1,
		TxNum:    0,
		TxID:     "tx-1",
	}}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer reader.Close()

	rec, err := reader.Get(testNS, "acc:"+addr.Hex()+":bal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("balance key missing after Update clone")
	}
	got := new(big.Int).SetBytes(rec.Value)
	if got.Cmp(DefaultTestAccountBalance) != 0 {
		t.Fatalf("balance after Update = %s, want %s", got, DefaultTestAccountBalance)
	}
}

func TestFundTestAccounts_UnsupportedKVS(t *testing.T) {
	// VersionedDBWrapper is not supported for in-place funding.
	err := FundTestAccounts(nil, testNS, []common.Address{{1}}, DefaultTestAccountBalance)
	if err == nil {
		t.Fatal("expected error for nil/unsupported KVS")
	}
}
