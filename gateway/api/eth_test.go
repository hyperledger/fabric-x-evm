/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestRpcBlockNumberToBigInt(t *testing.T) {
	tests := []struct {
		name string
		num  rpc.BlockNumber
		want *big.Int
	}{
		{"pending", rpc.PendingBlockNumber, nil},
		{"latest", rpc.LatestBlockNumber, nil},
		{"earliest", rpc.EarliestBlockNumber, big.NewInt(0)},
		{"safe", rpc.SafeBlockNumber, nil},
		{"finalized", rpc.FinalizedBlockNumber, nil},
		{"zero", 0, big.NewInt(0)},
		{"positive", 100, big.NewInt(100)},
		{"unrecognized negative sentinel", -10, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rpcBlockNumberToBigInt(tt.num)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("rpcBlockNumberToBigInt() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && got.Cmp(tt.want) != 0 {
				t.Errorf("rpcBlockNumberToBigInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockNumberToUint64(t *testing.T) {
	tests := []struct {
		name string
		num  rpc.BlockNumber
		want uint64
	}{
		{"zero", 0, 0},
		{"positive", 100, 100},
		{"negative", -1, math.MaxUint64},
		{"large negative", -100, math.MaxUint64},
		{"pending", rpc.PendingBlockNumber, math.MaxUint64},
		{"latest", rpc.LatestBlockNumber, math.MaxUint64},
		{"earliest", rpc.EarliestBlockNumber, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blockNumberToUint64(tt.num)
			if got != tt.want {
				t.Errorf("blockNumberToUint64() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFilterCriteriaToLogFilter_LatestToBlock reproduces issue #192: eth_getLogs
// with toBlock: "latest" must leave the upper bound unset, not resolve it to
// block 1. It unmarshals real JSON-RPC input so go-ethereum's own sentinel
// encoding (rpc.LatestBlockNumber, a negative constant) is exercised.
func TestFilterCriteriaToLogFilter_LatestToBlock(t *testing.T) {
	var crit filters.FilterCriteria
	if err := json.Unmarshal([]byte(`{"fromBlock":"0x1","toBlock":"latest"}`), &crit); err != nil {
		t.Fatalf("unmarshal FilterCriteria: %v", err)
	}

	got := filterCriteriaToLogFilter(crit)

	if got.FromBlock == nil || *got.FromBlock != 1 {
		t.Errorf("FromBlock = %v, want 1", got.FromBlock)
	}
	if got.ToBlock != nil {
		t.Errorf("ToBlock = %v, want nil (unbounded/latest)", *got.ToBlock)
	}
}

func TestFilterCriteriaToLogFilter_BlockBounds(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantFrom *uint64
		wantTo   *uint64
	}{
		{"omitted bounds", `{}`, nil, nil},
		{"explicit numbers", `{"fromBlock":"0x2","toBlock":"0x5"}`, new(uint64(2)), new(uint64(5))},
		{"earliest from, literal 0x0 to", `{"fromBlock":"earliest","toBlock":"0x0"}`, new(uint64(0)), new(uint64(0))},
		{"latest to", `{"fromBlock":"0x1","toBlock":"latest"}`, new(uint64(1)), nil},
		{"pending to", `{"toBlock":"pending"}`, nil, nil},
		{"latest from", `{"fromBlock":"latest"}`, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var crit filters.FilterCriteria
			if err := json.Unmarshal([]byte(tt.json), &crit); err != nil {
				t.Fatalf("unmarshal FilterCriteria: %v", err)
			}

			got := filterCriteriaToLogFilter(crit)

			if (got.FromBlock == nil) != (tt.wantFrom == nil) || (got.FromBlock != nil && *got.FromBlock != *tt.wantFrom) {
				t.Errorf("FromBlock = %v, want %v", got.FromBlock, tt.wantFrom)
			}
			if (got.ToBlock == nil) != (tt.wantTo == nil) || (got.ToBlock != nil && *got.ToBlock != *tt.wantTo) {
				t.Errorf("ToBlock = %v, want %v", got.ToBlock, tt.wantTo)
			}
		})
	}
}

func TestBlockNumberOrHashToBlockNumber(t *testing.T) {
	api := NewEthAPI(&stubBackend{})

	tests := []struct {
		name      string
		numOrHash rpc.BlockNumberOrHash
		want      *big.Int
	}{
		{"latest", rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), nil},
		{"pending", rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber), nil},
		{"earliest", rpc.BlockNumberOrHashWithNumber(rpc.EarliestBlockNumber), big.NewInt(0)},
		{"zero", rpc.BlockNumberOrHashWithNumber(0), big.NewInt(0)},
		{"positive", rpc.BlockNumberOrHashWithNumber(100), big.NewInt(100)},
		{"unrecognized negative sentinel", rpc.BlockNumberOrHashWithNumber(-10), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := api.blockNumberOrHashToBlockNumber(context.Background(), tt.numOrHash)
			if err != nil {
				t.Fatalf("blockNumberOrHashToBlockNumber() error = %v", err)
			}
			if (got == nil) != (tt.want == nil) {
				t.Errorf("blockNumberOrHashToBlockNumber() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && got.Cmp(tt.want) != 0 {
				t.Errorf("blockNumberOrHashToBlockNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockNumberOrHashToBlockNumber_Hash(t *testing.T) {
	hash := common.HexToHash("0x1234")
	api := NewEthAPI(&stubBackend{
		blockByHash: map[common.Hash]*domain.Block{
			hash: {BlockNumber: 42},
		},
	})

	got, err := api.blockNumberOrHashToBlockNumber(context.Background(), rpc.BlockNumberOrHashWithHash(hash, false))
	if err != nil {
		t.Fatalf("blockNumberOrHashToBlockNumber() error = %v", err)
	}
	if got == nil || got.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("blockNumberOrHashToBlockNumber() = %v, want 42", got)
	}
}

func TestBlockNumberOrHashToBlockNumber_HashNotFound(t *testing.T) {
	hash := common.HexToHash("0xabcd")
	api := NewEthAPI(&stubBackend{})

	_, err := api.blockNumberOrHashToBlockNumber(context.Background(), rpc.BlockNumberOrHashWithHash(hash, false))
	if !errors.Is(err, ethereum.NotFound) {
		t.Fatalf("blockNumberOrHashToBlockNumber() error = %v, want %v", err, ethereum.NotFound)
	}
}

func TestBlockNumberOrHashToBlockNumber_HashError(t *testing.T) {
	hash := common.HexToHash("0x9999")
	api := NewEthAPI(&stubBackend{
		getBlockErr: errors.New("boom"),
	})

	_, err := api.blockNumberOrHashToBlockNumber(context.Background(), rpc.BlockNumberOrHashWithHash(hash, false))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRPCReceiptMarshalJSON(t *testing.T) {
	fromAddr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	toAddr := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	tests := []struct {
		name        string
		receipt     *rpcReceipt
		checkTo     func(t *testing.T, m map[string]any)
		checkFields func(t *testing.T, m map[string]any)
	}{
		{
			name: "with to address",
			receipt: &rpcReceipt{
				Receipt: types.Receipt{
					Status:      1,
					BlockNumber: big.NewInt(42),
				},
				From: fromAddr,
				To:   &toAddr,
			},
			checkTo: func(t *testing.T, m map[string]any) {
				if m["to"] != toAddr.Hex() {
					t.Errorf("to = %v, want %v", m["to"], toAddr.Hex())
				}
			},
			checkFields: func(t *testing.T, m map[string]any) {
				if m["from"] != fromAddr.Hex() {
					t.Errorf("from = %v, want %v", m["from"], fromAddr.Hex())
				}
				if m["status"] == nil {
					t.Error("status field not preserved")
				}
			},
		},
		{
			name: "nil to address",
			receipt: &rpcReceipt{
				Receipt: types.Receipt{
					Status:      1,
					BlockNumber: big.NewInt(100),
				},
				From: fromAddr,
				To:   nil,
			},
			checkTo: func(t *testing.T, m map[string]any) {
				if m["to"] != nil {
					t.Errorf("to = %v, want nil", m["to"])
				}
			},
			checkFields: func(t *testing.T, m map[string]any) {
				if m["from"] != fromAddr.Hex() {
					t.Errorf("from = %v, want %v", m["from"], fromAddr.Hex())
				}
				if m["status"] == nil {
					t.Error("status field not preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.receipt)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			tt.checkTo(t, m)
			tt.checkFields(t, m)
		})
	}
}

func TestRPCBlockMarshalJSON(t *testing.T) {
	b := &domain.Block{
		BlockNumber: 7,
		BlockHash:   common.HexToHash("0xaa").Bytes(),
		ParentHash:  common.HexToHash("0xbb").Bytes(),
		StateRoot:   common.HexToHash("0xcc").Bytes(),
		Timestamp:   1700000000,
	}

	data, err := json.Marshal(rpcBlock(b, false))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	bloom, ok := m["logsBloom"].(string)
	if !ok {
		t.Fatalf("logsBloom missing or not a string: %v", m["logsBloom"])
	}
	// Empty bloom is 256 bytes of zeros, hex-encoded as 0x + 512 zero chars.
	wantBloom := "0x" + strings.Repeat("0", 512)
	if bloom != wantBloom {
		t.Errorf("logsBloom = %q, want %q", bloom, wantBloom)
	}

	extra, ok := m["extraData"].(string)
	if !ok {
		t.Fatalf("extraData missing or not a string: %v", m["extraData"])
	}
	if extra != "0x" {
		t.Errorf("extraData = %q, want %q", extra, "0x")
	}
}

type stubBackend struct {
	// Chain
	chainID     *big.Int
	chainIDErr  error
	blockNum    uint64
	blockNumErr error

	// Blocks
	blockByHash   map[common.Hash]*domain.Block
	blockByNumber map[uint64]*domain.Block
	getBlockErr   error // returned from GetBlockBy*/BlockNumberByHash

	// Block tx counts
	txCountByHash      map[common.Hash]int64
	txCountByHashErr   error
	txCountByNumber    map[uint64]int64
	txCountByNumberErr error

	// State
	balance    *big.Int
	balanceErr error
	storage    []byte
	storageErr error
	code       []byte
	codeErr    error
	nonce      uint64
	nonceErr   error

	// Send / Call
	sendErr  error
	lastSent *types.Transaction
	callRet  []byte
	callErr  error

	// Tx reads
	txByHash         map[common.Hash]*domain.Transaction
	txByHashErr      error
	txByBlockHashIdx map[common.Hash]map[int64]*domain.Transaction
	txByBlockHashErr error
	txByBlockNumIdx  map[uint64]map[int64]*domain.Transaction
	txByBlockNumErr  error

	// Logs
	logs       []domain.Log
	logsErr    error
	lastFilter domain.LogFilter // captured on GetLogs for assertion
}

func (s *stubBackend) ChainID(ctx context.Context) (*big.Int, error) {
	if s.chainIDErr != nil {
		return nil, s.chainIDErr
	}
	if s.chainID != nil {
		return s.chainID, nil
	}
	return big.NewInt(1), nil
}
func (s *stubBackend) BlockNumber(ctx context.Context) (uint64, error) {
	return s.blockNum, s.blockNumErr
}
func (s *stubBackend) GetBlockByNumber(ctx context.Context, num uint64, full bool) (*domain.Block, error) {
	if s.getBlockErr != nil {
		return nil, s.getBlockErr
	}
	if s.blockByNumber == nil {
		return nil, nil
	}
	return s.blockByNumber[num], nil
}
func (s *stubBackend) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (*domain.Block, error) {
	if s.getBlockErr != nil {
		return nil, s.getBlockErr
	}
	if s.blockByHash == nil {
		return nil, nil
	}
	return s.blockByHash[hash], nil
}
func (s *stubBackend) BlockNumberByHash(ctx context.Context, hash common.Hash) (*uint64, error) {
	if s.getBlockErr != nil {
		return nil, s.getBlockErr
	}
	if s.blockByHash == nil {
		return nil, nil
	}
	blk := s.blockByHash[hash]
	if blk == nil {
		return nil, nil
	}
	num := blk.BlockNumber
	return &num, nil
}
func (s *stubBackend) GetBlockTxCountByHash(ctx context.Context, hash common.Hash) (int64, error) {
	if s.txCountByHashErr != nil {
		return 0, s.txCountByHashErr
	}
	return s.txCountByHash[hash], nil
}
func (s *stubBackend) GetBlockTxCountByNumber(ctx context.Context, num uint64) (int64, error) {
	if s.txCountByNumberErr != nil {
		return 0, s.txCountByNumberErr
	}
	return s.txCountByNumber[num], nil
}
func (s *stubBackend) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	if s.balanceErr != nil {
		return nil, s.balanceErr
	}
	if s.balance != nil {
		return s.balance, nil
	}
	return big.NewInt(0), nil
}
func (s *stubBackend) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	return s.storage, s.storageErr
}
func (s *stubBackend) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return s.code, s.codeErr
}
func (s *stubBackend) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return s.nonce, s.nonceErr
}
func (s *stubBackend) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	s.lastSent = tx
	return s.sendErr
}
func (s *stubBackend) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return s.callRet, s.callErr
}
func (s *stubBackend) TransactionByHash(ctx context.Context, hash common.Hash) (*domain.Transaction, error) {
	if s.txByHashErr != nil {
		return nil, s.txByHashErr
	}
	if s.txByHash == nil {
		return nil, nil
	}
	return s.txByHash[hash], nil
}
func (s *stubBackend) GetTransactionByBlockHashAndIndex(ctx context.Context, hash common.Hash, idx int64) (*domain.Transaction, error) {
	if s.txByBlockHashErr != nil {
		return nil, s.txByBlockHashErr
	}
	if s.txByBlockHashIdx == nil {
		return nil, nil
	}
	return s.txByBlockHashIdx[hash][idx], nil
}
func (s *stubBackend) GetTransactionByBlockNumberAndIndex(ctx context.Context, num uint64, idx int64) (*domain.Transaction, error) {
	if s.txByBlockNumErr != nil {
		return nil, s.txByBlockNumErr
	}
	if s.txByBlockNumIdx == nil {
		return nil, nil
	}
	return s.txByBlockNumIdx[num][idx], nil
}
func (s *stubBackend) GetLogs(ctx context.Context, query domain.LogFilter) ([]domain.Log, error) {
	s.lastFilter = query
	return s.logs, s.logsErr
}

var (
	_ Backend = (*stubBackend)(nil)
)

func TestSendRawTransaction_InvalidPayloadIsInvalidParams(t *testing.T) {
	api := NewEthAPI(&stubBackend{})

	_, err := api.SendRawTransaction(context.Background(), []byte{0xff, 0xff})

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected rpc.Error, got %T (%v)", err, err)
	}
	if rpcErr.ErrorCode() != -32602 {
		t.Errorf("code = %d, want -32602 (InvalidParams)", rpcErr.ErrorCode())
	}
}

func TestArgsToCallMsg_BadHexFieldsAreInvalidParams(t *testing.T) {
	cases := []struct {
		name  string
		field string
		bad   string
	}{
		{"gas", "gas", "not-hex"},
		{"gasPrice", "gasPrice", "not-hex"},
		{"value", "value", "not-hex"},
		{"input", "input", "not-hex"},
		{"data", "data", "not-hex"},
		{"maxFeePerGas", "maxFeePerGas", "not-hex"},
		{"maxPriorityFeePerGas", "maxPriorityFeePerGas", "not-hex"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := argsToCallMsg(map[string]any{c.field: c.bad})

			var rpcErr rpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("expected rpc.Error for bad %s, got %T (%v)", c.field, err, err)
			}
			if rpcErr.ErrorCode() != -32602 {
				t.Errorf("code = %d, want -32602 (InvalidParams)", rpcErr.ErrorCode())
			}
		})
	}
}

func TestCall_RevertSurfacesAsExecutionReverted(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	api := NewEthAPI(&stubBackend{
		callErr: &domain.RevertError{
			Reason: "execution reverted: out of stock",
			Data:   payload,
		},
	})

	_, err := api.Call(context.Background(), map[string]any{}, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected rpc.Error, got %T (%v)", err, err)
	}
	if rpcErr.ErrorCode() != -32000 {
		t.Errorf("code = %d, want -32000 (ExecutionReverted)", rpcErr.ErrorCode())
	}
	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		t.Fatalf("revert must satisfy rpc.DataError")
	}
	if dataErr.ErrorData() != "0x08c379a0deadbeef" {
		t.Errorf("ErrorData() = %v, want 0x08c379a0deadbeef", dataErr.ErrorData())
	}
}

func TestCall_NonRevertBackendErrorIsInternal(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		callErr: errors.New("endorser unreachable"),
	})

	_, err := api.Call(context.Background(), map[string]any{}, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))

	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected rpc.Error, got %T (%v)", err, err)
	}
	if rpcErr.ErrorCode() != -32603 {
		t.Errorf("code = %d, want -32603 (Internal)", rpcErr.ErrorCode())
	}
}

func TestDomainLogToTypesLog_SetsBlockTimestamp(t *testing.T) {
	got := domainLogToTypesLog(domain.Log{
		Timestamp: 1234,
		BlockHash: make([]byte, 32),
		TxHash:    make([]byte, 32),
		Address:   make([]byte, 20),
	})
	if got.BlockTimestamp != 1234 {
		t.Errorf("BlockTimestamp = %d, want 1234", got.BlockTimestamp)
	}
}
