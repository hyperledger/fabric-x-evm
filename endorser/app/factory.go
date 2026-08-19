/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"fmt"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/core"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/endorser/storage"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	sdknet "github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// NewEndorserCore builds the endorser engine, its KVS, and its endorsement builder —
// the construction shared by a self-syncing production endorser (see NewEndorser) and a
// sync-less endorser whose state is instead kept current by an external synchronizer
// (e.g. a single gateway-level synchronizer feeding multiple endorsers, as used by the
// in-process test harness and testnode). It does not create a synchronizer or resolve a
// signer from MSP; callers own both.
func NewEndorserCore(
	dbCfg config.DB,
	channel, namespace, protocol string,
	signer sdk.Signer,
	evmConfig execution.EVMConfig,
	testImpl bool,
	// tsCfg supplies timestamp skew bounds (zero value uses package defaults).
	tsCfg config.Endorser,
) (*core.Endorser, storage.KVS, endorsement.Builder, error) {
	// Reject backend/protocol combinations that cannot work before opening any
	// files. gateway config validation catches this earlier for a real
	// deployment; this covers callers that build an endorser directly (testnode,
	// the in-process test harness).
	if err := config.ValidateDatabaseProtocol(dbCfg.Database, protocol); err != nil {
		return nil, nil, nil, err
	}

	// An unset protocol means fabric-x, matching the documented default
	// (common.Network.Protocol) and the gateway wiring (NewNetworkSubmitters,
	// NewGatewaySynchronizer). Normalize once so the KVS and builder choices
	// below cannot disagree about what "" means.
	protocol, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return nil, nil, nil, err
	}

	var kvs storage.KVS
	switch dbCfg.Database {
	case config.DBSQLite:
		writeDB, err := state.NewWriteDB(channel, dbCfg.ConnString)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to initialize store: %w", err)
		}
		kvs = storage.NewVersionedDBWrapper(writeDB)
	case config.DBMemory:
		baseLightKVS := storage.NewLightKVS(dbCfg.HistorySize)
		if testImpl {
			kvs = storage.NewRevertibleLightKVS(baseLightKVS)
		} else {
			kvs = baseLightKVS
		}
	case config.DBPebble:
		pebbleKVS, err := storage.NewPebbleKVS(dbCfg.ConnString, dbCfg.HistorySize)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to initialize store: %w", err)
		}
		kvs = pebbleKVS
	default:
		return nil, nil, nil, fmt.Errorf("invalid endorser database type %q, must be one of %q, %q, %q", dbCfg.Database, config.DBSQLite, config.DBMemory, config.DBPebble)
	}

	var builder endorsement.Builder
	var monotonicVersions bool
	switch protocol {
	case common.ProtocolFabricX:
		builder = efabx.NewEndorsementBuilder(signer)
		monotonicVersions = true
	default: // "fabric"
		builder = efab.NewEndorsementBuilder(signer)
	}

	end, err := core.New(
		execution.NewEVMEngine(namespace, kvs, evmConfig, monotonicVersions),
		builder,
		tsCfg,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create endorser: %w", err)
	}

	return end, kvs, builder, nil
}

// NewEndorser creates a single embedded, self-syncing endorser instance: it resolves an
// MSP-based signer and owns a synchronizer that keeps its KVS current from the committer.
// This is the canonical way to create a production endorser.
// Returns the endorser, synchronizer, and the LightKVS instance (or extended version) for state management.
func NewEndorser(
	cfg config.Endorser,
	network common.Network,
	signer sdk.Signer,
	logger sdk.Logger,
	testImpl bool,
) (*core.Endorser, *sdknet.Synchronizer, storage.KVS, error) {
	evmConfig := execution.EVMConfig{
		ChainConfig: common.BuildChainConfig(network.ChainID),
		MaxTxGas:    network.MaxTxGas,
		DebugLogs:   cfg.DebugLogs,
	}

	end, kvs, _, err := NewEndorserCore(cfg.Database, network.Channel, network.Namespace, network.Protocol, signer, evmConfig, testImpl, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	var sync *sdknet.Synchronizer
	switch network.Protocol {
	case common.ProtocolFabric:
		sync, err = nfab.NewSynchronizer(kvs, network.Channel, cfg.Committer.ToPeerConf(), signer, logger, kvs)
	default: // "fabric-x" or "" (the default)
		sync, err = nfabx.NewSynchronizer(kvs, network.Channel, cfg.Committer.ToPeerConf(), signer, logger, kvs)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create synchronizer: %w", err)
	}

	return end, sync, kvs, nil
}
