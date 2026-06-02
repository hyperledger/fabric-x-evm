/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

var memStoreLogger = flogging.MustGetLogger("gateway.core.memory_store")

const (
	// DefaultMemoryStoreSize is the default number of recent transactions to keep in memory
	DefaultMemoryStoreSize = 10000
)

// MemoryStore is a lightweight Store implementation that only supports
// GetTransactionByHash and GetLogsByTxHash by storing recent completed transactions in memory.
// It uses a map for fast lookups and a circular buffer to track eviction order.
// All other methods panic as they should not be called in the notification-based system.
// It implements TxHandler to receive notifications about completed transactions.
type MemoryStore struct {
	cache *PendingTxCache // Used to get data during HandleTx before cleanup

	// Transaction storage
	txs     map[string]*domain.Transaction // Ethereum tx hash (hex) -> Transaction
	txOrder []string                       // Circular buffer of tx hashes for eviction
	nextIdx int                            // Next index to write in circular buffer
	size    int                            // Size of circular buffer
	mu      sync.RWMutex
}

// NewMemoryStore creates a new MemoryStore with default size.
func NewMemoryStore(cache *PendingTxCache) *MemoryStore {
	return NewMemoryStoreWithSize(cache, DefaultMemoryStoreSize)
}

// NewMemoryStoreWithSize creates a new MemoryStore with specified buffer size.
func NewMemoryStoreWithSize(cache *PendingTxCache, size int) *MemoryStore {
	return &MemoryStore{
		cache:   cache,
		txs:     make(map[string]*domain.Transaction),
		txOrder: make([]string, size),
		size:    size,
	}
}

// HandleTx implements TxHandler. It stores completed transactions in the internal map
// and manages the circular buffer for eviction.
// Panics on any error as this indicates a bug in the system.
func (s *MemoryStore) HandleTx(ctx context.Context, notifs []TxNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, notif := range notifs {
		// Get the cached data before it's cleaned up
		entry := s.cache.Get(notif.FabricTxID)
		if entry == nil {
			panic(fmt.Sprintf("cache miss for txid %s in MemoryStore.HandleTx - this indicates a bug", notif.FabricTxID))
		}

		// Unmarshal ethereum transaction
		var ethTx types.Transaction
		if err := ethTx.UnmarshalBinary(entry.EthTxBytes); err != nil {
			panic(fmt.Sprintf("failed to unmarshal eth tx for %s: %v", notif.FabricTxID, err))
		}

		// Extract sender address
		signer := types.LatestSignerForChainID(ethTx.ChainId())
		from, err := types.Sender(signer, &ethTx)
		if err != nil {
			panic(fmt.Sprintf("failed to extract sender for %s: %v", notif.FabricTxID, err))
		}

		// Build domain.Transaction
		tx := &domain.Transaction{
			TxHash:         ethTx.Hash().Bytes(),
			BlockHash:      make([]byte, 32), // Fake block hash
			BlockNumber:    notif.BlockNum,
			TxIndex:        int64(notif.TxNum),
			RawTx:          entry.EthTxBytes,
			FromAddress:    from.Bytes(),
			FabricTxID:     notif.FabricTxID,
			FabricTxStatus: int(notif.Status),
		}

		if notif.Status == committerpb.Status_COMMITTED {
			tx.Status = 1
		}

		// Set To address
		if ethTx.To() != nil {
			tx.ToAddress = ethTx.To().Bytes()
		}

		// Extract contract address for contract creation
		if ethTx.To() == nil {
			contractAddr := crypto.CreateAddress(from, ethTx.Nonce())
			tx.ContractAddress = contractAddr.Bytes()
		}

		// Extract logs from events
		logs, err := extractLogsFromEvents(entry.Events, ethTx.Hash(), notif.BlockNum, int64(notif.TxNum))
		if err != nil {
			panic(fmt.Sprintf("failed to extract logs for tx %s: %v", ethTx.Hash().String(), err))
		}
		tx.Logs = logs

		// Store in map
		txHashHex := ethTx.Hash().Hex()

		// Evict old transaction if buffer is full
		if s.txOrder[s.nextIdx] != "" {
			oldTxHash := s.txOrder[s.nextIdx]
			delete(s.txs, oldTxHash)
			memStoreLogger.Debugf("Evicted old transaction: %s", oldTxHash)
		}

		// Add new transaction
		s.txs[txHashHex] = tx
		s.txOrder[s.nextIdx] = txHashHex
		s.nextIdx = (s.nextIdx + 1) % s.size

		memStoreLogger.Debugf("Stored transaction: txHash=%s, blockNum=%d, txNum=%d",
			txHashHex, notif.BlockNum, notif.TxNum)
	}

	return nil
}

// GetTransactionByHash retrieves a transaction from internal storage by its Ethereum hash.
// Returns nil if the transaction is not found.
func (s *MemoryStore) GetTransactionByHash(ctx context.Context, txHash []byte) (*domain.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txHashHex := common.BytesToHash(txHash).Hex()
	memStoreLogger.Debugf("GetTransactionByHash called for txHash=%s", txHashHex)

	tx := s.txs[txHashHex]
	if tx == nil {
		memStoreLogger.Debugf("Transaction not found for txHash=%s", txHashHex)
		return nil, nil
	}

	memStoreLogger.Debugf("Returning transaction: txHash=%s, blockNum=%d, logs=%d",
		txHashHex, tx.BlockNumber, len(tx.Logs))
	return tx, nil
}

