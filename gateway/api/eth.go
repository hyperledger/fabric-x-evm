/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/gateway/api/rpcerr"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

var logger = flogging.MustGetLogger("gateway.api.eth")

// Backend is the backend for the RPC API. Gas, fees and logs are mocked
// in the API itself, so not required in the Backend interface.
type Backend interface {
	ChainID(ctx context.Context) (*big.Int, error)   // ethereum.ChainIDReader
	BlockNumber(ctx context.Context) (uint64, error) // ethereum.BlockNumberReader

	// Blocks
	GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error)
	GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*domain.Block, error)
	BlockNumberByHash(ctx context.Context, hash common.Hash) (*uint64, error)
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
	// For pending transactions, BlockNumber will be 0 (converted to null in JSON response).
	TransactionByHash(ctx context.Context, hash common.Hash) (*domain.Transaction, error)
	GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx int64) (*domain.Transaction, error)
	GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error)
	GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error)
}

type EthAPI struct {
	b Backend
}

func NewEthAPI(b Backend) *EthAPI {
	return &EthAPI{
		b: b,
	}
}

// Backend returns the backend interface for use by wrappers
func (api *EthAPI) Backend() Backend {
	logger.Debugf("EthAPI.Backend() called")
	return api.b
}

// Chain

// eth_chainId
func (api *EthAPI) ChainId(ctx context.Context) (*hexutil.Big, error) {
	logger.Debugf("EthAPI.ChainId() called")
	chainID, err := api.b.ChainID(ctx)
	if err != nil {
		logger.Debugf("EthAPI.ChainId() returning error: %v", err)
		return nil, err
	}
	result := (*hexutil.Big)(chainID)
	logger.Debugf("EthAPI.ChainId() returning: %s", result.String())
	return result, nil
}

// eth_blockNumber
func (api *EthAPI) BlockNumber(ctx context.Context) (hexutil.Uint64, error) {
	logger.Debugf("EthAPI.BlockNumber() called")
	num, err := api.b.BlockNumber(ctx)
	if err != nil {
		logger.Debugf("EthAPI.BlockNumber() returning error: %v", err)
		return 0, err
	}
	result := hexutil.Uint64(num)
	logger.Debugf("EthAPI.BlockNumber() returning: %d", result)
	return result, nil
}

// Blocks

// eth_getBlockByNumber
func (api *EthAPI) GetBlockByNumber(ctx context.Context, num rpc.BlockNumber, full bool) (*RPCBlock, error) {
	logger.Debugf("EthAPI.GetBlockByNumber() called with num=%v, full=%v", num, full)
	b, err := api.b.GetBlockByNumber(ctx, blockNumberToUint64(num), full)
	if err != nil {
		logger.Debugf("EthAPI.GetBlockByNumber() returning error: %v", err)
		return nil, err
	}
	result := rpcBlock(b, full)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetBlockByNumber() returning: %s", string(resultJSON))
	}
	return result, nil
}

// eth_getBlockByHash
func (api *EthAPI) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*RPCBlock, error) {
	logger.Debugf("EthAPI.GetBlockByHash() called with hash=%s, full=%v", hash.Hex(), full)
	b, err := api.b.GetBlockByHash(ctx, hash, full)
	if err != nil {
		logger.Debugf("EthAPI.GetBlockByHash() returning error: %v", err)
		return nil, err
	}
	result := rpcBlock(b, full)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetBlockByHash() returning: %s", string(resultJSON))
	}
	return result, nil
}

// eth_getBlockTransactionCountByHash
func (api *EthAPI) GetBlockTransactionCountByHash(ctx context.Context, hash common.Hash) (*hexutil.Uint, error) {
	logger.Debugf("EthAPI.GetBlockTransactionCountByHash() called with hash=%s", hash.Hex())
	c, err := api.b.GetBlockTxCountByHash(ctx, hash)
	if err != nil {
		logger.Debugf("EthAPI.GetBlockTransactionCountByHash() returning error: %v", err)
		return nil, err
	}
	u := hexutil.Uint(c)
	logger.Debugf("EthAPI.GetBlockTransactionCountByHash() returning: %d", u)
	return &u, nil
}

