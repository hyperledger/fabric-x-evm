/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// Backend is the backend for the RPC API. Gas, fees and logs are mocked
// in the API itself, so not required in the Backend interface.
type Backend interface {
	ChainID(ctx context.Context) (*big.Int, error)   // ethereum.ChainIDReader
	BlockNumber(ctx context.Context) (uint64, error) // ethereum.BlockNumberReader

	// Blocks
	GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error)
	GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*domain.Block, error)
	GetBlockTxCountByHash(ctx context.Context, hash common.Hash) (int64, error)
	GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error)

	// State: ethereum.ChainStateReader
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)

	// Transactions
	SendTransaction(ctx context.Context, tx *types.Transaction) error                              // ethereum.TransactionSender
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) // ethereum.ContractCaller

	// Transactions. Our transactions also include the status, so we can build receipts out of the same data.
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *domain.Transaction, isPending bool, err error)
	GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx int64) (*domain.Transaction, error)
	GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error)
	GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error)
}

type EthAPI struct {
	b               Backend
	testAccounts    []common.Address
	testAccountKeys map[common.Address]*ecdsa.PrivateKey // address -> pre-converted private key
}

func NewEthAPI(b Backend, testAccounts []common.Address, testAccountKeys map[common.Address]*ecdsa.PrivateKey) *EthAPI {
	return &EthAPI{
		b:               b,
		testAccounts:    testAccounts,
		testAccountKeys: testAccountKeys,
	}
}

// Chain

// eth_chainId
func (api *EthAPI) ChainId(ctx context.Context) (*hexutil.Big, error) {
	chainID, err := api.b.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(chainID), nil
}

// eth_blockNumber
func (api *EthAPI) BlockNumber(ctx context.Context) (hexutil.Uint64, error) {
	num, err := api.b.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	return hexutil.Uint64(num), nil
}

// Blocks

// eth_accounts
func (api *EthAPI) Accounts(ctx context.Context) ([]common.Address, error) {
	return api.testAccounts, nil
}

// eth_getBlockByNumber
func (api *EthAPI) GetBlockByNumber(ctx context.Context, num rpc.BlockNumber, full bool) (*RPCBlock, error) {
	b, err := api.b.GetBlockByNumber(ctx, blockNumberToUint64(num), full)
	if err != nil {
		return nil, err
	}
	return rpcBlock(b, full), nil
}

// eth_getBlockByHash
func (api *EthAPI) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*RPCBlock, error) {
	b, err := api.b.GetBlockByHash(ctx, hash, full)
	if err != nil {
		return nil, err
	}
	return rpcBlock(b, full), nil
}

// eth_getBlockTransactionCountByHash
func (api *EthAPI) GetBlockTransactionCountByHash(ctx context.Context, hash common.Hash) (*hexutil.Uint, error) {
	c, err := api.b.GetBlockTxCountByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	u := hexutil.Uint(c)
	return &u, nil
}

// eth_getBlockTransactionCountByNumber
func (api *EthAPI) GetBlockTransactionCountByNumber(ctx context.Context, num rpc.BlockNumber) (*hexutil.Uint, error) {
	c, err := api.b.GetBlockTxCountByNumber(ctx, blockNumberToUint64(num))
	if err != nil {
		return nil, err
	}
	u := hexutil.Uint(c)
	return &u, nil
}

// State

// eth_getBalance
func (api *EthAPI) GetBalance(ctx context.Context, address common.Address, block rpc.BlockNumberOrHash) (*hexutil.Big, error) {
	b, err := api.b.BalanceAt(ctx, address, blockNumberOrHashToBlockNumber(block))
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(b), nil
}

// eth_getCode
func (api *EthAPI) GetCode(ctx context.Context, addr common.Address, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	code, err := api.b.CodeAt(ctx, addr, blockNumberOrHashToBlockNumber(block))
	if err != nil {
		return nil, err
	}
	return (hexutil.Bytes)(code), nil
}

// eth_getStorageAt
func (api *EthAPI) GetStorageAt(ctx context.Context, addr common.Address, slot common.Hash, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	data, err := api.b.StorageAt(ctx, addr, slot, blockNumberOrHashToBlockNumber(block))
	if err != nil {
		return nil, err
	}
	return (hexutil.Bytes)(data), nil
}

