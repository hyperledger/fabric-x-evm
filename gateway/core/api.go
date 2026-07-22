/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

type Signer interface {
	Sign(msg []byte) ([]byte, error)
	Serialize() ([]byte, error)
}

type Submitter interface {
	Submit(context.Context, sdk.Endorsement) error
	Close() error
}

// TxQueueInterface defines the interface that transaction queue implementations must satisfy.
// This allows switching between different queue implementations (e.g., TxQueue and TxQueueV2).
type TxQueueInterface interface {
	// Enqueue adds a transaction to the queue
	Enqueue(tx *types.Transaction)

	// Dequeue removes and returns a transaction from the queue
	// Returns (transaction, true) if successful, or (nil, false) if queue is closed
	Dequeue() (*types.Transaction, bool)

	// IsPending checks if a transaction is currently in the queue or being processed
	IsPending(txHash common.Hash) *types.Transaction

	// Complete removes a transaction from tracking. Safe to call for a hash
	// that is not currently tracked.
	Complete(txHash common.Hash)

	// Close signals shutdown of the queue
	Close()

	// Handle processes block notifications from the synchronizer
	Handle(ctx context.Context, block *domain.Block) error

	// Stats returns statistics about processed transactions (total, invalid)
	Stats() (total int, invalid int, totalEnq int, conflictEnq int)
}

var logger = flogging.MustGetLogger("gateway.core")

// Gateway is the component that bridges Fabric-x and the EVM. Its API is the
// Ethereum JSON RPC. When the user submits a transaction targeting an Ethereum
// contract, the gateway requests endorsement from a set of EVM endorsers. It then
// submits a signed transaction with the read/writeset to the Fabric orderers.
type Gateway struct {
	batchSubmitter  *BatchSubmitter
	endorsers       *EndorsementClient
	store           Store
	chainID         *big.Int
	ChainConfig     *params.ChainConfig
	Signer          types.Signer
	TxQueue         TxQueueInterface
	workerCount     int
	wg              sync.WaitGroup
	stopOnce        sync.Once
	endorsementChan chan sdk.Endorsement // Channel to send endorsements to BatchSubmitter
}

type Store interface {
	BlockNumber(ctx context.Context) (uint64, error)
	BlockNumberByHash(ctx context.Context, hash []byte) (*uint64, error)
	LatestBlock(ctx context.Context, full bool) (*domain.Block, error)
	GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error)
	GetBlockByHash(ctx context.Context, hash []byte, full bool) (*domain.Block, error)
	GetBlockTxCountByHash(ctx context.Context, hash []byte) (int64, error)
	GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error)
	GetTransactionByHash(ctx context.Context, hash []byte) (*domain.Transaction, error)
	GetTransactionByBlockHashAndIndex(ctx context.Context, hash []byte, idx int64) (*domain.Transaction, error)
	GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error)
	GetLogs(ctx context.Context, filter domain.LogFilter) ([]domain.Log, error)
	GetLogsByTxHash(ctx context.Context, txHash []byte) ([]domain.Log, error)
}

// New creates a new Ethereum Gateway.
// If txQueue is nil, NewTxQueue() will be used as the default.
// batchSubmitter handles all endorsement submissions and is owned by the Gateway.
// endorsementChan is the channel to send endorsements to the BatchSubmitter.
func New(ec *EndorsementClient, batchSubmitter *BatchSubmitter, store Store, chainID int64, workerCount int, txQueue TxQueueInterface, endorsementChan chan sdk.Endorsement) (*Gateway, error) {
	if workerCount <= 0 {
		workerCount = 1
	}

	// Use default TxQueue if none provided
	if txQueue == nil {
		txQueue = NewTxQueue()
	}

	cid := big.NewInt(chainID)
	return &Gateway{
		endorsers:       ec,
		batchSubmitter:  batchSubmitter,
		store:           store,
		chainID:         cid,
		ChainConfig:     cmn.BuildChainConfig(chainID),
		Signer:          types.LatestSignerForChainID(cid),
		TxQueue:         txQueue,
		workerCount:     workerCount,
		endorsementChan: endorsementChan,
	}, nil
}

// Start initializes the worker pool to process transactions from the queue
func (g *Gateway) Start(ctx context.Context) {
	for range g.workerCount {
		g.wg.Add(1)
		go g.worker(ctx)
	}
}