// eth_getBlockTransactionCountByNumber
func (api *EthAPI) GetBlockTransactionCountByNumber(ctx context.Context, num rpc.BlockNumber) (*hexutil.Uint, error) {
	logger.Debugf("EthAPI.GetBlockTransactionCountByNumber() called with num=%v", num)
	c, err := api.b.GetBlockTxCountByNumber(ctx, blockNumberToUint64(num))
	if err != nil {
		logger.Debugf("EthAPI.GetBlockTransactionCountByNumber() returning error: %v", err)
		return nil, err
	}
	u := hexutil.Uint(c)
	logger.Debugf("EthAPI.GetBlockTransactionCountByNumber() returning: %d", u)
	return &u, nil
}

// State

// eth_getBalance
func (api *EthAPI) GetBalance(ctx context.Context, address common.Address, block rpc.BlockNumberOrHash) (*hexutil.Big, error) {
	logger.Debugf("EthAPI.GetBalance() called with address=%s", address.Hex())
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, block)
	if err != nil {
		logger.Debugf("EthAPI.GetBalance() returning error: %v", err)
		return nil, err
	}
	b, err := api.b.BalanceAt(ctx, address, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.GetBalance() returning error: %v", err)
		return nil, err
	}
	result := (*hexutil.Big)(b)
	logger.Debugf("EthAPI.GetBalance() returning: %s", result.String())
	return result, nil
}

// eth_getCode
func (api *EthAPI) GetCode(ctx context.Context, addr common.Address, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	logger.Debugf("EthAPI.GetCode() called with addr=%s", addr.Hex())
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, block)
	if err != nil {
		logger.Debugf("EthAPI.GetCode() returning error: %v", err)
		return nil, err
	}
	code, err := api.b.CodeAt(ctx, addr, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.GetCode() returning error: %v", err)
		return nil, err
	}
	result := (hexutil.Bytes)(code)
	logger.Debugf("EthAPI.GetCode() returning: %s (len=%d)", result.String(), len(result))
	return result, nil
}

// eth_getStorageAt
func (api *EthAPI) GetStorageAt(ctx context.Context, addr common.Address, slot common.Hash, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	logger.Debugf("EthAPI.GetStorageAt() called with addr=%s, slot=%s", addr.Hex(), slot.Hex())
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, block)
	if err != nil {
		logger.Debugf("EthAPI.GetStorageAt() returning error: %v", err)
		return nil, err
	}
	data, err := api.b.StorageAt(ctx, addr, slot, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.GetStorageAt() returning error: %v", err)
		return nil, err
	}
	result := (hexutil.Bytes)(data)
	logger.Debugf("EthAPI.GetStorageAt() returning: %s", result.String())
	return result, nil
}

// eth_getTransactionCount
func (api *EthAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	logger.Debugf("EthAPI.GetTransactionCount() called with address=%s", address.Hex())
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, blockNrOrHash)
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionCount() returning error: %v", err)
		return nil, err
	}
	nonce, err := api.b.NonceAt(ctx, address, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionCount() returning error: %v", err)
		return nil, err
	}
	n := hexutil.Uint64(nonce)
	logger.Debugf("EthAPI.GetTransactionCount() returning: %d", n)
	return &n, nil
}

// Transactions

// eth_sendRawTransaction
func (api *EthAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	logger.Debugf("EthAPI.SendRawTransaction() called")
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		logger.Debugf("EthAPI.SendRawTransaction() returning error: %v", err)
		return common.Hash{}, rpcerr.InvalidParams("invalid raw transaction: %v", err)
	}
	if b, err := tx.MarshalJSON(); err == nil {
		logger.Debugf("EthAPI.SendRawTransaction() tx: %s", string(b))
	}
	if err := api.b.SendTransaction(ctx, tx); err != nil {
		logger.Debugf("EthAPI.SendRawTransaction() returning error: %v", err)
		return common.Hash{}, classifyValidationError(err)
	}
	hash := tx.Hash()
	logger.Debugf("EthAPI.SendRawTransaction() returning hash: %s", hash.Hex())
	return hash, nil
}

// eth_getTransactionByHash
func (api *EthAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	logger.Debugf("EthAPI.GetTransactionByHash() called with hash=%s", hash.Hex())
	tx, err := api.b.TransactionByHash(ctx, hash)
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionByHash() returning error: %v", err)
		return nil, err
	}
	result := rpcTransaction(tx)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetTransactionByHash() returning: %s", string(resultJSON))
	}
	return result, nil
}

