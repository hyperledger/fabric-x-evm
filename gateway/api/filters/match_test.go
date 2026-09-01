/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

func mustEthTxBytes(t *testing.T) []byte {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	signer := types.LatestSignerForChainID(big.NewInt(1337))
	tx, err := types.SignNewTx(key, signer, &types.LegacyTx{
		Nonce:    0,
		To:       &to,
		Gas:      21000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustLogEvents(t *testing.T, logs []state.Log) []byte {
	t.Helper()
	payload, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := fc.MarshalLogs(payload, "evmcc", "tx-1")
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func bytes32(b byte) []byte {
	h := make([]byte, 32)
	h[31] = b
	return h
}

func TestLogsFromBlock_HappyAndSkips(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	topic := common.HexToHash("0xdead")
	rawTx := mustEthTxBytes(t)
	events := mustLogEvents(t, []state.Log{{
		Address: addr.Bytes(),
		Topics:  [][]byte{topic.Bytes()},
		Data:    []byte{0x01},
	}})

	b := blocks.Block{
		Number: 5,
		Hash:   bytes32(0x55),
		Transactions: []blocks.Transaction{
			{
				Number:    0,
				Valid:     true,
				InputArgs: [][]byte{[]byte("nope")},
			},
			{
				Number:    1,
				Valid:     false,
				InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx},
				Events:    events,
			},
			{
				Number:    2,
				Valid:     true,
				InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx},
			},
			{
				Number:    3,
				Valid:     true,
				InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, []byte("not-rlp")},
				Events:    events,
			},
			{
				Number:    4,
				Valid:     true,
				InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx},
				Events:    events,
			},
		},
	}

	got := logsFromBlock(b)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].Address != addr || got[0].BlockNumber != 5 || got[0].Index != 0 {
		t.Fatalf("got %+v", got[0])
	}
	if got[0].Topics[0] != topic {
		t.Fatalf("topic = %s", got[0].Topics[0])
	}
	if got[0].BlockHash != common.BytesToHash(bytes32(0x55)) {
		t.Fatalf("block hash = %s", got[0].BlockHash)
	}
}

func TestLogsFromBlock_RevertSkipped(t *testing.T) {
	rawTx := mustEthTxBytes(t)
	inner, err := fc.MarshalRevert([]byte("boom"), "evmcc", "tx-rev")
	if err != nil {
		t.Fatal(err)
	}
	// Endorser wraps the revert marker in an outer "log" ChaincodeEvent.
	outer, err := fc.MarshalLogs(inner, "evmcc", "tx-rev")
	if err != nil {
		t.Fatal(err)
	}
	b := blocks.Block{
		Number: 1,
		Hash:   bytes32(1),
		Transactions: []blocks.Transaction{{
			Number:    0,
			Valid:     true,
			InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx},
			Events:    outer,
		}},
	}
	if got := logsFromBlock(b); len(got) != 0 {
		t.Fatalf("revert should yield no logs, got %d", len(got))
	}
}

func TestMatchLogs_AddressTopicsAndRange(t *testing.T) {
	a := common.HexToAddress("0xaa")
	bAddr := common.HexToAddress("0xbb")
	t0 := common.HexToHash("0x01")
	t1 := common.HexToHash("0x02")
	logs := []*types.Log{
		{Address: a, BlockNumber: 10, Topics: []common.Hash{t0, t1}},
		{Address: bAddr, BlockNumber: 10, Topics: []common.Hash{t0}},
		{Address: a, BlockNumber: 20, Topics: []common.Hash{t0, t1}},
	}

	matched := matchLogs(logs, gethfilters.FilterCriteria{
		FromBlock: big.NewInt(10),
		ToBlock:   big.NewInt(10),
		Addresses: []common.Address{a},
		Topics:    [][]common.Hash{{t0}, {t1}},
	})
	if len(matched) != 1 || matched[0].BlockNumber != 10 {
		t.Fatalf("got %#v", matched)
	}

	matched = matchLogs(logs, gethfilters.FilterCriteria{
		Addresses: []common.Address{bAddr},
		Topics:    [][]common.Hash{{}, {t1}},
	})
	if len(matched) != 0 {
		t.Fatalf("topic length miss: %#v", matched)
	}

	matched = matchLogs(logs[:1], gethfilters.FilterCriteria{
		Topics: [][]common.Hash{{}},
	})
	if len(matched) != 1 {
		t.Fatalf("wildcard: %#v", matched)
	}
}

func TestLogFilter_LivePathViaFeed(t *testing.T) {
	feed := NewBlockFeed()
	defer feed.Close()
	api := NewFilterAPI(feed, &stubLogs{head: 1})
	defer api.Close()

	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	id, err := api.NewFilter(context.Background(), gethfilters.FilterCriteria{
		Addresses: []common.Address{addr},
	})
	if err != nil {
		t.Fatal(err)
	}

	rawTx := mustEthTxBytes(t)
	events := mustLogEvents(t, []state.Log{{
		Address: addr.Bytes(),
		Topics:  [][]byte{common.HexToHash("0x01").Bytes()},
		Data:    []byte{0xab},
	}})
	miss := mustLogEvents(t, []state.Log{{
		Address: common.HexToAddress("0xbb").Bytes(),
		Data:    []byte{0x00},
	}})

	_ = feed.Handle(context.Background(), blocks.Block{
		Number: 3,
		Hash:   bytes32(3),
		Transactions: []blocks.Transaction{
			{Number: 0, Valid: true, InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx}, Events: miss},
			{Number: 1, Valid: true, InputArgs: [][]byte{{byte(fc.ProposalTypeEVMTx)}, rawTx}, Events: events},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		n := 0
		if f := api.filters[id]; f != nil {
			n = len(f.logs)
		}
		api.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	got, err := api.GetFilterChanges(id)
	if err != nil {
		t.Fatal(err)
	}
	logs := got.([]*types.Log)
	if len(logs) != 1 || logs[0].Address != addr {
		t.Fatalf("got %#v", got)
	}
}
