/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"math/big"
	"testing"
)

func TestBuildChainConfig(t *testing.T) {
	const chainID int64 = 4011

	c := BuildChainConfig(chainID)
	if c == nil {
		t.Fatal("BuildChainConfig returned nil")
	}

	if c.ChainID == nil || c.ChainID.Cmp(big.NewInt(chainID)) != 0 {
		t.Errorf("ChainID = %v, want %d", c.ChainID, chainID)
	}

	// Every fork through Osaka is active from genesis; check a representative subset.
	activeAtZero := map[string]*big.Int{
		"HomesteadBlock":      c.HomesteadBlock,
		"EIP150Block":         c.EIP150Block,
		"EIP155Block":         c.EIP155Block,
		"ByzantiumBlock":      c.ByzantiumBlock,
		"ConstantinopleBlock": c.ConstantinopleBlock,
		"IstanbulBlock":       c.IstanbulBlock,
		"BerlinBlock":         c.BerlinBlock,
		"LondonBlock":         c.LondonBlock,
	}
	for name, v := range activeAtZero {
		if v == nil || v.Sign() != 0 {
			t.Errorf("%s = %v, want 0", name, v)
		}
	}

	// Time-based forks (post-merge) must be non-nil zero pointers.
	timeForks := map[string]*uint64{
		"ShanghaiTime": c.ShanghaiTime,
		"CancunTime":   c.CancunTime,
		"PragueTime":   c.PragueTime,
		"OsakaTime":    c.OsakaTime,
	}
	for name, p := range timeForks {
		if p == nil || *p != 0 {
			t.Errorf("%s = %v, want *uint64(0)", name, p)
		}
	}

	// Explicit "off" settings that should stay unset.
	if c.DAOForkBlock != nil {
		t.Errorf("DAOForkBlock = %v, want nil", c.DAOForkBlock)
	}
	if c.DAOForkSupport {
		t.Error("DAOForkSupport = true, want false")
	}
	if c.MergeNetsplitBlock != nil {
		t.Errorf("MergeNetsplitBlock = %v, want nil", c.MergeNetsplitBlock)
	}
	if c.Clique != nil {
		t.Errorf("Clique = %v, want nil", c.Clique)
	}

	// Post-merge signal + PoW placeholder consensus config.
	if c.TerminalTotalDifficulty == nil || c.TerminalTotalDifficulty.Sign() != 0 {
		t.Errorf("TerminalTotalDifficulty = %v, want 0", c.TerminalTotalDifficulty)
	}
	if c.Ethash == nil {
		t.Error("Ethash config must be non-nil")
	}
	if c.BlobScheduleConfig == nil {
		t.Error("BlobScheduleConfig must be non-nil")
	}
}

func TestBuildChainConfig_ChainIDIsIndependent(t *testing.T) {
	// Calling BuildChainConfig twice with different chain IDs must produce
	// independent ChainID big.Int values (no shared pointer surprises).
	a := BuildChainConfig(1)
	b := BuildChainConfig(4011)
	if a.ChainID == b.ChainID {
		t.Fatal("ChainID pointers must not be shared across calls")
	}
	if a.ChainID.Int64() != 1 || b.ChainID.Int64() != 4011 {
		t.Errorf("ChainID values wrong: a=%d b=%d", a.ChainID.Int64(), b.ChainID.Int64())
	}
}
