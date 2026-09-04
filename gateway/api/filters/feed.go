/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

var feedLogger = flogging.MustGetLogger("gateway.api.filters")

// BlockFeed implements blocks.BlockHandler. Handle updates the FilterAPI
// synchronously under the API lock, matching the rest of the synchronizer
// handler chain (KVS, chain, gateway).
type BlockFeed struct {
	api *FilterAPI
}

// NewBlockFeed returns a handler that delivers blocks to api.
func NewBlockFeed(api *FilterAPI) *BlockFeed {
	return &BlockFeed{api: api}
}

// Handle applies the block to every installed filter before returning.
func (f *BlockFeed) Handle(_ context.Context, b blocks.Block) error {
	f.api.onBlock(b)
	return nil
}

// Close stops the filter expiry loop.
func (f *BlockFeed) Close() {
	f.api.Close()
}
