/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"
)

func TestNewExecutor_WrapsStateDBWhenDebugEnabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_debug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{
		ChainConfig: common.BuildChainConfig(4011),
		DebugLogs:   true,
	}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	ex, err := eng.newExecutor(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	if _, ok := ex.state.(*StateDBLogger); !ok {
		t.Fatalf("expected *StateDBLogger, got %T", ex.state)
	}
}

// TestExecute_MaxTxGas verifies that MaxTxGas caps msg.GasLimit before execution.
// A tx declared with large gas but a tight MaxTxGas must fail; once MaxTxGas is
// raised to cover intrinsic cost the same tx must succeed.
func TestExecute_MaxTxGas(t *testing.T) {
	to := ethcommon.HexToAddress("0xdead")

	newExecutor := func(maxTxGas uint64) *Executor {
		backend, err := state.NewWriteDB(Channel, "file:exec_maxtxgas?mode=memory&cache=shared")
		if err != nil {
			t.Fatal(err)
		}
		kvs := &testVersionedDBSnapshotter{db: backend}
		cfg := EVMConfig{
			ChainConfig: common.BuildChainConfig(4011),
			MaxTxGas:    maxTxGas,
		}
		eng := NewEVMEngine(Namespace, kvs, cfg, false)
		ex, err := eng.newExecutor(nil)
		if err != nil {
			t.Fatal(err)
		}
		return ex
	}

	// msg with declared gas well above MaxTxGas
	msg := func() *gethcore.Message {
		return &gethcore.Message{
			To:       &to,
			GasLimit: 100_000,
			Value:    new(uint256.Int),
		}
	}

	// MaxTxGas below intrinsic gas (21000 for a simple transfer) → must fail
	ex := newExecutor(1_000)
	defer ex.Close()
	if _, err := ex.execute(msg()); err == nil {
		t.Fatal("expected error when MaxTxGas < intrinsic gas, got nil")
	}

	// MaxTxGas at exactly intrinsic gas → simple transfer must succeed
	ex2 := newExecutor(21_000)
	defer ex2.Close()
	if _, err := ex2.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == intrinsic gas, got: %v", err)
	}

	// MaxTxGas = 0 (unlimited) → declared gas used as-is, must succeed
	ex3 := newExecutor(0)
	defer ex3.Close()
	if _, err := ex3.execute(msg()); err != nil {
		t.Fatalf("expected success when MaxTxGas == 0 (unlimited), got: %v", err)
	}
}

func TestNewExecutor_BareStateDBWhenDebugDisabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_nodebug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &testVersionedDBSnapshotter{db: backend}
	cfg := EVMConfig{
		ChainConfig: common.BuildChainConfig(4011),
		DebugLogs:   false,
	}
	eng := NewEVMEngine(Namespace, kvs, cfg, false)

	ex, err := eng.newExecutor(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()

	if _, ok := ex.state.(*StateDB); !ok {
		t.Fatalf("expected *StateDB, got %T", ex.state)
	}
}