// eth_getTransactionByBlockHashAndIndex
func (api *EthAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx hexutil.Uint) (*RPCTransaction, error) {
	logger.Debugf("EthAPI.GetTransactionByBlockHashAndIndex() called with hash=%s, idx=%d", hash.Hex(), idx)
	tx, err := api.b.GetTransactionByBlockHashAndIndex(ctx, hash, int64(idx))
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionByBlockHashAndIndex() returning error: %v", err)
		return nil, err
	}
	result := rpcTransaction(tx)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetTransactionByBlockHashAndIndex() returning: %s", string(resultJSON))
	}
	return result, nil
}

// eth_getTransactionByBlockNumberAndIndex
func (api *EthAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, num rpc.BlockNumber, idx hexutil.Uint) (*RPCTransaction, error) {
	logger.Debugf("EthAPI.GetTransactionByBlockNumberAndIndex() called with num=%v, idx=%d", num, idx)
	tx, err := api.b.GetTransactionByBlockNumberAndIndex(ctx, blockNumberToUint64(num), int64(idx))
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionByBlockNumberAndIndex() returning error: %v", err)
		return nil, err
	}
	result := rpcTransaction(tx)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetTransactionByBlockNumberAndIndex() returning: %s", string(resultJSON))
	}
	return result, nil
}

// eth_getTransactionReceipt
func (api *EthAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (*rpcReceipt, error) {
	logger.Debugf("EthAPI.GetTransactionReceipt() called with hash=%s", hash.Hex())
	r, err := api.b.TransactionByHash(ctx, hash)
	if err != nil {
		logger.Debugf("EthAPI.GetTransactionReceipt() returning error: %v", err)
		return nil, err
	}
	result := receipt(r)
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetTransactionReceipt() returning: %s", string(resultJSON))
	} else {
		logger.Debugf("EthAPI.GetTransactionReceipt() returning nada")
	}
	return result, nil
}

// eth_call
func (api *EthAPI) Call(ctx context.Context, args map[string]any, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	logger.Debugf("EthAPI.Call() called with args=%v", args)
	callMsg, err := argsToCallMsg(args)
	if err != nil {
		logger.Debugf("EthAPI.Call() returning error: %v", err)
		return nil, err
	}
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, block)
	if err != nil {
		logger.Debugf("EthAPI.Call() returning error: %v", err)
		return nil, err
	}
	logger.Debugf("EthAPI.Call() using blockNum %d", blockNum)
	ret, err := api.b.CallContract(ctx, callMsg, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.Call() returning error: %v", err)
		return nil, classifyCallError(err)
	}
	logger.Debugf("EthAPI.Call() returning: %s (len=%d)", hexutil.Bytes(ret).String(), len(ret))
	return ret, nil
}

// Fees -- mocked

// eth_estimateGas
func (api *EthAPI) EstimateGas(ctx context.Context, args map[string]any, block *rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	logger.Debugf("EthAPI.EstimateGas() called with args=%v", args)

	// we invoke api.Call first to see if the tx is valid and won't revert
	blockRef := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if block != nil {
		blockRef = *block
	}
	if _, err := api.Call(ctx, args, blockRef); err != nil {
		return nil, err
	}

	// Gas is not metered; return a constant that satisfies the intrinsic-gas
	// check in ValidateTx and allows Metamask/wallets to submit transactions.
	// TODO: Implement proper gas estimation based on actual execution
	u := hexutil.Uint64(10_000_000)
	logger.Debugf("EthAPI.EstimateGas() returning: %d", u)
	return &u, nil
}

// eth_gasPrice
func (api *EthAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	logger.Debugf("EthAPI.GasPrice() called")
	result := (*hexutil.Big)(big.NewInt(0))
	logger.Debugf("EthAPI.GasPrice() returning: %s", result.String())
	return result, nil
}

// eth_maxPriorityFeePerGas
func (api *EthAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	logger.Debugf("EthAPI.MaxPriorityFeePerGas() called")
	result := (*hexutil.Big)(big.NewInt(0))
	logger.Debugf("EthAPI.MaxPriorityFeePerGas() returning: %s", result.String())
	return result, nil
}

// eth_feeHistory
func (api *EthAPI) FeeHistory(ctx context.Context, blockCount hexutil.Uint, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	logger.Debugf("EthAPI.FeeHistory() called with blockCount=%d, lastBlock=%v", blockCount, lastBlock)
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

	result := &FeeHistoryResult{
		OldestBlock:  (*hexutil.Big)(big.NewInt(0)),
		BaseFee:      baseFee,
		GasUsedRatio: gasUsedRatio,
		Reward:       reward,
	}
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.FeeHistory() returning: %s", string(resultJSON))
	}
	return result, nil
}