// eth_getTransactionCount
func (api *EthAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	nonce, err := api.b.NonceAt(ctx, address, blockNumberOrHashToBlockNumber(blockNrOrHash))
	if err != nil {
		return nil, err
	}
	n := hexutil.Uint64(nonce)
	return &n, nil
}

// Transactions

// eth_sendRawTransaction
func (api *EthAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	if err := api.b.SendTransaction(ctx, tx); err != nil {
		return common.Hash{}, err
	}
	return tx.Hash(), nil
}

// eth_sendTransaction
func (api *EthAPI) SendTransaction(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	// Validate from address
	if args.From == nil {
		return common.Hash{}, fmt.Errorf("missing 'from' field")
	}

	// Get private key for this address
	privateKey, ok := api.testAccountKeys[*args.From]
	if !ok {
		return common.Hash{}, fmt.Errorf("no private key available for address %s", args.From.Hex())
	}

	// Set defaults for unspecified fields
	args.setDefaults()

	// Get nonce if not specified
	var nonce uint64
	if args.Nonce != nil {
		nonce = uint64(*args.Nonce)
	} else {
		var err error
		nonce, err = api.b.NonceAt(ctx, *args.From, nil)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to get nonce: %w", err)
		}
	}

	// Get chainID
	chainID, err := api.b.ChainID(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get chainID: %w", err)
	}

	// Build transaction
	var tx *types.Transaction
	data := args.data()
	gasLimit := uint64(*args.Gas)
	value := (*big.Int)(args.Value)
	gasPrice := (*big.Int)(args.GasPrice)

	if args.To != nil {
		// Contract call or transfer
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			To:       args.To,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	} else {
		// Contract deployment
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			To:       nil,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	}

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send signed transaction
	if err := api.b.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}

	return signedTx.Hash(), nil
}

// eth_getTransactionByHash
func (api *EthAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	tx, _, err := api.b.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return rpcTransaction(tx), nil
}

// eth_getTransactionByBlockHashAndIndex
func (api *EthAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx hexutil.Uint) (*RPCTransaction, error) {
	tx, err := api.b.GetTransactionByBlockHashAndIndex(ctx, hash, int64(idx))
	if err != nil {
		return nil, err
	}
	return rpcTransaction(tx), nil
}

// eth_getTransactionByBlockNumberAndIndex
func (api *EthAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, num rpc.BlockNumber, idx hexutil.Uint) (*RPCTransaction, error) {
	tx, err := api.b.GetTransactionByBlockNumberAndIndex(ctx, blockNumberToUint64(num), int64(idx))
	if err != nil {
		return nil, err
	}
	return rpcTransaction(tx), nil
}

// eth_getTransactionReceipt
func (api *EthAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (*rpcReceipt, error) {
	r, _, err := api.b.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return receipt(r), nil
}

// eth_call
func (api *EthAPI) Call(ctx context.Context, args map[string]any, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	callMsg, err := argsToCallMsg(args)
	if err != nil {
		return nil, err
	}
	blockNum := blockNumberOrHashToBlockNumber(block)
	return api.b.CallContract(ctx, callMsg, blockNum)
}

// Fees -- mocked

// eth_estimateGas
func (api *EthAPI) EstimateGas(ctx context.Context, args map[string]any, block *rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	u := hexutil.Uint64(0)
	return &u, nil
}

// eth_gasPrice
func (api *EthAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(1)), nil
}

// eth_maxPriorityFeePerGas
func (api *EthAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(1)), nil
}

// eth_feeHistory
func (api *EthAPI) FeeHistory(ctx context.Context, blockCount hexutil.Uint, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	zero := (*hexutil.Big)(big.NewInt(0))

	baseFee := make([]*hexutil.Big, blockCount+1)
	for i := range baseFee {
		baseFee[i] = zero
	}
	gasUsedRatio := make([]float64, blockCount)

	reward := make([][]*hexutil.Big, blockCount)
	for i := range reward {
		reward[i] = make([]*hexutil.Big, len(rewardPercentiles))
		for j := range reward[i] {
			reward[i][j] = zero
		}
	}

	return &FeeHistoryResult{
		OldestBlock:  (*hexutil.Big)(big.NewInt(0)),
		BaseFee:      baseFee,
		GasUsedRatio: gasUsedRatio,
		Reward:       reward,
	}, nil
}

