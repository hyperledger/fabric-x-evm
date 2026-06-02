/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// TxCacheEntry stores transaction data needed to process notifications.
// This data is cached after endorsement and retrieved when notifications arrive.
type TxCacheEntry struct {
	// EthTxBytes is the RLP-encoded Ethereum transaction
	EthTxBytes []byte

	// NsRWS contains the read-write sets from all namespaces
	NsRWS []blocks.NsReadWriteSet

	// Events contains the serialized chaincode events (logs or revert)
	Events []byte

	// FabricTxID is the Fabric transaction ID
	FabricTxID string
}

// TxNotification contains all data needed to process a transaction notification.
// It combines data from the notification service with cached endorsement data.
type TxNotification struct {
	// From notification service
	BlockNum   uint64
	TxNum      uint64
	FabricTxID string
	Status     committerpb.Status // Validation status code

	// From cache
	EthTxBytes []byte
	NsRWS      []blocks.NsReadWriteSet
	Events     []byte
}

// PendingTxCache is a thread-safe cache for storing transaction data
// between endorsement and notification arrival.
// It maintains two indexes: by Fabric TxID and by Ethereum tx hash.
type PendingTxCache struct {
	mu             sync.RWMutex
	cache          map[string]*TxCacheEntry // Fabric TxID -> Entry
	ethTxHashIndex map[string]string        // Eth tx hash (hex) -> Fabric TxID
}

// NewPendingTxCache creates a new empty cache.
func NewPendingTxCache() *PendingTxCache {
	return &PendingTxCache{
		cache:          make(map[string]*TxCacheEntry),
		ethTxHashIndex: make(map[string]string),
	}
}

// Add stores a transaction entry in the cache.
// If an entry with the same Fabric txid already exists, it will be overwritten.
// Also updates the secondary index by Ethereum tx hash.
func (c *PendingTxCache) Add(entry *TxCacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Compute ethereum tx hash for indexing
	ethTxHash, err := computeEthTxHash(entry.EthTxBytes)
	if err != nil {
		return err
	}

	c.cache[entry.FabricTxID] = entry
	c.ethTxHashIndex[ethTxHash] = entry.FabricTxID
	return nil
}

// Get retrieves a transaction entry from the cache.
// Returns nil if the entry is not found.
func (c *PendingTxCache) Get(fabricTxID string) *TxCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[fabricTxID]
}

// GetByEthTxHash retrieves a transaction entry from the cache by Ethereum tx hash.
// Returns nil if the entry is not found.
func (c *PendingTxCache) GetByEthTxHash(ethTxHash []byte) *TxCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fabricTxID, ok := c.ethTxHashIndex[string(ethTxHash)]
	if !ok {
		return nil
	}
	return c.cache[fabricTxID]
}

// Delete removes a transaction entry from the cache.
// This is idempotent - safe to call even if the entry doesn't exist.
// Also removes from the secondary index.
func (c *PendingTxCache) Delete(fabricTxID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get the entry to find its eth tx hash
	entry := c.cache[fabricTxID]
	if entry != nil {
		// Remove from eth tx hash index
		ethTxHash, err := computeEthTxHash(entry.EthTxBytes)
		if err == nil {
			delete(c.ethTxHashIndex, ethTxHash)
		}
	}

	delete(c.cache, fabricTxID)
}

// Size returns the current number of entries in the cache.
// Useful for monitoring and debugging.
func (c *PendingTxCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// computeEthTxHash computes the Ethereum transaction hash from RLP-encoded bytes.
// Returns the hash as a hex string (with 0x prefix) for use as a map key.
func computeEthTxHash(ethTxBytes []byte) (string, error) {
	var tx types.Transaction
	if err := tx.UnmarshalBinary(ethTxBytes); err != nil {
		return "", err
	}
	return tx.Hash().String(), nil
}