// Logs

// eth_getLogs
func (api *EthAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*types.Log, error) {
	logger.Debugf("EthAPI.GetLogs() called with criteria=%+v", crit)
	query := filterCriteriaToLogFilter(crit)

	logs, err := api.b.GetLogs(ctx, query)
	if err != nil {
		logger.Debugf("EthAPI.GetLogs() returning error: %v", err)
		return nil, err
	}

	result := make([]*types.Log, len(logs))
	for i, l := range logs {
		result[i] = domainLogToTypesLog(l)
	}
	if resultJSON, err := json.Marshal(result); err == nil {
		logger.Debugf("EthAPI.GetLogs() returning %d logs: %s", len(result), string(resultJSON))
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
		BlockHash:   common.BytesToHash(l.BlockHash),
		TxHash:      common.BytesToHash(l.TxHash),
		TxIndex:     uint(l.TxIndex),
		Index:       uint(l.LogIndex),
	}
}

func argsToCallMsg(args map[string]any) (ethereum.CallMsg, error) {
	var msg ethereum.CallMsg

	if v, ok := args["from"]; ok && v != nil {
		msg.From = common.HexToAddress(v.(string))
	}

	// "to" may be explicitly null for contract-creation calls.
	if v, ok := args["to"]; ok && v != nil {
		addr := common.HexToAddress(v.(string))
		msg.To = &addr
	}

	if v, ok := args["gas"]; ok && v != nil {
		gas, err := hexutil.DecodeUint64(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid gas: %v", err)
		}
		msg.Gas = gas
	}

	if v, ok := args["gasPrice"]; ok && v != nil {
		gp, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid gasPrice: %v", err)
		}
		msg.GasPrice = gp
	}

	if v, ok := args["value"]; ok && v != nil {
		val, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid value: %v", err)
		}
		msg.Value = val
	}

	// "input" is the canonical field; "data" is the legacy alias (used by some clients)
	inputKey := "input"
	if _, ok := args["input"]; !ok {
		if _, ok := args["data"]; ok {
			inputKey = "data"
		}
	}
	if v, ok := args[inputKey]; ok && v != nil {
		data, err := hexutil.Decode(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid %s: %v", inputKey, err)
		}
		msg.Data = data
	}

	// EIP-1559 (optional, ignore safely if absent)
	if v, ok := args["maxFeePerGas"]; ok && v != nil {
		fee, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid maxFeePerGas: %v", err)
		}
		msg.GasFeeCap = fee
	}

	if v, ok := args["maxPriorityFeePerGas"]; ok && v != nil {
		tip, err := hexutil.DecodeBig(v.(string))
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid maxPriorityFeePerGas: %v", err)
		}
		msg.GasTipCap = tip
	}

	return msg, nil
}

// blockNumberOrHashToBlockNumber converts rpc.BlockNumberOrHash to *big.Int.
// If a block hash is provided, it resolves the hash to a block number.
func (api *EthAPI) blockNumberOrHashToBlockNumber(ctx context.Context, numOrHash rpc.BlockNumberOrHash) (*big.Int, error) {
	if num, ok := numOrHash.Number(); ok {
		return rpcBlockNumberToBigInt(num), nil
	}

	hash, ok := numOrHash.Hash()
	if !ok {
		return nil, nil
	}

	num, err := api.b.BlockNumberByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if num == nil {
		return nil, ethereum.NotFound
	}
	return new(big.Int).SetUint64(*num), nil
}

// rpcBlockNumberToBigInt converts rpc.BlockNumber to *big.Int
func rpcBlockNumberToBigInt(num rpc.BlockNumber) *big.Int {
	if num == rpc.PendingBlockNumber || num == rpc.LatestBlockNumber {
		return nil
	}
	return big.NewInt(num.Int64())
}

// blockNumberToUint64 converts an rpc.BlockNumber to a uint64.
// "earliest" maps to 0 (genesis). All other negative sentinels (latest, pending,
// safe, finalized) map to math.MaxUint64, which the backend interprets as "latest".
func blockNumberToUint64(num rpc.BlockNumber) uint64 {
	if num == rpc.EarliestBlockNumber {
		return 0
	}
	if num < 0 {
		return math.MaxUint64
	}
	return uint64(num)
}
