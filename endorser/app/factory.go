/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"fmt"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	"github.com/hyperledger/fabric-x-sdk/identity"
	sdknet "github.com/hyperledger/fabric-x-sdk/network"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// NewEndorser creates a single endorser instance with its synchronizer.
// This is the canonical way to create an endorser, whether embedded or standalone.
func NewEndorser(
	cfg config.Endorser,
	network config.Network,
	logger sdk.Logger,
	skipAllNonceChecks bool,
) (*endorser.Endorser, *sdknet.Synchronizer, error) {
	// Signer is the identity to connect to the peer for synchronizing, and for signing the endorsement.
	signer, err := identity.SignerFromMSP(cfg.MspDir, cfg.MspID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create signer: %w", err)
	}

	writeDB, err := state.NewWriteDB(network.Channel, cfg.DbConnStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize store: %w", err)
	}
	readDB, err := state.NewReadDB(network.Channel, cfg.DbConnStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	evmConfig := &endorser.EVMConfig{
		ChainConfig: common.BuildChainConfig(network.ChainID),
	}

	// Executing transactions and signing the endorsement.
	engine := endorser.NewEVMEngine(network.Namespace, readDB, evmConfig, false)
	end, err := endorser.New(engine, efab.NewEndorsementBuilder(signer))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create endorser: %w", err)
	}

	// Synchronizer synchronizes the world state with a committing peer.
	// extractor := bfab.NewRwSetExtractor(network.Namespace)
	processor := blocks.NewProcessor(fabric.NewBlockParser(logger), []blocks.BlockHandler{writeDB})
	sync, err := sdknet.NewSynchronizer(readDB, network.Channel, cfg.PeerAddr, cfg.PeerTLS, signer, processor, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create synchronizer: %w", err)
	}

	return end, sync, nil
}
