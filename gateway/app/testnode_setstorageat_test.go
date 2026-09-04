/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

// TestTestNode_HardhatSetStorageAt exercises hardhat_setStorageAt end-to-end
// against the self-contained testnode: set + immediate read, overwriting the
// same slot, and clearing a slot to zero.
func TestTestNode_HardhatSetStorageAt(t *testing.T) {
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

	addr := common.HexToAddress("0x00000000000000000000000000000000C0DE0043")
	slot := common.HexToHash("0x1")

	// --- Check 1: set + immediate read (sync guarantee) ---
	value := common.HexToHash("0x2a")
	setStorageAt(t, ctx, rc, addr, slot, value)

	got, err := ec.StorageAt(ctx, addr, slot, nil)
	if err != nil {
		t.Fatalf("eth_getStorageAt (after set): %v", err)
	}
	if common.BytesToHash(got) != value {
		t.Fatalf("storage after set = %s, want %s", hexutil.Encode(got), value.Hex())
	}

	// --- Check 2: overwriting the same slot lands the new value ---
	overwrite := common.HexToHash("0x2b")
	setStorageAt(t, ctx, rc, addr, slot, overwrite)

	got, err = ec.StorageAt(ctx, addr, slot, nil)
	if err != nil {
		t.Fatalf("eth_getStorageAt (after overwrite): %v", err)
	}
	if common.BytesToHash(got) != overwrite {
		t.Fatalf("storage after overwrite = %s, want %s", hexutil.Encode(got), overwrite.Hex())
	}

	// --- Check 3: setting a slot to zero clears it ---
	setStorageAt(t, ctx, rc, addr, slot, common.Hash{})

	got, err = ec.StorageAt(ctx, addr, slot, nil)
	if err != nil {
		t.Fatalf("eth_getStorageAt (after clear): %v", err)
	}
	if common.BytesToHash(got) != (common.Hash{}) {
		t.Fatalf("storage after clear = %s, want zero", hexutil.Encode(got))
	}

	// --- Check 4: quantity-style short hex, which is what Hardhat actually sends ---
	var ok bool
	if err := rc.CallContext(ctx, &ok, "hardhat_setStorageAt", addr, "0x2", "0x2c"); err != nil {
		t.Fatalf("hardhat_setStorageAt with short hex: %v", err)
	}

	got, err = ec.StorageAt(ctx, addr, common.HexToHash("0x2"), nil)
	if err != nil {
		t.Fatalf("eth_getStorageAt (after short hex): %v", err)
	}
	if common.BytesToHash(got) != common.HexToHash("0x2c") {
		t.Fatalf("storage after short hex = %s, want 0x2c", hexutil.Encode(got))
	}
}

// setStorageAt drives hardhat_setStorageAt over RPC the way Hardhat does:
// slot and value are passed as "0x…" quantity-style hex strings.
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
