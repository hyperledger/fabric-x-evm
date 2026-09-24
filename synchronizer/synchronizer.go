/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package synchronizer builds the one synchronizer a process runs, delivering
// committed blocks to every store it owns — chain, an embedded endorser's
// KVS, gateway, or any combination — and waiting for its initial sync.
package synchronizer

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/synchronizer/hybridx"
)

// Synchronizer is the interface required by any process from its one
// synchronizer. Both *network.Synchronizer (delivery) and
// *hybridx.HybridSynchronizer satisfy it.
type Synchronizer interface {
	Start(ctx context.Context) error
	Ready() error
}

// New creates the protocol-appropriate synchronizer that delivers committed
// blocks to handlers, in order — the one synchronizer a process runs, feeding
// every store it owns (chain, an embedded endorser's KVS, gateway), not just
// the gateway. Callers decide the handler list (and therefore the sync
// topology): a standalone endorser registers only its own KVS, while a
// gateway registers [endorser KVS, chain, gateway] on a single synchronizer
// so that endorser state is applied before the gateway marks a transaction
// complete.
//
// namespace is used by the fabric-x hybrid synchronizer to filter the notification stream.
func New(protocol string, db network.BlockHeightReader, channel, namespace string, committer network.PeerConf, signer sdk.Signer, logger sdk.Logger, handlers ...blocks.BlockHandler) (Synchronizer, error) {
	protocol, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return nil, err
	}

	switch protocol {
	case common.ProtocolFabric:
		return nfab.NewSynchronizer(db, channel, committer, signer, logger, handlers...)
	case common.ProtocolFabricX:
		return hybridx.New(db, channel, namespace, committer, signer, logger, handlers...)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", protocol)
	}
}

// NewDelivery creates a delivery only synchronizer (SDK standard path), not hybridx.
// Empty blocks from the committer Delivery service are pushed through the handlers.
// Used by the self contained testnode so hardhat_mine CutBlock empty blocks are seen.
//
// Temporary for testnode: switch back to hybridx once it incorporates fabric-x-common
// 0.2.9 and later (empty block notifications). Until then AllTxBatch drops empty blocks
// after catch up, so eth_blockNumber never advances on CutBlock.
func NewDelivery(protocol string, db network.BlockHeightReader, channel string, committer network.PeerConf, signer sdk.Signer, logger sdk.Logger, handlers ...blocks.BlockHandler) (Synchronizer, error) {
	protocol, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return nil, err
	}
	switch protocol {
	case common.ProtocolFabric:
		return nfab.NewSynchronizer(db, channel, committer, signer, logger, handlers...)
	case common.ProtocolFabricX:
		return nfabx.NewSynchronizer(db, channel, committer, signer, logger, handlers...)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", protocol)
	}
}

// WaitUntilSynced blocks until sync reports Ready or timeout elapses, polling every 100ms.
func WaitUntilSynced(ctx context.Context, sync Synchronizer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if err := sync.Ready(); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for sync")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}
