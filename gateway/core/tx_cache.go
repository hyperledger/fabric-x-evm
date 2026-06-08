/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// TxNotification contains all data needed to process a transaction notification.
// EthTxBytes and EthTxHash come from the cache; NsRWS and Events come from the AllTxStreamer event.
type TxNotification struct {
	// From notification service
	BlockNum   uint64
	TxNum      uint64
	FabricTxID string
	Status     committerpb.Status

	// From cache
	EthTxBytes []byte
	EthTxHash  common.Hash // pre-computed; handlers that only need the hash skip UnmarshalBinary

	// From AllTxStreamer (IncludeReadWriteSets must be true)
	NsRWS  []blocks.NsReadWriteSet
	Events []byte
}

// PendingTxCache is a thread-safe cache for storing Ethereum transaction bytes
// between endorsement and commit notification.
type PendingTxCache struct {
	mu    sync.RWMutex
	cache map[string][]byte // Fabric TxID -> EthTxBytes
}

// NewPendingTxCache creates a new empty cache.
func NewPendingTxCache() *PendingTxCache {
	return &PendingTxCache{
		cache: make(map[string][]byte),
	}
}

// Add stores Ethereum transaction bytes in the cache.
func (c *PendingTxCache) Add(fabricTxID string, ethTxBytes []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[fabricTxID] = ethTxBytes
}

// Get retrieves Ethereum transaction bytes from the cache.
// Returns nil if the entry is not found.
func (c *PendingTxCache) Get(fabricTxID string) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[fabricTxID]
}

// Delete removes a transaction entry from the cache. Safe to call if not present.
func (c *PendingTxCache) Delete(fabricTxID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, fabricTxID)
}
