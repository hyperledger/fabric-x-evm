/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"

	"github.com/hyperledger/fabric-x-evm/common"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
)

// NewNetworkSubmitters creates one network submitter per parallel-submission worker for
// the given protocol. count <= 0 defaults to core.DefaultNumWorkers. This is the wiring
// shared between a real backend (connecting to real orderers) and an in-process test
// backend (connecting to a fabrictest orderer).
func NewNetworkSubmitters(ctx context.Context, protocol string, orderers []network.OrdererConf, gwSigner sdk.Signer, count int, logger sdk.Logger) ([]core.Submitter, error) {
	protocol, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return nil, err
	}

	if count <= 0 {
		count = core.DefaultNumWorkers
	}
	submitters := make([]core.Submitter, count)
	for i := 0; i < count; i++ {
		var err error
		switch protocol {
		case common.ProtocolFabric:
			submitters[i], err = nfab.NewSubmitter(ctx, orderers, gwSigner, time.Duration(0), logger)
		case common.ProtocolFabricX:
			submitters[i], err = nfabx.NewSubmitter(ctx, orderers, time.Duration(0), logger)
		default:
			return nil, fmt.Errorf("unsupported protocol: %q", protocol)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create submitter %d: %w", i, err)
		}
	}
	return submitters, nil
}

// BuildGateway wires the endorsement client, batch submitter, and gateway core component
// from pre-built endorsers, a pre-built chain store, and pre-built submitters. This is the
// wiring shared between a real backend and an in-process test backend; callers are
// responsible for creating the chain store (so they can register its cleanup independently
// of the rest of this wiring) and for creating and starting the synchronizer(s) that feed
// committed blocks to chain/gateway/endorsers.
func BuildGateway(ctx context.Context, endorsers []eapi.Service, gwSigner sdk.Signer, netCfg common.Network, chain core.Store, submitters []core.Submitter, submitterCount int, workerCount int, txQueue core.TxQueueInterface, nonceGate core.NonceSequencer, endorsementChanSize int, txPerSec int) (*core.Gateway, error) {
	ec, err := core.NewEndorsementClient(endorsers, gwSigner, netCfg.Channel, netCfg.Namespace, netCfg.NsVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorsement client: %w", err)
	}

	if endorsementChanSize <= 0 {
		endorsementChanSize = 1000
	}
	if txQueue == nil {
		txQueue = core.NewTxQueue()
	}
	endorsementChan := make(chan core.EndorsedTx, endorsementChanSize)
	// txQueue as Completer: a submission failure means the tx will never reach a block, so
	// it must be completed here instead of leaking forever waiting for a commit that won't come.
	batchSubmitter := core.NewBatchSubmitter(submitters, endorsementChan, submitterCount, txPerSec, txQueue)
	batchSubmitter.Start(ctx)

	gw, err := core.New(ec, batchSubmitter, chain, netCfg.ChainID, workerCount, txQueue, nonceGate, endorsementChan)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	return gw, nil
}
