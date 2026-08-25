/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

// TestTestNode_HardhatSetBalance exercises hardhat_setBalance end-to-end against the
// self-contained testnode: read-back after set, the delta-down path, block-height
// progression, and spending a setBalance-funded account via a real value transfer.
func TestTestNode_HardhatSetBalance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	application, err := NewTestNode(ctx, TestNodeConfig{
		Listen:  "127.0.0.1:0",
		ChainID: 31337,
	})
	if err != nil {
		t.Fatalf("NewTestNode: %v", err)
	}

	// The commit pipeline only runs once the app is started, so run it in the background.
	runCtx, runCancel := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- application.Run(runCtx) }()
	t.Cleanup(func() {
		runCancel()
		select {
		case <-runErr:
		case <-time.After(15 * time.Second):
			t.Log("timed out waiting for App.Run to return")
		}
	})

	gw := application.Gateway()

	waitReady(t, ctx, gw)

	// Drive the RPC the way a client would: in-proc server over the real hardhat + eth APIs.
	srv := rpc.NewServer()
	if err := srv.RegisterName("hardhat", testimpl.NewHardhatAPI(gw)); err != nil {
		t.Fatalf("register hardhat: %v", err)
	}
	if err := srv.RegisterName("eth", gwapi.NewEthAPI(gw)); err != nil {
		t.Fatalf("register eth: %v", err)
	}
	rc := rpc.DialInProc(srv)
	t.Cleanup(rc.Close)
	ec := ethclient.NewClient(rc)

	addr := common.HexToAddress("0x00000000000000000000000000000000DeaDBeef")

	heightBefore, err := ec.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("eth_blockNumber (before): %v", err)
	}

	// --- Check 1: set + immediate read (sync guarantee) ---
	up := new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18))
	setBalance(t, ctx, rc, addr, up)

	balUp, err := ec.BalanceAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("eth_getBalance (after set-up): %v", err)
	}
	if balUp.Cmp(up) != 0 {
		t.Fatalf("balance after set-up = %s, want %s", balUp, up)
	}

	// --- Check 2: delta-down (SubBalance path) ---
	down := new(big.Int).Mul(big.NewInt(2), big.NewInt(1e18))
	setBalance(t, ctx, rc, addr, down)

	balDown, err := ec.BalanceAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("eth_getBalance (after set-down): %v", err)
	}
	if balDown.Cmp(down) != 0 {
		t.Fatalf("balance after set-down = %s, want %s", balDown, down)
	}

	// --- Check 3: block height progressed across the setBalance calls ---
	heightAfter, err := ec.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("eth_blockNumber (after): %v", err)
	}
	if heightAfter <= heightBefore {
		t.Fatalf("block height did not advance: before=%d after=%d", heightBefore, heightAfter)
	}

	// --- Check 4: compose with a real value transfer ---
	// A setBalance-funded EOA must be able to spend, proving the balance is real
	// ledger state and not a read-time illusion.
	senderKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate sender key: %v", err)
	}
	sender := crypto.PubkeyToAddress(senderKey.PublicKey)
	recipient := common.HexToAddress("0x000000000000000000000000000000000000C0FE")

	fund := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	setBalance(t, ctx, rc, sender, fund)

	transfer := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))

	chainID, err := ec.ChainID(ctx)
	if err != nil {
		t.Fatalf("eth_chainId: %v", err)
	}
	nonce, err := ec.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatalf("pending nonce: %v", err)
	}
	gasPrice, err := ec.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("suggest gas price: %v", err)
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &recipient,
		Value:    transfer,
		Gas:      21000,
		GasPrice: gasPrice,
	})
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), senderKey)
	if err != nil {
		t.Fatalf("sign value transfer: %v", err)
	}
	if err := ec.SendTransaction(ctx, signedTx); err != nil {
		t.Fatalf("send value transfer from setBalance-funded account: %v", err)
	}

	// eth_sendRawTransaction only enqueues, so wait for the transfer to commit.
	waitForBalance(t, ctx, ec, recipient, transfer)

	// Sanity: the sender was actually debited (value + gas).
	senderBal, err := ec.BalanceAt(ctx, sender, nil)
	if err != nil {
		t.Fatalf("eth_getBalance (sender after transfer): %v", err)
	}
	if senderBal.Cmp(new(big.Int).Sub(fund, transfer)) > 0 {
		t.Fatalf("sender balance after transfer = %s, want <= %s (fund - value)", senderBal, new(big.Int).Sub(fund, transfer))
	}
}

// setBalance calls hardhat_setBalance the way Hardhat does, passing wei as a hex quantity.
func setBalance(t *testing.T, ctx context.Context, rc *rpc.Client, addr common.Address, amount *big.Int) {
	t.Helper()
	var result interface{}
	if err := rc.CallContext(ctx, &result, "hardhat_setBalance", addr, (*hexutil.Big)(amount)); err != nil {
		t.Fatalf("hardhat_setBalance(%s, %s): %v", addr.Hex(), amount, err)
	}
}

// waitReady blocks until the gateway's block store is readable, i.e. the pipeline is live.
func waitReady(t *testing.T, ctx context.Context, gw interface {
	BlockNumber(context.Context) (uint64, error)
}) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		if _, err := gw.BlockNumber(ctx); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for gateway block store to become ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// waitForBalance polls until addr's balance equals want, or fails on timeout.
func waitForBalance(t *testing.T, ctx context.Context, ec *ethclient.Client, addr common.Address, want *big.Int) {
	t.Helper()
	deadline := time.After(45 * time.Second)
	for {
		bal, err := ec.BalanceAt(ctx, addr, nil)
		if err == nil && bal.Cmp(want) == 0 {
			return
		}
		select {
		case <-deadline:
			bal, _ := ec.BalanceAt(ctx, addr, nil)
			t.Fatalf("timed out waiting for balance of %s to reach %s (last read %s)", addr.Hex(), want, bal)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
