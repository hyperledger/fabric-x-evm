/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	blocksfx "github.com/hyperledger/fabric-x-sdk/blocks/fabricx"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
)

// NewFabricXSynchronizer creates a Fabric-X synchronizer using a parser that
// preserves per-transaction status and handles legacy committed status values.
func NewFabricXSynchronizer(
	db network.BlockHeightReader,
	channel string,
	conf network.PeerConf,
	signer sdk.Signer,
	logger sdk.Logger,
	handlers ...blocks.BlockHandler,
) (*network.Synchronizer, error) {
	peer, err := nfabx.NewPeer(conf, channel, signer)
	if err != nil {
		return nil, err
	}

	return newFabricXSynchronizerWithPeer(db, peer, logger, handlers...)
}

func newFabricXSynchronizerWithPeer(
	db network.BlockHeightReader,
	peer network.SyncPeer,
	logger sdk.Logger,
	handlers ...blocks.BlockHandler,
) (*network.Synchronizer, error) {
	processor := blocks.NewProcessor(newFabricXCompatParser(logger), handlers)
	return network.NewSynchronizer(db, peer, processor, logger)
}

type fabricXCompatParser struct {
	base blocksfx.BlockParser
}

func newFabricXCompatParser(log sdk.Logger) blocks.BlockParser {
	return &fabricXCompatParser{
		base: blocksfx.NewBlockParser(log),
	}
}

func (p *fabricXCompatParser) Parse(b *cb.Block) (blocks.Block, error) {
	parsed, err := p.base.Parse(b)
	if err != nil {
		return parsed, err
	}

	if b == nil || b.Metadata == nil {
		return parsed, nil
	}
	if len(b.Metadata.Metadata) <= int(cb.BlockMetadataIndex_TRANSACTIONS_FILTER) {
		return parsed, nil
	}

	txFilter := b.Metadata.Metadata[cb.BlockMetadataIndex_TRANSACTIONS_FILTER]
	for i := range parsed.Transactions {
		txNum := int(parsed.Transactions[i].Number)
		if txNum < 0 || txNum >= len(txFilter) {
			continue
		}
		status := int(txFilter[txNum])
		parsed.Transactions[i].Status = status
		parsed.Transactions[i].Valid = isFabricXCommittedStatus(status)
	}
	return parsed, nil
}

func isFabricXCommittedStatus(status int) bool {
	const legacyCommittedStatusV2 = 2
	// Compatibility across committer status encodings:
	// - legacy: committed was encoded as numeric 0.
	//   In newer protobuf enums, value 0 is named STATUS_UNSPECIFIED.
	//   We intentionally treat 0 as committed for backward compatibility.
	// - current: COMMITTED=1
	// - some deployed test committers: COMMITTED=2
	return status == int(committerpb.Status_STATUS_UNSPECIFIED) ||
		status == int(committerpb.Status_COMMITTED) ||
		status == legacyCommittedStatusV2
}
