/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/state/queryservice"
)

// QueryServiceKVS reads committed state from the Fabric-X query service. It
// keeps no state of its own, so it only serves the latest state.
type QueryServiceKVS struct {
	*queryservice.Store
}

var _ KVS = QueryServiceKVS{}

// NewQueryServiceKVS returns a KVS reading through client.
func NewQueryServiceKVS(client queryservice.Client) QueryServiceKVS {
	return QueryServiceKVS{queryservice.NewStore(client)}
}

// NewSnapshot opens a view of the latest state. A non-nil blockNumber returns
// state.ErrHistoryUnavailable: the query service has no history.
func (k QueryServiceKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	if blockNumber != nil {
		return k.SnapshotAt(context.Background(), *blockNumber)
	}
	return k.Snapshot(context.Background())
}
