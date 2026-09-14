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
	"github.com/hyperledger/fabric-x-sdk/state"
)

// NewEndorserCore builds the endorser engine, its KVS, and its endorsement builder —
// the construction shared by every endorser this process embeds, gateway or
// standalone (see gateway/app.newApp and endorser/app.New). It does not
// create a synchronizer or resolve a signer from MSP; callers own both, then
// feed the returned KVS into synchronizer.New themselves.
func NewEndorserCore(
	dbCfg config.DB,
	channel, namespace, protocol string,
	signer sdk.Signer,
	evmConfig execution.EVMConfig,
	testImpl bool,
	// tsCfg supplies timestamp skew bounds (zero value uses package defaults).
	tsCfg config.Endorser,
) (*core.Endorser, storage.KVS, endorsement.Builder, error) {
	// An unset protocol means fabric-x, matching the documented default
	// (common.Network.Protocol) and the gateway wiring (NewNetworkSubmitters,
	// synchronizer.New). Normalize once so the KVS and builder choices
	// below cannot disagree about what "" means.
	protocol, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return nil, nil, nil, err
	}

	// Zero means unset, not "no history": an in-memory KVS with a zero-length
	// history window can't even serve the current tip. Test RPC needs a large
	// sequential window (loadFixture/snapshot stretches can commit far more
	// than a small window between reverts); production only needs a couple of
	// snapshots for the synchronizer to redeliver into.
	if dbCfg.HistorySize == 0 {
		if testImpl {
			dbCfg.HistorySize = 16384
		} else {
			dbCfg.HistorySize = 2
		}
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
	case common.ProtocolFabric:
		builder = efab.NewEndorsementBuilder(signer)
	case common.ProtocolFabricX:
		builder = efabx.NewEndorsementBuilder(signer)
		monotonicVersions = true
	default:
		return nil, nil, nil, fmt.Errorf("unsupported protocol: %q", protocol)
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
