/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
	evmfabx "github.com/hyperledger/fabric-x-evm/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
)

type stubSigner struct{}

func (stubSigner) Sign([]byte) ([]byte, error) { return []byte("sig"), nil }
func (stubSigner) Serialize() ([]byte, error)  { return []byte("identity"), nil }

func newCore(t *testing.T, protocol string) (any, error) {
	t.Helper()
	_, _, builder, err := NewEndorserCore(
		config.DB{Database: config.DBMemory, HistorySize: 1},
		"mychannel", "evm", protocol,
		stubSigner{},
		execution.EVMConfig{ChainConfig: common.BuildChainConfig(4011)},
		false,
		config.Endorser{},
	)
	return builder, err
}

// The protocol picks the endorsement format, and the two have disagreed before:
// a fabric-x synchronizer once ran with a fabric builder. An unset protocol is
// fabric-x, so it must not fall through to the classic default.
func TestNewEndorserCore_ProtocolPicksTheBuilder(t *testing.T) {
	tests := []struct {
		protocol string
		want     any
	}{
		{protocol: common.ProtocolFabricX, want: evmfabx.EndorsementBuilder{}},
		{protocol: "", want: evmfabx.EndorsementBuilder{}},
		{protocol: common.ProtocolFabric, want: efab.EndorsementBuilder{}},
	}

	for _, tt := range tests {
		t.Run("protocol="+tt.protocol, func(t *testing.T) {
			builder, err := newCore(t, tt.protocol)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got, want := fmt.Sprintf("%T", builder), fmt.Sprintf("%T", tt.want); got != want {
				t.Errorf("builder = %s, want %s", got, want)
			}
		})
	}
}

// An unrecognized protocol fails before anything is opened.
func TestNewEndorserCore_RejectsUnknownProtocol(t *testing.T) {
	if _, err := newCore(t, "bogus"); err == nil {
		t.Fatal("expected an error for an unknown protocol")
	}
}
