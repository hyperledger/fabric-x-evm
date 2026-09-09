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
)

// runtimeReturn42 is runtime bytecode that returns the 32-byte word 42:
//
//	60 2a   PUSH1 0x2a   ; 42
//	60 00   PUSH1 0x00   ; offset 0
//	52      MSTORE       ; mem[0:32] = 42
//	60 20   PUSH1 0x20   ; return length 32
//	60 00   PUSH1 0x00   ; return offset 0
//	f3      RETURN       ; return the 32-byte word 42
//
// Calling an account whose code is this must yield the big-endian encoding of 42.
// That only happens if the injected code actually EXECUTES in the EVM, which is a
// stronger check than reading the code back.
var runtimeReturn42 = common.FromHex("0x602a60005260206000f3")

// startHardhatTestNode brings up a self-contained testnode and returns an in-proc RPC
// client for its fully-wired test RPC surface — the same server production builds, so
// the state primer under the hardhat directives is the real one.
func startHardhatTestNode(t *testing.T, ctx context.Context) (*rpc.Client, *ethclient.Client) {
	t.Helper()

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

	waitReady(t, ctx, application.Gateway())

	rc := rpc.DialInProc(application.RPCServer())
	t.Cleanup(rc.Close)
	return rc, ethclient.NewClient(rc)
}

// waitReady blocks until the gateway's block store answers, or fails on timeout.
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

// TestTestNode_HardhatSetBalance covers hardhat_setBalance end to end: raising a
// balance from zero, and lowering one — the case an add-only primer gets wrong.
func TestTestNode_HardhatSetBalance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	rc, ec := startHardhatTestNode(t, ctx)

	addr := common.HexToAddress("0x364d6D0333432C3Ac016Ca832fb8594A8cE43Ca6")

	// Raise from zero.
	want := big.NewInt(1_000_000_000_000_000_000)
	setBalance(t, ctx, rc, addr, want)
	if got, err := ec.BalanceAt(ctx, addr, nil); err != nil {
		t.Fatalf("BalanceAt: %v", err)
	} else if got.Cmp(want) != 0 {
		t.Fatalf("balance = %s, want %s", got, want)
	}

	// Lower it: the delta path. ForceSetBalance must subtract, not add.
	lower := big.NewInt(5_000)
	setBalance(t, ctx, rc, addr, lower)
	if got, err := ec.BalanceAt(ctx, addr, nil); err != nil {
		t.Fatalf("BalanceAt (after lower): %v", err)
	} else if got.Cmp(lower) != 0 {
		t.Fatalf("balance after lowering = %s, want %s", got, lower)
	}

	// Setting the same value again is a no-op that must still succeed.
	setBalance(t, ctx, rc, addr, lower)
}

// TestTestNode_HardhatSetCode covers hardhat_setCode end to end: set + read back,
// that the injected code actually executes, clearing with empty code, and that the
// account's balance is left alone.
func TestTestNode_HardhatSetCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	rc, ec := startHardhatTestNode(t, ctx)

	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Give the account a balance first, so we can prove setCode leaves it untouched.
	balance := big.NewInt(7_777)
	setBalance(t, ctx, rc, addr, balance)

	setCode(t, ctx, rc, addr, runtimeReturn42)
	got, err := ec.CodeAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if !bytes.Equal(got, runtimeReturn42) {
		t.Fatalf("code = %s, want %s", hexutil.Encode(got), hexutil.Encode(runtimeReturn42))
	}

	// The code must be executable, not merely stored.
	out, err := ec.CallContract(ctx, ethereum.CallMsg{To: &addr}, nil)
	if err != nil {
		t.Fatalf("eth_call against injected code: %v", err)
	}
	if new(big.Int).SetBytes(out).Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("injected code returned %s, want 42", hexutil.Encode(out))
	}

	// setCode must not have disturbed the balance.
	if bal, err := ec.BalanceAt(ctx, addr, nil); err != nil {
		t.Fatalf("BalanceAt: %v", err)
	} else if bal.Cmp(balance) != 0 {
		t.Fatalf("balance after setCode = %s, want %s", bal, balance)
	}

	// Empty code clears it.
	setCode(t, ctx, rc, addr, nil)
	if got, err := ec.CodeAt(ctx, addr, nil); err != nil {
		t.Fatalf("CodeAt (after clear): %v", err)
	} else if len(got) != 0 {
		t.Fatalf("code after clear = %s, want empty", hexutil.Encode(got))
	}
}

// TestTestNode_HardhatSetStorageAt covers hardhat_setStorageAt end to end: set,
// overwrite, clear to zero, and the quantity-style short hex form Hardhat sends.
func TestTestNode_HardhatSetStorageAt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	rc, ec := startHardhatTestNode(t, ctx)

	addr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	key := common.HexToHash("0x1")

	for _, tc := range []struct {
		name string
		val  common.Hash
	}{
		{"set", common.HexToHash("0x2a")},
		{"overwrite", common.HexToHash("0x2b")},
		{"clear to zero", common.Hash{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setStorageAt(t, ctx, rc, addr, key, tc.val)
			got, err := ec.StorageAt(ctx, addr, key, nil)
			if err != nil {
				t.Fatalf("StorageAt: %v", err)
			}
			if common.BytesToHash(got) != tc.val {
				t.Fatalf("storage = %s, want %s", hexutil.Encode(got), tc.val.Hex())
			}
		})
	}

	// Hardhat sends quantity-style hex, not padded 32-byte words.
	var ok bool
	if err := rc.CallContext(ctx, &ok, "hardhat_setStorageAt", addr, "0x1", "0x2c"); err != nil {
		t.Fatalf("hardhat_setStorageAt (short hex): %v", err)
	}
	got, err := ec.StorageAt(ctx, addr, key, nil)
	if err != nil {
		t.Fatalf("StorageAt (after short hex): %v", err)
	}
	if common.BytesToHash(got) != common.HexToHash("0x2c") {
		t.Fatalf("storage after short hex = %s, want 0x2c", hexutil.Encode(got))
	}
}

func setBalance(t *testing.T, ctx context.Context, rc *rpc.Client, addr common.Address, amount *big.Int) {
	t.Helper()
	if err := rc.CallContext(ctx, nil, "hardhat_setBalance", addr, hexutil.EncodeBig(amount)); err != nil {
		t.Fatalf("hardhat_setBalance(%s, %s): %v", addr.Hex(), amount, err)
	}
}

func setCode(t *testing.T, ctx context.Context, rc *rpc.Client, addr common.Address, code []byte) {
	t.Helper()
	var ok bool
	if err := rc.CallContext(ctx, &ok, "hardhat_setCode", addr, hexutil.Encode(code)); err != nil {
		t.Fatalf("hardhat_setCode(%s): %v", addr.Hex(), err)
	}
	if !ok {
		t.Fatalf("hardhat_setCode(%s) returned false", addr.Hex())
	}
}

func setStorageAt(t *testing.T, ctx context.Context, rc *rpc.Client, addr common.Address, slot, value common.Hash) {
	t.Helper()
	var ok bool
	if err := rc.CallContext(ctx, &ok, "hardhat_setStorageAt", addr, slot.Hex(), value.Hex()); err != nil {
		t.Fatalf("hardhat_setStorageAt(%s, %s, %s): %v", addr.Hex(), slot.Hex(), value.Hex(), err)
	}
	if !ok {
		t.Fatalf("hardhat_setStorageAt(%s, %s, %s) returned false", addr.Hex(), slot.Hex(), value.Hex())
	}
}
