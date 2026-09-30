/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

func TestNew_BlockNumberBounds(t *testing.T) {
	f, err := New(nil, nil, config.Endorser{})
	if err != nil {
		t.Fatal(err)
	}
	if f.maxAhead != config.DefaultBlockNumberAhead || f.maxBehind != config.DefaultBlockNumberBehind {
		t.Fatalf("defaults = %d/%d", f.maxAhead, f.maxBehind)
	}
	f, err = New(nil, nil, config.Endorser{MaxBlockNumberAhead: 2, MaxBlockNumberBehind: 3})
	if err != nil {
		t.Fatal(err)
	}
	if f.maxAhead != 2 || f.maxBehind != 3 {
		t.Fatalf("custom = %d/%d", f.maxAhead, f.maxBehind)
	}
}

func TestValidateRequestBlockNumber(t *testing.T) {
	// height 10, so next block is 11; window is [11-3, 11+2].
	for _, tc := range []struct {
		n    uint64
		ok   bool
		name string
	}{
		{0, false, "unset"},
		{7, false, "too far behind"},
		{8, true, "lower bound"},
		{11, true, "next block"},
		{13, true, "upper bound"},
		{14, false, "too far ahead"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequestBlockNumber(tc.n, 10, 2, 3)
			if (err == nil) != tc.ok {
				t.Fatalf("validateRequestBlockNumber(%d) err = %v, want ok=%v", tc.n, err, tc.ok)
			}
		})
	}
	// Behind window larger than height must not underflow.
	if err := validateRequestBlockNumber(1, 0, 5, 20); err != nil {
		t.Fatalf("genesis: %v", err)
	}
}

func TestExecute_BlockNumber(t *testing.T) {
	now := time.Now()
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})
	newEndorser := func(eng *captureEngine) *Endorser {
		return &Endorser{
			Engine:    eng,
			builder:   &stubBuilder{resp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}},
			maxFuture: time.Minute,
			maxPast:   time.Minute,
			maxAhead:  2,
			maxBehind: 2,
			now:       func() time.Time { return now },
		}
	}

	t.Run("passes number to engine", func(t *testing.T) {
		eng := &captureEngine{stubEngine: stubEngine{height: 10}}
		resp, err := newEndorser(eng).Execute(context.Background(), endorsement.Invocation{}, tx, api.BlockEnv{Number: 12, Time: now})
		if err != nil || resp.Response.Status != common.StatusOK {
			t.Fatalf("err=%v resp=%v", err, resp.GetResponse())
		}
		if eng.blockNumber != 12 {
			t.Fatalf("blockNumber = %d, want 12", eng.blockNumber)
		}
	})

	t.Run("rejects outside window", func(t *testing.T) {
		eng := &captureEngine{stubEngine: stubEngine{height: 10}}
		resp, err := newEndorser(eng).Execute(context.Background(), endorsement.Invocation{}, tx, api.BlockEnv{Number: 20, Time: now})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Response.Status != common.StatusTxRejected {
			t.Fatalf("status = %d, want StatusTxRejected", resp.Response.Status)
		}
		if eng.calls != 0 {
			t.Error("engine should not run when block number is rejected")
		}
	})
}
