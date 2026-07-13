/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"fmt"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
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
	if count <= 0 {
		count = core.DefaultNumWorkers
	}
	submitters := make([]core.Submitter, count)
	for i := 0; i < count; i++ {
		var err error
		switch protocol {
		case "fabric":
			submitters[i], err = nfab.NewSubmitter(ctx, orderers, gwSigner, 0, logger)
		case "fabric-x", "":
			submitters[i], err = nfabx.NewSubmitter(ctx, orderers, gwSigner, 0, logger)
		default:
			return nil, fmt.Errorf("unsupported protocol: %q", protocol)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create submitter %d: %w", i, err)
		}
	}
	return submitters, nil
}

// NewGatewaySynchronizer creates the protocol-appropriate synchronizer that delivers
// committed blocks to handlers, in order. Callers decide the handler list (and therefore
// the sync topology): a real backend typically registers only [chain, gateway], while an
// in-process test backend with per-endorser-embedded synchronization instead registers
// [endorser DBs..., chain, gateway] on a single synchronizer so that endorser state is
// applied before the gateway marks a transaction complete.
func NewGatewaySynchronizer(protocol string, db network.BlockHeightReader, channel string, committer network.PeerConf, gwSigner sdk.Signer, logger sdk.Logger, handlers ...blocks.BlockHandler) (*network.Synchronizer, error) {
	switch protocol {
	case "fabric":
		return nfab.NewSynchronizer(db, channel, committer, gwSigner, logger, handlers...)
	case "fabric-x", "":
		return nfabx.NewSynchronizer(db, channel, committer, gwSigner, logger, handlers...)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", protocol)
	}
}

// BuildGateway wires the endorsement client, batch submitter, and gateway core component
// from pre-built endorsers, a pre-built chain store, and pre-built submitters. This is the
// wiring shared between a real backend and an in-process test backend; callers are
// responsible for creating the chain store (so they can register its cleanup independently
// of the rest of this wiring) and for creating and starting the synchronizer(s) that feed
// committed blocks to chain/gateway/endorsers.
func BuildGateway(ctx context.Context, endorsers []eapi.Service, gwSigner sdk.Signer, netCfg common.Network, chain core.Store, submitters []core.Submitter, submitterCount int, workerCount int, txQueue core.TxQueueInterface) (*core.Gateway, error) {
	ec, err := core.NewEndorsementClient(endorsers, gwSigner, netCfg.Channel, netCfg.Namespace, netCfg.NsVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorsement client: %w", err)
	}

	endorsementChan := make(chan sdk.Endorsement, 1000)
	batchSubmitter := core.NewBatchSubmitter(submitters, endorsementChan, submitterCount)
	batchSubmitter.Start(ctx)

	gw, err := core.New(ec, batchSubmitter, chain, netCfg.ChainID, workerCount, txQueue, endorsementChan)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	return gw, nil
}
