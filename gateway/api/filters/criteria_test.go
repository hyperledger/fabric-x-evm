/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
)

func TestCriteriaToLogFilter_BlockHash(t *testing.T) {
	h := common.HexToHash("0xabc")
	got := CriteriaToLogFilter(gethfilters.FilterCriteria{BlockHash: &h}, 9)
	if got.BlockHash == nil || common.BytesToHash(*got.BlockHash) != h {
		t.Fatalf("BlockHash = %v", got.BlockHash)
	}
	if got.FromBlock != nil || got.ToBlock != nil {
		t.Fatalf("range bounds should be unset with BlockHash: %+v", got)
	}
}

func TestCriteriaToLogFilter_BoundsAndFilters(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		head     uint64
		wantFrom *uint64
		wantTo   *uint64
	}{
		{"omitted", `{}`, 7, u64(7), nil},
		{"explicit", `{"fromBlock":"0x2","toBlock":"0x5"}`, 7, u64(2), u64(5)},
		{"earliest from", `{"fromBlock":"earliest","toBlock":"0x0"}`, 7, u64(0), u64(0)},
		{"latest to", `{"fromBlock":"0x1","toBlock":"latest"}`, 7, u64(1), nil},
		{"pending to", `{"toBlock":"pending"}`, 7, u64(7), nil},
		{"latest from", `{"fromBlock":"latest"}`, 7, u64(7), nil},
		{"earliest to only", `{"toBlock":"earliest"}`, 7, u64(7), u64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var crit gethfilters.FilterCriteria
			if err := json.Unmarshal([]byte(tt.json), &crit); err != nil {
				t.Fatal(err)
			}
			got := CriteriaToLogFilter(crit, tt.head)
			if !u64eq(got.FromBlock, tt.wantFrom) {
				t.Errorf("FromBlock = %v want %v", got.FromBlock, tt.wantFrom)
			}
			if !u64eq(got.ToBlock, tt.wantTo) {
				t.Errorf("ToBlock = %v want %v", got.ToBlock, tt.wantTo)
			}
		})
	}
}

func TestCriteriaToLogFilter_AddressesAndTopics(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	topic := common.HexToHash("0x1111")
	crit := gethfilters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}, {}},
	}
	got := CriteriaToLogFilter(crit, 1)
	if len(got.Addresses) != 1 || common.BytesToAddress(got.Addresses[0]) != addr {
		t.Fatalf("addresses = %#v", got.Addresses)
	}
	if len(got.Topics) != 2 {
		t.Fatalf("topics len = %d", len(got.Topics))
	}
	if len(got.Topics[0]) != 1 || common.BytesToHash(got.Topics[0][0]) != topic {
		t.Fatalf("topic0 = %#v", got.Topics[0])
	}
	if len(got.Topics[1]) != 0 {
		t.Fatalf("wildcard topic should stay empty, got %#v", got.Topics[1])
	}
}

func TestResolveFromBlock_EarliestSentinel(t *testing.T) {
	earliest := big.NewInt(int64(rpc.EarliestBlockNumber))
	got := resolveFromBlock(earliest, 99)
	if got == nil || *got != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestResolveToBlock_NilAndNegative(t *testing.T) {
	if resolveToBlock(nil) != nil {
		t.Fatal("nil should stay open")
	}
	latest := big.NewInt(int64(rpc.LatestBlockNumber))
	if resolveToBlock(latest) != nil {
		t.Fatal("latest sentinel should stay open")
	}
}

func u64(v uint64) *uint64 { return &v }

func u64eq(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
