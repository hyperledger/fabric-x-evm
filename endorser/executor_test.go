/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/state"
	_ "modernc.org/sqlite"
)

func TestNewExecutor_WrapsStateDBWhenDebugEnabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_debug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := NewVersionedDBWrapper(backend)
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

func TestNewExecutor_BareStateDBWhenDebugDisabled(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:exec_nodebug?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := NewVersionedDBWrapper(backend)
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
