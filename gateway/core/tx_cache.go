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

// TxCacheEntry stores the Ethereum transaction bytes for a pending Fabric transaction.
// NsRWS and Events are no longer cached here — they are delivered by AllTxStreamer.
type TxCacheEntry struct {
	// EthTxBytes is the RLP-encoded Ethereum transaction
	EthTxBytes []byte

	// FabricTxID is the Fabric transaction ID
	FabricTxID string
}

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
	cache map[string]*TxCacheEntry // Fabric TxID -> Entry
}

// NewPendingTxCache creates a new empty cache.
func NewPendingTxCache() *PendingTxCache {
	return &PendingTxCache{
		cache: make(map[string]*TxCacheEntry),
	}
}

// Add stores a transaction entry in the cache.
func (c *PendingTxCache) Add(entry *TxCacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[entry.FabricTxID] = entry
	return nil
}

// Get retrieves a transaction entry from the cache.
// Returns nil if the entry is not found.
func (c *PendingTxCache) Get(fabricTxID string) *TxCacheEntry {
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

// Size returns the current number of entries in the cache.
func (c *PendingTxCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
