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
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-sdk/identity"
	sdknet "github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// NewEndorser creates a single endorser instance with its synchronizer.
// This is the canonical way to create an endorser, whether embedded or standalone.
func NewEndorser(
	cfg config.Endorser,
	network common.Network,
	logger sdk.Logger,
	skipAllNonceChecks bool,
) (*endorser.Endorser, *sdknet.Synchronizer, error) {
	// Signer is the identity to connect to the peer for synchronizing, and for signing the endorsement.
	signer, err := identity.SignerFromMSP(cfg.Identity.MSPDir, cfg.Identity.MspID)
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
		FreeGas:     true,
	}

	var builder endorsement.Builder
	var sync *sdknet.Synchronizer
	switch network.Protocol {
	case "fabric-x":
		builder = efabx.NewEndorsementBuilder(signer)
		sync, err = nfabx.NewSynchronizer(readDB, network.Channel, cfg.Committer.ToPeerConf(), signer, logger, writeDB)
	default: // "fabric" or ""
		builder = efab.NewEndorsementBuilder(signer)
		sync, err = nfab.NewSynchronizer(readDB, network.Channel, cfg.Committer.ToPeerConf(), signer, logger, writeDB)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create synchronizer: %w", err)
	}

	// Executing transactions and signing the endorsement.
	monotonicVersions := network.Protocol == "fabric-x"
	end, err := endorser.New(
		endorser.NewEVMEngine(network.Namespace, readDB, evmConfig, monotonicVersions),
		builder,
		evmConfig.ChainConfig,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create endorser: %w", err)
	}

	return end, sync, nil
}
