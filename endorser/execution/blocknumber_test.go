/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// returnBlockNumber is init code returning block.number as a 32-byte word:
// NUMBER PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN.
var returnBlockNumber = []byte{0x43, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}

// fixedHeightSnapshotter reports a fixed latest height, as if blocks were committed.
type fixedHeightSnapshotter struct {
	*testVersionedDBSnapshotter
	height uint64
}

func (f *fixedHeightSnapshotter) BlockNumber(context.Context) (uint64, error) {
	return f.height, nil
}

func newBlockNumberEngine(t *testing.T, dsn string, height uint64) *EVMEngine {
	t.Helper()
	backend, err := state.NewWriteDB(Channel, "file:"+dsn+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	kvs := &fixedHeightSnapshotter{testVersionedDBSnapshotter: &testVersionedDBSnapshotter{db: backend}, height: height}
	return NewEVMEngine(Namespace, kvs, EVMConfig{ChainConfig: common.BuildChainConfig(4011)}, false)
}

func TestExecute_UsesGatewayBlockNumber(t *testing.T) {
	eng := newBlockNumberEngine(t, "exec_blocknumber_execute", 3)

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	tx := types.NewContractCreation(0, big.NewInt(0), 100_000, big.NewInt(1), returnBlockNumber)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(4011)), key)
	if err != nil {
		t.Fatal(err)
	}

	res, err := eng.Execute(t.Context(), signed, 42, uint64(1_700_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != common.StatusOK {
		t.Fatalf("Status = %d, want OK", res.Status)
	}
	// The deployed code is the returned word, so it must appear among the writes.
	want := ethcommon.BigToHash(big.NewInt(42)).Bytes()
	for _, w := range res.RWS.Writes {
		if bytes.Equal(w.Value, want) {
			return
		}
	}
	t.Fatalf("no write with block.number 42 among %d writes", len(res.RWS.Writes))
}

func TestCall_BlockNumber(t *testing.T) {
	eng := newBlockNumberEngine(t, "exec_blocknumber_call", 7)

	for name, tc := range map[string]struct {
		arg  *big.Int
		want int64
	}{
		"latest":   {nil, 7},
		"explicit": {big.NewInt(5), 5},
	} {
		t.Run(name, func(t *testing.T) {
			ret, _, err := eng.Call(ethereum.CallMsg{Data: returnBlockNumber}, tc.arg)
			if err != nil {
				t.Fatal(err)
			}
			if got := new(big.Int).SetBytes(ret); got.Int64() != tc.want {
				t.Fatalf("block.number = %s, want %d", got, tc.want)
			}
		})
	}
}
