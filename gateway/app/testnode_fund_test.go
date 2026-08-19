/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

// TestNewTestNode_PrefundsHardhatAccounts verifies issue #254: known Hardhat
// EOAs receive the default genesis balance on testnode startup so value
// transfers are not rejected with insufficient funds.
func TestNewTestNode_PrefundsHardhatAccounts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	application, err := NewTestNode(ctx, TestNodeConfig{
		Listen:  "127.0.0.1:0",
		ChainID: 31337,
	})
	if err != nil {
		t.Fatalf("NewTestNode: %v", err)
	}

	mgr, err := testimpl.DefaultTestAccounts()
	if err != nil {
		t.Fatalf("DefaultTestAccounts: %v", err)
	}

	gw := application.Gateway()
	for _, addr := range mgr.Addresses {
		bal, err := gw.BalanceAt(ctx, addr, nil)
		if err != nil {
			t.Fatalf("BalanceAt(%s): %v", addr.Hex(), err)
		}
		if bal.Cmp(testimpl.DefaultTestAccountBalance) != 0 {
			t.Errorf("balance of %s = %s, want %s", addr.Hex(), bal, testimpl.DefaultTestAccountBalance)
		}
	}
}
