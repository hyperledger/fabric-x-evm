/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/utils"
	sdk "github.com/hyperledger/fabric-x-sdk"
)

type Signer interface {
	Sign(msg []byte) ([]byte, error)
	Serialize() ([]byte, error)
}

type Submitter interface {
	Submit(context.Context, sdk.Endorsement) error
	Close() error
}

// Gateway is the component that bridges Fabric-x and the EVM. Its API is the
// Ethereum JSON RPC. When the user submits a transaction targeting an Ethereum
// contract, the gateway requests endorsement from a set of EVM endorsers. It then
// submits a signed transaction with the read/writeset to the Fabric orderers.
type Gateway struct {
	submitter   Submitter
	endorsers   *EndorsementClient
	store       Store
	chainID     *big.Int
	chainConfig *params.ChainConfig
	signer      types.Signer
	txQueue     *TxQueue
	pendingTxs  sync.Map // common.Hash -> pendingTx
	workerCount int
	wg          sync.WaitGroup
	stopOnce    sync.Once
}

// pendingTx is the data we keep about a tx accepted by SendTransaction but not yet committed.
type pendingTx struct {
	tx   *types.Transaction
	from common.Address
}

type Store interface {
	BlockNumber(ctx context.Context) (uint64, error)
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
func New(ec *EndorsementClient, submitter Submitter, store Store, chainID int64, workerCount int) (*Gateway, error) {
	if workerCount <= 0 {
		workerCount = 1
	}

	cid := big.NewInt(chainID)
	return &Gateway{
		endorsers:   ec,
		submitter:   submitter,
		store:       store,
		chainID:     cid,
		chainConfig: cmn.BuildChainConfig(chainID),
		signer:      types.LatestSignerForChainID(cid),
		txQueue:     NewTxQueue(),
		workerCount: workerCount,
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
		tx, ok := g.txQueue.Dequeue()
		if !ok {
			// Queue is closed and empty
			return
		}

		// Process the transaction (old SendTransaction logic)
		if err := g.processTx(ctx, tx); err != nil {
			// TODO: Add proper error handling/logging
			// For now, we just continue processing
			continue
		}
	}
}

// processTx handles the actual transaction processing
func (g *Gateway) processTx(ctx context.Context, tx *types.Transaction) error {
	end, err := g.ExecuteEthTx(ctx, tx, nil)
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
	if err := ValidateTx(ctx, tx, g.chainConfig, g.signer, g); err != nil {
		return err
	}
	from, err := types.Sender(g.signer, tx)
	if err != nil {
		return err
	}
	g.pendingTxs.Store(tx.Hash(), pendingTx{tx: tx, from: from})
	g.txQueue.Enqueue(tx)
	return nil
}

// CallContract is a query. It doesn't require a signature of the end user and doesn't change the ledger or nonce.
// We requests endorsement from a single endorser, return the payload, and discard the signed response.
// This is the same way queries are handled in Fabric.
func (g *Gateway) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return g.endorsers.CallContract(ctx, call, &utils.BlockInfo{BlockNumber: blockNumber})
}

// ExecuteEthTx requests endorsements for the submitted ethereum-style transaction.
func (g *Gateway) ExecuteEthTx(ctx context.Context, tx *types.Transaction, blockInfo *utils.BlockInfo) (sdk.Endorsement, error) {
	return g.endorsers.ExecuteTransaction(ctx, tx, blockInfo)
}

// SubmitFabricTx submits a Fabric envelope.
func (g *Gateway) SubmitFabricTx(ctx context.Context, end sdk.Endorsement) error {
	if err := g.submitter.Submit(ctx, end); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	return nil
}

// ChainID returns the configured chainID for this deployment.
func (g *Gateway) ChainID(ctx context.Context) (*big.Int, error) {
	return g.chainID, nil
}

// BlockNumber is the current blockheight as observed by this gateway.
func (g *Gateway) BlockNumber(ctx context.Context) (uint64, error) {
	return g.store.BlockNumber(ctx)
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
	res, err := g.endorsers.GetState(ctx, cmn.StateQuery{
		Account:     account,
		BlockNumber: blockNumber,
		Type:        cmn.QueryTypeBalance,
	})
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(res), err
}

func (g *Gateway) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	res, err := g.endorsers.GetState(ctx, cmn.StateQuery{
		Account:     account,
		Key:         key,
		BlockNumber: blockNumber,
		Type:        cmn.QueryTypeStorage,
	})
	if err != nil {
		return nil, err
	}
	return res, err
}

func (g *Gateway) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	res, err := g.endorsers.GetState(ctx, cmn.StateQuery{
		Account:     account,
		BlockNumber: blockNumber,
		Type:        cmn.QueryTypeCode,
	})
	if err != nil {
		return nil, err
	}
	return res, err
}

func (g *Gateway) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	res, err := g.endorsers.GetState(ctx, cmn.StateQuery{
		Account:     account,
		BlockNumber: blockNumber,
		Type:        cmn.QueryTypeNonce,
	})
	if len(res) == 0 || err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint64(res), err
}

// Transactions

// TransactionByHash returns isPending=true for a tx that was accepted by
// SendTransaction but is not yet visible in a committed block, and
// isPending=false once the local store has indexed it.
func (g *Gateway) TransactionByHash(ctx context.Context, hash common.Hash) (*domain.Transaction, bool, error) {
	tx, err := g.store.GetTransactionByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, false, err
	}
	if tx != nil {
		// Lazy cleanup so the pending map doesn't grow forever.
		g.pendingTxs.Delete(hash)

		logs, err := g.store.GetLogsByTxHash(ctx, hash.Bytes())
		if err != nil {
			return nil, false, err
		}
		tx.Logs = logs
		return tx, false, nil
	}

	if v, ok := g.pendingTxs.Load(hash); ok {
		p := v.(pendingTx)
		return pendingDomainTx(p.tx, p.from), true, nil
	}
	return nil, false, nil
}

// pendingDomainTx synthesises a domain.Transaction for a queued-but-uncommitted
// tx. Block-context fields (BlockHash/Number, TxIndex, Status, Logs) are zero.
func pendingDomainTx(tx *types.Transaction, from common.Address) *domain.Transaction {
	raw, _ := tx.MarshalBinary()
	d := &domain.Transaction{
		TxHash:      tx.Hash().Bytes(),
		RawTx:       raw,
		FromAddress: from.Bytes(),
	}
	if to := tx.To(); to != nil {
		d.ToAddress = to.Bytes()
	} else {
		d.ContractAddress = crypto.CreateAddress(from, tx.Nonce()).Bytes()
	}
	return d
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
// It closes the transaction queue, waits for all workers to finish, and closes connections.
func (g *Gateway) Stop() error {
	var err error
	g.stopOnce.Do(func() {
		// Close the queue to signal workers to stop
		g.txQueue.Close()

		// Wait for all workers to finish processing
		g.wg.Wait()

		// Close submitter connection
		err = g.submitter.Close()
	})
	return err
}