// worker processes transactions from the queue
func (g *Gateway) worker(ctx context.Context) {
	defer g.wg.Done()

	for {
		tx, ok := g.TxQueue.Dequeue()
		if !ok {
			// Queue is closed and empty
			return
		}

		// Process the transaction (old SendTransaction logic)
		if err := g.processTx(ctx, tx); err != nil {
			logger.Errorf("tx %s failed: %v", tx.Hash().Hex(), err)
			g.TxQueue.Complete(tx.Hash())
			continue
		}
	}
}

// processTx handles the actual transaction processing
func (g *Gateway) processTx(ctx context.Context, tx *types.Transaction) error {
	end, err := g.ExecuteEthTx(ctx, tx)
	if err != nil {
		return err
	}
	if err := g.SubmitFabricTx(ctx, end); err != nil {
		return err
	}

	return nil
}

// SendTransaction runs geth-style pre-flight validation, then enqueues the tx
// for async endorse/submit. Mirrors eth_sendRawTransaction's failure model.
func (g *Gateway) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if err := ValidateTx(ctx, tx, g.ChainConfig, g.Signer, g); err != nil {
		return err
	}
	if g.TxQueue.IsPending(tx.Hash()) != nil {
		return domain.ErrTransactionAlreadyPending
	}
	g.TxQueue.Enqueue(tx)
	return nil
}

// CallContract is a query. It doesn't require a signature of the end user and doesn't change the ledger or nonce.
// We requests endorsement from a single endorser, return the payload, and discard the signed response.
// This is the same way queries are handled in Fabric.
func (g *Gateway) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return g.endorsers.CallContract(ctx, call, blockNumber)
}

// ExecuteEthTx requests endorsements for the submitted ethereum-style transaction.
func (g *Gateway) ExecuteEthTx(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error) {
	return g.endorsers.ExecuteTransaction(ctx, tx)
}

// SubmitFabricTx submits a Fabric envelope via the BatchSubmitter.
func (g *Gateway) SubmitFabricTx(ctx context.Context, end sdk.Endorsement) error {
	// Send endorsement to BatchSubmitter via channel
	select {
	case g.endorsementChan <- end:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context canceled while sending endorsement: %w", ctx.Err())
	}
}

// ChainID returns the configured chainID for this deployment.
func (g *Gateway) ChainID(ctx context.Context) (*big.Int, error) {
	return g.chainID, nil
}

// BlockNumber is the current blockheight as observed by this gateway.
func (g *Gateway) BlockNumber(ctx context.Context) (uint64, error) {
	return g.store.BlockNumber(ctx)
}

// BlockNumberByHash resolves a block hash to a block number.
func (g *Gateway) BlockNumberByHash(ctx context.Context, hash common.Hash) (*uint64, error) {
	return g.store.BlockNumberByHash(ctx, hash.Bytes())
}

// GetBlockByNumber returns the block at the specified number.
// If full is true, the block includes transactions.
// num == math.MaxUint64 means "latest".
func (g *Gateway) GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error) {
	if num == math.MaxUint64 {
		return g.store.LatestBlock(ctx, full)
	}
	return g.store.GetBlockByNumber(ctx, num, full)
}

// GetBlockByHash returns block metadata based on the block hash.
// If full is true, the block includes transactions.
func (g *Gateway) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*domain.Block, error) {
	return g.store.GetBlockByHash(ctx, hash.Bytes(), full)
}

// GetBlockTxCountByHash counts the transactions in a specific block.
func (g *Gateway) GetBlockTxCountByHash(ctx context.Context, hash common.Hash) (int64, error) {
	return g.store.GetBlockTxCountByHash(ctx, hash.Bytes())
}

// GetBlockTxCountByNumber counts the transactions in a specific block.
func (g *Gateway) GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error) {
	return g.store.GetBlockTxCountByNumber(ctx, num)
}

// State

// BalanceAt returns the balance of an account.
func (g *Gateway) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return g.endorsers.BalanceAt(ctx, account, blockNumber)
}

func (g *Gateway) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	return g.endorsers.StorageAt(ctx, account, key, blockNumber)
}

func (g *Gateway) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return g.endorsers.CodeAt(ctx, account, blockNumber)
}