// Logs

// eth_getLogs
func (api *EthAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*types.Log, error) {
	query := filterCriteriaToLogFilter(crit)

	logs, err := api.b.GetLogs(ctx, query)
	if err != nil {
		return nil, err
	}

	result := make([]*types.Log, len(logs))
	for i, l := range logs {
		result[i] = domainLogToTypesLog(l)
	}
	return result, nil
}

func filterCriteriaToLogFilter(crit filters.FilterCriteria) domain.LogFilter {
	filter := domain.LogFilter{}

	if crit.BlockHash != nil {
		hash := crit.BlockHash.Bytes()
		filter.BlockHash = &hash
	} else {
		if crit.FromBlock != nil {
			from := crit.FromBlock.Uint64()
			filter.FromBlock = &from
		}
		if crit.ToBlock != nil {
			to := crit.ToBlock.Uint64()
			filter.ToBlock = &to
		}
	}

	if len(crit.Addresses) > 0 {
		filter.Addresses = make([][]byte, len(crit.Addresses))
		for i, addr := range crit.Addresses {
			filter.Addresses[i] = addr.Bytes()
		}
	}

	if len(crit.Topics) > 0 {
		filter.Topics = make([][][]byte, len(crit.Topics))
		for i, alternatives := range crit.Topics {
			if len(alternatives) > 0 {
				filter.Topics[i] = make([][]byte, len(alternatives))
				for j, topic := range alternatives {
					filter.Topics[i][j] = topic.Bytes()
				}
			}
		}
	}

	return filter
}

func domainLogToTypesLog(l domain.Log) *types.Log {
	topics := make([]common.Hash, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = common.BytesToHash(t)
	}

	return &types.Log{
		Address:     common.BytesToAddress(l.Address),
		Topics:      topics,
		Data:        l.Data,
		BlockNumber: l.BlockNumber,
		TxHash:      common.BytesToHash(l.TxHash),
		TxIndex:     uint(l.TxIndex),
		Index:       uint(l.LogIndex),
	}
}

func argsToCallMsg(args map[string]any) (ethereum.CallMsg, error) {
	var msg ethereum.CallMsg

	if v, ok := args["from"]; ok {
		msg.From = common.HexToAddress(v.(string))
	}

	if v, ok := args["to"]; ok {
		addr := common.HexToAddress(v.(string))
		msg.To = &addr
	}

	if v, ok := args["gas"]; ok {
		gas, err := hexutil.DecodeUint64(v.(string))
		if err != nil {
			return msg, err
		}
		msg.Gas = gas
	}

	if v, ok := args["gasPrice"]; ok {
		gp, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, err
		}
		msg.GasPrice = gp
	}

	if v, ok := args["value"]; ok {
		val, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, err
		}
		msg.Value = val
	}

	if v, ok := args["input"]; ok {
		data, err := hexutil.Decode(v.(string))
		if err != nil {
			return msg, err
		}
		msg.Data = data
	}

	// EIP-1559 (optional, ignore safely if absent)
	if v, ok := args["maxFeePerGas"]; ok {
		fee, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, err
		}
		msg.GasFeeCap = fee
	}

	if v, ok := args["maxPriorityFeePerGas"]; ok {
		tip, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, err
		}
		msg.GasTipCap = tip
	}

	return msg, nil
}

// blockNumberOrHashToBlockNumber converts rpc.BlockNumberOrHash to *big.Int
func blockNumberOrHashToBlockNumber(numOrHash rpc.BlockNumberOrHash) *big.Int {
	if num, ok := numOrHash.Number(); ok {
		return rpcBlockNumberToBigInt(num)
	}
	// TODO: For block hash, we now return nil (latest).
	return nil
}

// rpcBlockNumberToBigInt converts rpc.BlockNumber to *big.Int
func rpcBlockNumberToBigInt(num rpc.BlockNumber) *big.Int {
	if num == rpc.PendingBlockNumber || num == rpc.LatestBlockNumber {
		return nil
	}
	return big.NewInt(num.Int64())
}

func blockNumberToUint64(num rpc.BlockNumber) uint64 {
	n := uint64(0)
	if num > 0 {
		n = uint64(num)
	}
	return n
}
