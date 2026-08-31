/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

// runtimeReturn42 is a minimal piece of EVM runtime bytecode:
//
//	60 2a   PUSH1 0x2a   ; the value 42
//	60 00   PUSH1 0x00   ; memory offset 0
//	52      MSTORE       ; mem[0:32] = 42
//	60 20   PUSH1 0x20   ; return length 32
//	60 00   PUSH1 0x00   ; return offset 0
//	f3      RETURN       ; return the 32-byte word 42
//
// Calling an account whose code is this must yield the 32-byte big-endian
// encoding of 42. That only happens if the injected code actually EXECUTES in
// the EVM, which is what makes it a stronger check than reading the code back.
var runtimeReturn42 = common.FromHex("0x602a60005260206000f3")

// TestTestNode_HardhatSetCode exercises hardhat_setCode end-to-end against the
// self-contained testnode: set + immediate read, executable injected code,
// clearing with empty code, and balance untouched.
func TestTestNode_HardhatSetCode(t *testing.T) {
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

	addr := common.HexToAddress("0x00000000000000000000000000000000C0DE0042")

	// Seed a known balance BEFORE touching the code, so check 4 can prove the
	// setCode directive left the balance alone.
	want := new(big.Int).Mul(big.NewInt(7), big.NewInt(1e18)) // 7 ETH in wei
	setBalance(t, ctx, rc, addr, want)

	// --- Check 1: set + immediate read (sync guarantee) ---
	setCode(t, ctx, rc, addr, runtimeReturn42)

	got, err := ec.CodeAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("eth_getCode (after set): %v", err)
	}
	if !bytes.Equal(got, runtimeReturn42) {
		t.Fatalf("code after set = %s, want %s", hexutil.Encode(got), hexutil.Encode(runtimeReturn42))
	}

	// --- Check 2: the injected code is EXECUTABLE, not just stored bytes ---
	ret, err := ec.CallContract(ctx, ethereum.CallMsg{To: &addr}, nil)
	if err != nil {
		t.Fatalf("eth_call against injected code: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("eth_call returned %d bytes (%s), want 32", len(ret), hexutil.Encode(ret))
	}
	if v := new(big.Int).SetBytes(ret); v.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("eth_call returned %s (raw %s), want 42", v, hexutil.Encode(ret))
	}

	// --- Check 3: empty code clears the account's code ---
	setCode(t, ctx, rc, addr, nil)

	cleared, err := ec.CodeAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("eth_getCode (after clear): %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("code after clear = %s, want empty", hexutil.Encode(cleared))
	}

	// --- Check 4: the balance was never touched by the setCode directives ---
	bal, err := ec.BalanceAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("eth_getBalance (after setCode calls): %v", err)
	}
	if bal.Cmp(want) != 0 {
		t.Fatalf("balance after setCode calls = %s, want %s (setCode must not clobber balance)", bal, want)
	}
}

// setCode drives hardhat_setCode over RPC the way Hardhat does: the code is
// passed as a "0x…" hex byte string. Passing nil/empty code clears it.
func setCode(t *testing.T, ctx context.Context, rc *rpc.Client, addr common.Address, code []byte) {
	t.Helper()
	var ok bool
	if err := rc.CallContext(ctx, &ok, "hardhat_setCode", addr, hexutil.Bytes(code)); err != nil {
		t.Fatalf("hardhat_setCode(%s, %d bytes): %v", addr.Hex(), len(code), err)
	}
	if !ok {
		t.Fatalf("hardhat_setCode(%s, %d bytes) returned false", addr.Hex(), len(code))
	}
}