// GetLogsByTxHash retrieves logs for a transaction from internal storage by its Ethereum hash.
// Returns empty slice if the transaction is not found.
func (s *MemoryStore) GetLogsByTxHash(ctx context.Context, txHash []byte) ([]domain.Log, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txHashHex := common.BytesToHash(txHash).Hex()
	memStoreLogger.Debugf("GetLogsByTxHash called for txHash=%s", txHashHex)

	tx := s.txs[txHashHex]
	if tx == nil {
		memStoreLogger.Debugf("Transaction not found for txHash=%s", txHashHex)
		return []domain.Log{}, nil
	}

	memStoreLogger.Debugf("Returning %d logs for txHash=%s", len(tx.Logs), txHashHex)
	return tx.Logs, nil
}

// BlockNumber panics - not supported in notification-based system.
func (s *MemoryStore) BlockNumber(ctx context.Context) (uint64, error) {
	panic("MemoryStore.BlockNumber not implemented - use state DB for block number queries")
}

// BlockNumberByHash panics - not supported in notification-based system.
func (s *MemoryStore) BlockNumberByHash(ctx context.Context, hash []byte) (*uint64, error) {
	panic("MemoryStore.BlockNumberByHash not implemented")
}

// LatestBlock panics - not supported in notification-based system.
func (s *MemoryStore) LatestBlock(ctx context.Context, full bool) (*domain.Block, error) {
	panic("MemoryStore.LatestBlock not implemented")
}

// GetBlockByNumber panics - not supported in notification-based system.
func (s *MemoryStore) GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error) {
	panic("MemoryStore.GetBlockByNumber not implemented")
}

// GetBlockByHash panics - not supported in notification-based system.
func (s *MemoryStore) GetBlockByHash(ctx context.Context, hash []byte, full bool) (*domain.Block, error) {
	panic("MemoryStore.GetBlockByHash not implemented")
}

// GetBlockTxCountByHash panics - not supported in notification-based system.
func (s *MemoryStore) GetBlockTxCountByHash(ctx context.Context, hash []byte) (int64, error) {
	panic("MemoryStore.GetBlockTxCountByHash not implemented")
}

// GetBlockTxCountByNumber panics - not supported in notification-based system.
func (s *MemoryStore) GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error) {
	panic("MemoryStore.GetBlockTxCountByNumber not implemented")
}

// GetTransactionByBlockHashAndIndex panics - not supported in notification-based system.
func (s *MemoryStore) GetTransactionByBlockHashAndIndex(ctx context.Context, hash []byte, idx int64) (*domain.Transaction, error) {
	panic("MemoryStore.GetTransactionByBlockHashAndIndex not implemented")
}

// GetTransactionByBlockNumberAndIndex panics - not supported in notification-based system.
func (s *MemoryStore) GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error) {
	panic("MemoryStore.GetTransactionByBlockNumberAndIndex not implemented")
}

// GetLogs panics - not supported in notification-based system.
func (s *MemoryStore) GetLogs(ctx context.Context, filter domain.LogFilter) ([]domain.Log, error) {
	panic("MemoryStore.GetLogs not implemented")
}

// extractLogsFromEvents parses the events bytes and extracts logs.
// The events bytes contain serialized log data from the chaincode.
func extractLogsFromEvents(eventsBytes []byte, ethTxHash common.Hash, blockNum uint64, txIndex int64) ([]domain.Log, error) {
	return []domain.Log{}, nil

	// if len(eventsBytes) == 0 {
	// 	return []domain.Log{}, nil
	// }

	// // Parse the events bytes to extract logs
	// // The format depends on how the endorser serializes events
	// // For now, we'll use the types.Receipt unmarshaling

	// var receipt types.Receipt
	// if err := receipt.UnmarshalBinary(eventsBytes); err != nil {
	// 	return nil, fmt.Errorf("unmarshal receipt from events: %w", err)
	// }

	// // Convert geth logs to domain logs
	// logs := make([]domain.Log, len(receipt.Logs))
	// for i, gethLog := range receipt.Logs {
	// 	topics := make([][]byte, len(gethLog.Topics))
	// 	for j, topic := range gethLog.Topics {
	// 		topics[j] = topic.Bytes()
	// 	}

	// 	logs[i] = domain.Log{
	// 		BlockNumber: blockNum,
	// 		BlockHash:   nil, // We don't have block hash in cache
	// 		TxHash:      ethTxHash.Bytes(),
	// 		TxIndex:     txIndex,
	// 		LogIndex:    int64(gethLog.Index),
	// 		Address:     gethLog.Address.Bytes(),
	// 		Data:        gethLog.Data,
	// 		Topics:      topics,
	// 	}
	// }

	// return logs, nil
}