func (g *Gateway) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return g.endorsers.NonceAt(ctx, account, blockNumber)
}

// Transactions

// TransactionByHash retrieves transaction data from either the queue or database.
// It first checks if the transaction is in the queue (pending or in-progress),
// then queries the database for committed transactions.
//
// Return values represent three possible states:
// - State 1 (Pending): Transaction in queue → tx with BlockNumber=0 (becomes null in JSON)
// - State 2 (Completed): Transaction in database → tx with block data populated
// - State 3 (Not Found): Transaction in neither location → nil
//
// The pending status is signaled by BlockNumber=0, which the API layer converts to null.
func (g *Gateway) TransactionByHash(ctx context.Context, hash common.Hash) (*domain.Transaction, error) {
	// Check if transaction is pending in the queue (either waiting or being processed)
	if pendingTx := g.TxQueue.IsPending(hash); pendingTx != nil {
		// Transaction is pending - return it with zero block fields
		// The API layer will convert these to nil in the JSON response
		rawTx, err := pendingTx.MarshalBinary()
		if err != nil {
			return nil, err
		}

		from, err := types.Sender(g.Signer, pendingTx)
		if err != nil {
			return nil, err
		}

		var toAddr []byte
		if to := pendingTx.To(); to != nil {
			toAddr = to.Bytes()
		}

		return &domain.Transaction{
			TxHash:      hash.Bytes(),
			BlockHash:   nil, // nil signals pending to API layer
			BlockNumber: 0,   // 0 signals pending to API layer
			TxIndex:     0,   // Value doesn't matter - API layer checks BlockNumber==0 for pending
			RawTx:       rawTx,
			FromAddress: from.Bytes(),
			ToAddress:   toAddr,
		}, nil
	}

	// Transaction not in queue, check database for committed transaction
	tx, err := g.store.GetTransactionByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, err
	}
	if tx == nil {
		// Transaction not found in queue or database
		return nil, nil
	}

	// Fetch logs for the transaction (needed for receipts)
	logs, err := g.store.GetLogsByTxHash(ctx, hash.Bytes())
	if err != nil {
		return nil, err
	}
	tx.Logs = logs

	// Transaction found in database
	return tx, nil
}

// GetTransactionByBlockHashAndIndex retrieves a transaction based on block hash in the transaction index in that block.
func (g *Gateway) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx int64) (*domain.Transaction, error) {
	return g.store.GetTransactionByBlockHashAndIndex(ctx, hash.Bytes(), idx)
}

// GetTransactionByBlockNumberAndIndex retrieves a transaction based on block number in the transaction index in that block.
func (g *Gateway) GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error) {
	return g.store.GetTransactionByBlockNumberAndIndex(ctx, num, idx)
}

func (g *Gateway) GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error) {
	return g.store.GetLogs(ctx, query)
}

// Stop performs an orderly shutdown of the gateway.
// It closes the transaction queue, waits for all workers to finish, and closes the batch submitter.
func (g *Gateway) Stop() error {
	var err error
	g.stopOnce.Do(func() {
		// Close the queue to signal workers to stop
		g.TxQueue.Close()

		// Wait for all workers to finish processing
		g.wg.Wait()

		// Stop and close batch submitter
		g.batchSubmitter.Stop()
		err = g.batchSubmitter.Close()
	})

	total, invalid, totalEnq, conflictEnq := g.TxQueue.Stats()
	if total > 0 {
		log.Println("gw stats: valid/invalid/invalid rate        ", total, invalid, float64(invalid)/float64(total))
		log.Println("gw stats: total/conflicting/conflicting rate", totalEnq, conflictEnq, float64(conflictEnq)/float64(totalEnq))
	}

	return err
}

// Handle processes block notifications and marks transactions as complete.
// This method implements blocks.BlockHandler and can be registered with the synchronizer
// to receive notifications when blocks are committed. It converts the blocks.Block to domain.Block
// and delegates to the TxQueue's Handle method.
func (g *Gateway) Handle(ctx context.Context, b blocks.Block) error {
	// Convert blocks.Block to domain.Block using the shared conversion function
	domainBlock := ConvertToDomain(b)
	err := g.TxQueue.Handle(ctx, &domainBlock)
	return err
}
