/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
	submitter Submitter
	endorsers *EndorsementClient
	store     Store
	chainID   *big.Int
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
func New(ec *EndorsementClient, submitter Submitter, store Store, chainID int64) (*Gateway, error) {
	return &Gateway{
		endorsers: ec,
		submitter: submitter,
		store:     store,
		chainID:   big.NewInt(chainID),
	}, nil
}

// SendTransaction sends a signed ethereum transaction to the endorsers and submits the result to the orderer.
// As per standard ethereum APIs, it does not return the payload of executed transaction.
func (g Gateway) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	end, err := g.ExecuteEthTx(ctx, tx, nil)
	if err != nil {
		return err
	}
	if err := g.SubmitFabricTx(ctx, end); err != nil {
		return err
	}
	return nil
}

// CallContract is a query. It doesn't require a signature of the end user and doesn't change the ledger or nonce.
// We requests endorsement from a single endorser, return the payload, and discard the signed response.
// This is the same way queries are handled in Fabric.
func (g Gateway) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return g.endorsers.CallContract(ctx, call, &utils.BlockInfo{BlockNumber: blockNumber})
}

// ExecuteEthTx requests endorsements for the submitted ethereum-style transaction.
func (g Gateway) ExecuteEthTx(ctx context.Context, tx *types.Transaction, blockInfo *utils.BlockInfo) (sdk.Endorsement, error) {
	return g.endorsers.ExecuteTransaction(ctx, tx, blockInfo)
}

// SubmitFabricTx submits a Fabric envelope.
func (g Gateway) SubmitFabricTx(ctx context.Context, end sdk.Endorsement) error {
	if err := g.submitter.Submit(ctx, end); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	return nil
}

// ChainID returns the configured chainID for this deployment.
func (g Gateway) ChainID(ctx context.Context) (*big.Int, error) {
	return g.chainID, nil
}

// BlockNumber is the current blockheight as observed by this gateway.
func (g Gateway) BlockNumber(ctx context.Context) (uint64, error) {
	return g.store.BlockNumber(ctx)
}

// GetBlockByNumber returns the block at the specified number.
// If full is true, the block includes transactions.
// num == 0 means "latest" (blocks start at 1 since they map directly to Fabric block numbers).
func (g Gateway) GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error) {
	if num == 0 {
		return g.store.LatestBlock(ctx, full)
	}
	return g.store.GetBlockByNumber(ctx, num, full)
}

// GetBlockByHash returns block metadata based on the block hash.
// If full is true, the block includes transactions.
func (g Gateway) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*domain.Block, error) {
	return g.store.GetBlockByHash(ctx, hash.Bytes(), full)
}

// GetBlockTxCountByHash counts the transactions in a specific block.
func (g Gateway) GetBlockTxCountByHash(ctx context.Context, hash common.Hash) (int64, error) {
	return g.store.GetBlockTxCountByHash(ctx, hash.Bytes())
}

// GetBlockTxCountByNumber counts the transactions in a specific block.
func (g Gateway) GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error) {
	return g.store.GetBlockTxCountByNumber(ctx, num)
}

// State

// BalanceAt returns the balance of an account.
func (g Gateway) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
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

func (g Gateway) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
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

func (g Gateway) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
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

func (g Gateway) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
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

// TransactionByHash retrieves a past transaction data from local storage, reconstructs a Transaction object and returns it.
// We always return false for "is pending".
func (g Gateway) TransactionByHash(ctx context.Context, hash common.Hash) (*domain.Transaction, bool, error) {
	tx, err := g.store.GetTransactionByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, false, err
	}
	if tx == nil {
		return nil, false, nil
	}

	// Fetch logs for the transaction (needed for receipts)
	logs, err := g.store.GetLogsByTxHash(ctx, hash.Bytes())
	if err != nil {
		return nil, false, err
	}
	tx.Logs = logs

	return tx, false, nil
}

// GetTransactionByBlockHashAndIndex retrieves a transaction based on block hash in the transaction index in that block.
func (g Gateway) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx int64) (*domain.Transaction, error) {
	return g.store.GetTransactionByBlockHashAndIndex(ctx, hash.Bytes(), idx)
}

// GetTransactionByBlockNumberAndIndex retrieves a transaction based on block number in the transaction index in that block.
func (g Gateway) GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error) {
	return g.store.GetTransactionByBlockNumberAndIndex(ctx, num, idx)
}

func (g Gateway) GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error) {
	return g.store.GetLogs(ctx, query)
}

// Stop closes all connections.
func (g Gateway) Stop() error {
	return g.submitter.Close()
}
