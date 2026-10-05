/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"testing"
	"time"
)

// TestTestNode_HardhatMine advances eth_blockNumber via hardhat_mine empty cuts.
func TestTestNode_HardhatMine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	rc, ec := startHardhatTestNode(t, ctx)

	before, err := ec.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber before: %v", err)
	}

	if err := rc.CallContext(ctx, nil, "hardhat_mine", "0x3"); err != nil {
		t.Fatalf("hardhat_mine: %v", err)
	}

	after, err := ec.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber after: %v", err)
	}
	if after != before+3 {
		t.Fatalf("block number %d -> %d, want +3", before, after)
	}

	// Default (omitted blocks) mines one.
	if err := rc.CallContext(ctx, nil, "hardhat_mine"); err != nil {
		t.Fatalf("hardhat_mine default: %v", err)
	}
	after2, err := ec.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber after default: %v", err)
	}
	if after2 != after+1 {
		t.Fatalf("block number %d -> %d, want +1", after, after2)
	}
}
