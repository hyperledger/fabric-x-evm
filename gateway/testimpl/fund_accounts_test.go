/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
)

const testNS = "basic"

func TestFundTestAccounts_SeedsBalancesReadableViaStateDB(t *testing.T) {
	kvs := estorage.NewRevertibleLightKVS(estorage.NewLightKVS(8))
	addrs := []common.Address{
		common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
		common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
	}
	balance := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(params.Ether))

	if err := FundTestAccounts(t.Context(), kvs, testNS, addrs, balance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer reader.Close()

	stateDB, err := execution.NewStateDB(t.Context(), reader, testNS, 0, true)
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

	if err := FundTestAccounts(t.Context(), kvs, testNS, mgr.Addresses, DefaultTestAccountBalance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	reader, err := kvs.NewSnapshot(nil)
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
		if rec.TxID != "test-account-funding" {
			t.Errorf("%s TxID = %q, want test-account-funding", addr.Hex(), rec.TxID)
		}
	}
}

func TestFundTestAccounts_NoopOnZeroOrEmpty(t *testing.T) {
	kvs := estorage.NewLightKVS(2)
	addr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	if err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{addr}, big.NewInt(0)); err != nil {
		t.Fatalf("zero balance: %v", err)
	}
	if err := FundTestAccounts(t.Context(), kvs, testNS, nil, DefaultTestAccountBalance); err != nil {
		t.Fatalf("empty addrs: %v", err)
	}
	if err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{addr}, nil); err != nil {
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
	if err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{addr}, DefaultTestAccountBalance); err != nil {
		t.Fatalf("FundTestAccounts: %v", err)
	}

	// Simulate a later block write (unrelated key); funded balance must clone through.
	if err := kvs.Update([]estorage.KeyValueVersion{{
		Key:      testNS + ":str:" + addr.Hex() + ":0x00",
		Value:    []byte{1},
		BlockNum: 1,
		TxNum:    0,
		TxID:     "tx-1",
	}}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reader, err := kvs.NewSnapshot(nil)
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

func TestFundTestAccounts_NilKVS(t *testing.T) {
	err := FundTestAccounts(t.Context(), nil, testNS, []common.Address{{1}}, DefaultTestAccountBalance)
	if err == nil {
		t.Fatal("expected error for nil KVS")
	}
}

func TestFundTestAccounts_NegativeBalanceIsNoop(t *testing.T) {
	kvs := estorage.NewLightKVS(2)
	err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{{1}}, big.NewInt(-1))
	if err != nil {
		t.Fatalf("negative balance: %v", err)
	}
	if len(kvs.Current.Load().Data) != 0 {
		t.Fatal("expected no keys for negative balance")
	}
}

func TestFundTestAccounts_UsesHandleVersions(t *testing.T) {
	// Two funding rounds for the same account should bump the key version via Update.
	kvs := estorage.NewLightKVS(4)
	addr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	half := new(big.Int).Mul(big.NewInt(5_000), big.NewInt(params.Ether))

	if err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{addr}, half); err != nil {
		t.Fatalf("first fund: %v", err)
	}
	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	rec1, err := reader.Get(testNS, "acc:"+addr.Hex()+":bal")
	reader.Close()
	if err != nil || rec1 == nil {
		t.Fatalf("first bal: rec=%v err=%v", rec1, err)
	}
	if rec1.Version != 0 {
		t.Fatalf("first write version = %d, want 0", rec1.Version)
	}

	// Second fund AddBalances again (creates a new write through Handle).
	if err := FundTestAccounts(t.Context(), kvs, testNS, []common.Address{addr}, half); err != nil {
		t.Fatalf("second fund: %v", err)
	}
	reader, err = kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	defer reader.Close()
	rec2, err := reader.Get(testNS, "acc:"+addr.Hex()+":bal")
	if err != nil || rec2 == nil {
		t.Fatalf("second bal: rec=%v err=%v", rec2, err)
	}
	if rec2.Version != 1 {
		t.Fatalf("second write version = %d, want 1 (Handle/Update path)", rec2.Version)
	}
	// CreateAccount resets bal to 0 before AddBalance, so the second fund ends at half.
	got := new(big.Int).SetBytes(rec2.Value)
	if got.Cmp(half) != 0 {
		t.Fatalf("balance after second fund = %s, want %s", got, half)
	}
}
