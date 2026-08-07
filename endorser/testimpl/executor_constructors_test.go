/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/triedb"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/endorser/storage"
)

func testEVMConfig() execution.EVMConfig {
	return execution.EVMConfig{ChainConfig: fxcommon.BuildChainConfig(4011)}
}

func emptyEthStateDB(t *testing.T) *state.StateDB {
	t.Helper()
	trieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	sdb := state.NewDatabase(trieDB, nil)
	ethStateDB, err := state.New(types.EmptyRootHash, sdb)
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	return ethStateDB
}

func TestNewBalancePrimingExecutor_UsesLatestSnapshot(t *testing.T) {
	kvs := storage.NewLightKVS(8)
	ex, err := NewBalancePrimingExecutor("testns", kvs, testEVMConfig(), true, nil, nil)
	if err != nil {
		t.Fatalf("NewBalancePrimingExecutor: %v", err)
	}
	if ex == nil || ex.Executor == nil {
		t.Fatal("expected non-nil executor")
	}
	t.Cleanup(func() { ex.Close() })
}

func TestNewBalancePrimingExecutor_WithPrimingEnabled(t *testing.T) {
	kvs := storage.NewLightKVS(8)
	cfg := &BalancePrimingConfig{
		Enabled:         true,
		ContractAddress: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		MappingPosition: 0,
	}
	ex, err := NewBalancePrimingExecutor("testns", kvs, testEVMConfig(), true, cfg, nil)
	if err != nil {
		t.Fatalf("NewBalancePrimingExecutor: %v", err)
	}
	if ex == nil || ex.Executor == nil {
		t.Fatal("expected non-nil executor")
	}
	if _, ok := ex.state.(*BalancePrimingWrapper); !ok {
		t.Fatalf("expected *BalancePrimingWrapper, got %T", ex.state)
	}
	t.Cleanup(func() { ex.Close() })
}

func TestNewExecutorWrapper_UsesLatestSnapshot(t *testing.T) {
	kvs := storage.NewLightKVS(8)
	ex, err := NewExecutorWrapper("testns", kvs, testEVMConfig(), true, emptyEthStateDB(t), nil)
	if err != nil {
		t.Fatalf("NewExecutorWrapper: %v", err)
	}
	if ex == nil || ex.Executor == nil {
		t.Fatal("expected non-nil executor")
	}
	if ex.state == nil {
		t.Fatal("expected DualStateDB")
	}
	t.Cleanup(func() { ex.Close() })
}
