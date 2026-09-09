/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethmath "github.com/ethereum/go-ethereum/common/math"
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
	// EstimateGas returns EVM usedGas from a simulation.
	EstimateGas(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) (uint64, error)

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

// decodeStorageKey left-pads a quantity-encoded storage slot to 32 bytes, as geth does.
func decodeStorageKey(s string) (common.Hash, error) {
	key := s
	if strings.HasPrefix(key, "0x") || strings.HasPrefix(key, "0X") {
		key = key[2:]
	}
	if len(key)%2 != 0 {
		key = "0" + key
	}
	if len(key) > 2*common.HashLength {
		return common.Hash{}, rpcerr.InvalidParams("storage key too long (want at most 32 bytes): %q", s)
	}
	b, err := hex.DecodeString(key)
	if err != nil {
		return common.Hash{}, rpcerr.InvalidParams("invalid hex in storage key: %q", s)
	}
	return common.BytesToHash(b), nil
}

// eth_getStorageAt
func (api *EthAPI) GetStorageAt(ctx context.Context, addr common.Address, hexSlot string, block rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	logger.Debugf("EthAPI.GetStorageAt() called with addr=%s, slot=%s", addr.Hex(), hexSlot)
	slot, err := decodeStorageKey(hexSlot)
	if err != nil {
		logger.Debugf("EthAPI.GetStorageAt() returning error: %v", err)
		return nil, err
	}
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

	callMsg, err := argsToCallMsg(args)
	if err != nil {
		logger.Debugf("EthAPI.EstimateGas() returning error: %v", err)
		return nil, err
	}
	blockRef := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if block != nil {
		blockRef = *block
	}
	blockNum, err := api.blockNumberOrHashToBlockNumber(ctx, blockRef)
	if err != nil {
		logger.Debugf("EthAPI.EstimateGas() returning error: %v", err)
		return nil, err
	}

	// Simulate via the endorser and return real EVM usedGas.
	// Reverts and other execution failures surface as JSON-RPC errors, same as eth_call.
	gas, err := api.b.EstimateGas(ctx, callMsg, blockNum)
	if err != nil {
		logger.Debugf("EthAPI.EstimateGas() returning error: %v", err)
		return nil, classifyCallError(err)
	}
	u := hexutil.Uint64(gas)
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

// maxFeeHistory bounds eth_feeHistory's response, matching geth's default. Without it an
// arbitrarily large blockCount would size the slices below straight into an OOM.
const maxFeeHistory = 1024

// eth_feeHistory
// blockCount is math.HexOrDecimal64, as in geth, so a decimal or unquoted count is accepted too.
func (api *EthAPI) FeeHistory(ctx context.Context, blockCount gethmath.HexOrDecimal64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	logger.Debugf("EthAPI.FeeHistory() called with blockCount=%d, lastBlock=%v", blockCount, lastBlock)
	if blockCount > maxFeeHistory {
		logger.Debugf("EthAPI.FeeHistory() truncating blockCount %d to %d", blockCount, maxFeeHistory)
		blockCount = maxFeeHistory
	}
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
	query, err := api.filterCriteriaToLogFilter(ctx, crit)
	if err != nil {
		logger.Debugf("EthAPI.GetLogs() returning error: %v", err)
		return nil, err
	}

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

func (api *EthAPI) filterCriteriaToLogFilter(ctx context.Context, crit filters.FilterCriteria) (domain.LogFilter, error) {
	filter := domain.LogFilter{}

	if crit.BlockHash != nil {
		hash := crit.BlockHash.Bytes()
		filter.BlockHash = &hash
	} else {
		from, err := api.resolveLogFilterFromBlock(ctx, crit.FromBlock)
		if err != nil {
			return domain.LogFilter{}, err
		}
		filter.FromBlock = from
		filter.ToBlock = resolveLogFilterToBlock(crit.ToBlock)
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

	return filter, nil
}

func domainLogToTypesLog(l domain.Log) *types.Log {
	topics := make([]common.Hash, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = common.BytesToHash(t)
	}

	return &types.Log{
		Address:        common.BytesToAddress(l.Address),
		Topics:         topics,
		Data:           l.Data,
		BlockNumber:    l.BlockNumber,
		BlockHash:      common.BytesToHash(l.BlockHash),
		BlockTimestamp: uint64(l.Timestamp),
		TxHash:         common.BytesToHash(l.TxHash),
		TxIndex:        uint(l.TxIndex),
		Index:          uint(l.LogIndex),
	}
}

// hexArg type-asserts a call argument, so a non-string (say a JSON number) is invalid params rather than a panic.
func hexArg(field string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", rpcerr.InvalidParams("invalid %s: expected a hex string, got %T", field, v)
	}
	return s, nil
}

// quantityArg decodes an optional quantity-encoded call argument, absent and null alike yielding nil.
func quantityArg(args map[string]any, field string) (*big.Int, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return nil, nil
	}
	s, err := hexArg(field, v)
	if err != nil {
		return nil, err
	}
	n, err := hexutil.DecodeBig(s)
	if err != nil {
		return nil, rpcerr.InvalidParams("invalid %s: %v", field, err)
	}
	return n, nil
}

// addressArg decodes an address strictly, as geth does: common.HexToAddress never fails, silently
// zero-padding a short address and truncating a long one to a different address entirely.
func addressArg(field string, v any) (common.Address, error) {
	s, err := hexArg(field, v)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	if err := addr.UnmarshalText([]byte(s)); err != nil {
		return common.Address{}, rpcerr.InvalidParams("invalid %s: %v", field, err)
	}
	return addr, nil
}

func argsToCallMsg(args map[string]any) (ethereum.CallMsg, error) {
	var msg ethereum.CallMsg

	if v, ok := args["from"]; ok && v != nil {
		from, err := addressArg("from", v)
		if err != nil {
			return msg, err
		}
		msg.From = from
	}

	// "to" may be explicitly null for contract-creation calls.
	if v, ok := args["to"]; ok && v != nil {
		to, err := addressArg("to", v)
		if err != nil {
			return msg, err
		}
		msg.To = &to
	}

	if v, ok := args["gas"]; ok && v != nil {
		s, err := hexArg("gas", v)
		if err != nil {
			return msg, err
		}
		gas, err := hexutil.DecodeUint64(s)
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid gas: %v", err)
		}
		msg.Gas = gas
	}

	gasPrice, err := quantityArg(args, "gasPrice")
	if err != nil {
		return msg, err
	}
	msg.GasPrice = gasPrice

	value, err := quantityArg(args, "value")
	if err != nil {
		return msg, err
	}
	msg.Value = value

	// "input" is the canonical field; "data" is the legacy alias (used by some clients)
	inputKey := "input"
	if _, ok := args["input"]; !ok {
		if _, ok := args["data"]; ok {
			inputKey = "data"
		}
	}
	if v, ok := args[inputKey]; ok && v != nil {
		s, err := hexArg(inputKey, v)
		if err != nil {
			return msg, err
		}
		data, err := hexutil.Decode(s)
		if err != nil {
			return msg, rpcerr.InvalidParams("invalid %s: %v", inputKey, err)
		}
		msg.Data = data
	}

	// EIP-1559 (optional, ignore safely if absent)
	if msg.GasFeeCap, err = quantityArg(args, "maxFeePerGas"); err != nil {
		return msg, err
	}
	if msg.GasTipCap, err = quantityArg(args, "maxPriorityFeePerGas"); err != nil {
		return msg, err
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

// rpcBlockNumberToBigInt converts rpc.BlockNumber to *big.Int for state queries.
// "earliest" resolves to block 0; every other negative sentinel (latest, pending, safe,
// finalized, and any future one) resolves to nil/"latest" — every committed Fabric block
// is final, so finalized == latest is semantically correct here too.
func rpcBlockNumberToBigInt(num rpc.BlockNumber) *big.Int {
	if num == rpc.EarliestBlockNumber {
		return big.NewInt(0)
	}
	if num < 0 {
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

// resolveLogFilterFromBlock resolves the lower bound of an eth_getLogs range.
// An omitted bound defaults to "latest" per the JSON-RPC spec, so it — like "latest",
// "pending", "safe" and "finalized" — resolves to the head block; "earliest" is block 0.
// Resolving to nil would mean genesis and return the whole chain's logs.
func (api *EthAPI) resolveLogFilterFromBlock(ctx context.Context, n *big.Int) (*uint64, error) {
	if n != nil && n.Sign() >= 0 {
		v := n.Uint64()
		return &v, nil
	}
	if n != nil && n.Cmp(big.NewInt(int64(rpc.EarliestBlockNumber))) == 0 {
		zero := uint64(0)
		return &zero, nil
	}
	head, err := api.b.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	return &head, nil
}

// resolveLogFilterToBlock resolves the upper bound of an eth_getLogs range.
// nil (omitted), "latest", "pending", "safe" and "finalized" all leave the bound
// open, which the store reads as the head block; "earliest" resolves to block 0.
func resolveLogFilterToBlock(n *big.Int) *uint64 {
	if n == nil {
		return nil
	}
	if n.Cmp(big.NewInt(int64(rpc.EarliestBlockNumber))) == 0 {
		zero := uint64(0)
		return &zero
	}
	if n.Sign() < 0 {
		return nil
	}
	v := n.Uint64()
	return &v
}
