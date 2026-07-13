/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"fmt"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"

	"github.com/hyperledger/fabric-x-evm/common"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	econfig "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

// localSigner is a no-MSP signer: fabrictest doesn't validate real certificates.
type localSigner struct{}

func (localSigner) Sign(msg []byte) ([]byte, error) { return []byte("signature"), nil }
func (localSigner) Serialize() ([]byte, error)      { return []byte("serialised identity"), nil }

// TestNodeConfig carries the flags exposed by `fxevm testnode`'s self-contained
// (no --config) path.
type TestNodeConfig struct {
	Listen           string
	ChainID          int64
	Protocol         string
	TestAccountsPath string
}

const (
	testNodeChannel   = "mychannel"
	testNodeNamespace = "basic"
	testNodeNsVersion = "1.0"
)

// NewTestNode builds a fully self-contained App: an in-process fabrictest network,
// ephemeral local identities, in-memory storage, and the test RPC surface enabled —
// no config file, no generated crypto, no docker/Fablo backend. It replicates the
// single-synchronizer topology proven by integration/test_helpers.go's local harness:
// one gateway-level synchronizer feeds the endorser's KVS before chain/gateway, so the
// test RPC's synchronous eth_sendRawTransaction always has read-your-writes.
func NewTestNode(ctx context.Context, tcfg TestNodeConfig) (*App, error) {
	logger := sdk.NewStdLogger("testnode")
	signer := localSigner{}

	protocol := tcfg.Protocol
	if protocol == "" {
		protocol = "fabric-x"
	}

	evmConfig := execution.EVMConfig{
		ChainConfig: common.BuildChainConfig(tcfg.ChainID),
	}

	// HistorySize=128 gives evm_snapshot/evm_revert enough history to rewind through.
	endorserDB := econfig.DB{Database: "memory", HistorySize: 128}
	endorser, endorserKVS, _, err := eapp.NewEndorserCore(endorserDB, testNodeChannel, testNodeNamespace, protocol, signer, evmConfig, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorser: %w", err)
	}

	// db1 makes fabrictest's MVCC validation read the same DB the endorser reads.
	nw, err := fabrictest.Start(ctx, testNodeNamespace, protocol, fabrictest.Config{}, endorserKVS)
	if err != nil {
		return nil, fmt.Errorf("failed to start in-process network: %w", err)
	}

	orderer := &common.Endpoint{Host: "127.0.0.1", Port: nw.OrdererPort}
	peer := &common.Endpoint{Host: "127.0.0.1", Port: nw.PeerPort}

	cfg := config.Config{
		Network: common.Network{
			Protocol:  protocol,
			Channel:   testNodeChannel,
			Namespace: testNodeNamespace,
			NsVersion: testNodeNsVersion,
			ChainID:   tcfg.ChainID,
		},
		Gateway: config.Gateway{
			Listen:    tcfg.Listen,
			Database:  config.DB{ConnString: ":memory:"},
			Orderers:  []common.ClientConfig{{Endpoint: orderer}},
			Committer: common.ClientConfig{Endpoint: peer},
		},
	}

	return buildApp(ctx, cfg, signer, logger, []eapi.Service{endorser}, nil, endorserKVS, true, tcfg.TestAccountsPath, endorserKVS)
}
