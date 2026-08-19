/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"
)

func TestNewExecutor_UsesProvidedBlockTime(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_blocktime?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	stateDB, err := NewStateDB(context.Background(), reader, Namespace, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := EVMConfig{ChainConfig: fxcommon.BuildChainConfig(4011)}
	want := uint64(1_700_000_123)
	ex, err := NewExecutor(stateDB, reader, big.NewInt(0), want, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ex.BlockCtx.Time != want {
		t.Fatalf("BlockCtx.Time = %d, want %d", ex.BlockCtx.Time, want)
	}
}

func TestNewExecutor_ZeroBlockTimeIsRejected(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_blocktime0?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	reader, err := kvs.NewSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	stateDB, err := NewStateDB(context.Background(), reader, Namespace, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := EVMConfig{ChainConfig: fxcommon.BuildChainConfig(4011)}
	_, err = NewExecutor(stateDB, reader, nil, 0, cfg)
	if err == nil {
		t.Fatal("expected error for blockTime 0")
	}
}

// Call uses wall-clock Unix seconds for TIMESTAMP.
func TestCall_UsesWallClockBlockTime(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_call_time?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{ChainConfig: fxcommon.BuildChainConfig(4011)}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	to := ethcommon.HexToAddress("0xdead")
	before := uint64(time.Now().Unix())
	_, err = eng.Call(ethereum.CallMsg{To: &to}, nil)
	after := uint64(time.Now().Unix())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	ex, err := eng.newExecutor(nil, before)
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()
	if ex.BlockCtx.Time < before || ex.BlockCtx.Time > after+1 {
		t.Fatalf("BlockCtx.Time = %d, want in [%d,%d]", ex.BlockCtx.Time, before, after+1)
	}
}
