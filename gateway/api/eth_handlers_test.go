/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

var (
	testAddr       = common.HexToAddress("0x1111111111111111111111111111111111111111")
	testBlockHash  = common.HexToHash("0xabc123")
	testTxHash     = common.HexToHash("0xdeadbeef")
	latestBlockRef = rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	errBoom        = errors.New("boom")
)

// mustBlock returns a fully populated domain.Block so rpcBlock() marshalling
// can convert every []byte to common.Hash without panicking.
func mustBlock(num uint64, hash common.Hash) *domain.Block {
	return &domain.Block{
		BlockNumber: num,
		BlockHash:   hash.Bytes(),
		ParentHash:  common.Hash{}.Bytes(),
		StateRoot:   common.Hash{}.Bytes(),
	}
}

// ---- Chain ----

func TestChainId_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{chainID: big.NewInt(4011)})
	got, err := api.ChainId(context.Background())
	if err != nil {
		t.Fatalf("ChainId err: %v", err)
	}
	if (*big.Int)(got).Cmp(big.NewInt(4011)) != 0 {
		t.Errorf("ChainId = %s, want 4011", (*big.Int)(got))
	}
}

func TestChainId_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{chainIDErr: errBoom})
	if _, err := api.ChainId(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestBlockNumber_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{blockNum: 42})
	got, err := api.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("BlockNumber err: %v", err)
	}
	if got != 42 {
		t.Errorf("BlockNumber = %d, want 42", got)
	}
}

func TestBlockNumber_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{blockNumErr: errBoom})
	if _, err := api.BlockNumber(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// ---- Blocks ----

func TestGetBlockByNumber_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		blockByNumber: map[uint64]*domain.Block{
			7: mustBlock(7, testBlockHash),
		},
	})
	got, err := api.GetBlockByNumber(context.Background(), rpc.BlockNumber(7), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || uint64(got.Number) != 7 {
		t.Fatalf("block = %+v, want number 7", got)
	}
}

func TestGetBlockByNumber_NotFound(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.GetBlockByNumber(context.Background(), rpc.BlockNumber(99), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for not-found, got %+v", got)
	}
}

func TestGetBlockByNumber_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{getBlockErr: errBoom})
	if _, err := api.GetBlockByNumber(context.Background(), rpc.BlockNumber(1), false); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBlockByHash_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		blockByHash: map[common.Hash]*domain.Block{
			testBlockHash: mustBlock(5, testBlockHash),
		},
	})
	got, err := api.GetBlockByHash(context.Background(), testBlockHash, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.Hash != testBlockHash {
		t.Fatalf("block = %+v, want hash %s", got, testBlockHash.Hex())
	}
}

func TestGetBlockByHash_NotFound(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.GetBlockByHash(context.Background(), testBlockHash, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestGetBlockByHash_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{getBlockErr: errBoom})
	if _, err := api.GetBlockByHash(context.Background(), testBlockHash, false); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBlockTransactionCountByHash_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		txCountByHash: map[common.Hash]int64{testBlockHash: 3},
	})
	got, err := api.GetBlockTransactionCountByHash(context.Background(), testBlockHash)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || uint(*got) != 3 {
		t.Errorf("count = %v, want 3", got)
	}
}

func TestGetBlockTransactionCountByHash_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txCountByHashErr: errBoom})
	if _, err := api.GetBlockTransactionCountByHash(context.Background(), testBlockHash); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBlockTransactionCountByNumber_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		txCountByNumber: map[uint64]int64{7: 2},
	})
	got, err := api.GetBlockTransactionCountByNumber(context.Background(), rpc.BlockNumber(7))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || uint(*got) != 2 {
		t.Errorf("count = %v, want 2", got)
	}
}

func TestGetBlockTransactionCountByNumber_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txCountByNumberErr: errBoom})
	if _, err := api.GetBlockTransactionCountByNumber(context.Background(), rpc.BlockNumber(1)); err == nil {
		t.Fatal("expected error")
	}
}

// ---- State ----

func TestGetBalance_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{balance: big.NewInt(1_000_000)})
	got, err := api.GetBalance(context.Background(), testAddr, latestBlockRef)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if (*big.Int)(got).Cmp(big.NewInt(1_000_000)) != 0 {
		t.Errorf("balance = %s, want 1000000", (*big.Int)(got))
	}
}

func TestGetBalance_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{balanceErr: errBoom})
	if _, err := api.GetBalance(context.Background(), testAddr, latestBlockRef); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBalance_BlockNumberByHashError(t *testing.T) {
	api := NewEthAPI(&stubBackend{getBlockErr: errBoom})
	if _, err := api.GetBalance(context.Background(), testAddr, rpc.BlockNumberOrHashWithHash(testBlockHash, false)); err == nil {
		t.Fatal("expected error from block-hash resolution")
	}
}

func TestGetCode_Happy(t *testing.T) {
	want := []byte{0x60, 0x60, 0x60, 0x40}
	api := NewEthAPI(&stubBackend{code: want})
	got, err := api.GetCode(context.Background(), testAddr, latestBlockRef)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("code = %x, want %x", got, want)
	}
}

func TestGetCode_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{codeErr: errBoom})
	if _, err := api.GetCode(context.Background(), testAddr, latestBlockRef); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetStorageAt_Happy(t *testing.T) {
	want := []byte{0xde, 0xad, 0xbe, 0xef}
	api := NewEthAPI(&stubBackend{storage: want})
	got, err := api.GetStorageAt(context.Background(), testAddr, common.HexToHash("0x1"), latestBlockRef)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("storage = %x, want %x", got, want)
	}
}

func TestGetStorageAt_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{storageErr: errBoom})
	if _, err := api.GetStorageAt(context.Background(), testAddr, common.HexToHash("0x1"), latestBlockRef); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTransactionCount_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{nonce: 17})
	got, err := api.GetTransactionCount(context.Background(), testAddr, latestBlockRef)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || uint64(*got) != 17 {
		t.Errorf("nonce = %v, want 17", got)
	}
}

func TestGetTransactionCount_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{nonceErr: errBoom})
	if _, err := api.GetTransactionCount(context.Background(), testAddr, latestBlockRef); err == nil {
		t.Fatal("expected error")
	}
}

// ---- Transactions ----

// signedRawTx returns an EIP-155 signed transaction and its raw bytes.
func signedRawTx(t *testing.T) (*types.Transaction, []byte) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tx := types.NewTransaction(0, testAddr, big.NewInt(0), 21_000, big.NewInt(1), nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(4011)), key)
	if err != nil {
		t.Fatalf("SignTx: %v", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return signed, raw
}

func TestSendRawTransaction_Happy(t *testing.T) {
	signed, raw := signedRawTx(t)
	backend := &stubBackend{}
	api := NewEthAPI(backend)

	hash, err := api.SendRawTransaction(context.Background(), raw)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if hash != signed.Hash() {
		t.Errorf("hash = %s, want %s", hash.Hex(), signed.Hash().Hex())
	}
	if backend.lastSent == nil || backend.lastSent.Hash() != signed.Hash() {
		t.Errorf("backend didn't receive the tx")
	}
}

func TestGetTransactionByHash_Happy(t *testing.T) {
	_, raw := signedRawTx(t)
	api := NewEthAPI(&stubBackend{
		txByHash: map[common.Hash]*domain.Transaction{
			testTxHash: {TxHash: testTxHash.Bytes(), RawTx: raw, FromAddress: testAddr.Bytes()},
		},
	})
	got, err := api.GetTransactionByHash(context.Background(), testTxHash)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.From != testAddr {
		t.Fatalf("tx = %+v, want from %s", got, testAddr.Hex())
	}
}

func TestGetTransactionByHash_NotFound(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.GetTransactionByHash(context.Background(), testTxHash)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestGetTransactionByHash_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txByHashErr: errBoom})
	if _, err := api.GetTransactionByHash(context.Background(), testTxHash); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTransactionByBlockHashAndIndex_Happy(t *testing.T) {
	_, raw := signedRawTx(t)
	api := NewEthAPI(&stubBackend{
		txByBlockHashIdx: map[common.Hash]map[int64]*domain.Transaction{
			testBlockHash: {0: {TxHash: testTxHash.Bytes(), RawTx: raw, FromAddress: testAddr.Bytes()}},
		},
	})
	got, err := api.GetTransactionByBlockHashAndIndex(context.Background(), testBlockHash, hexutil.Uint(0))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.From != testAddr {
		t.Fatalf("tx = %+v", got)
	}
}

func TestGetTransactionByBlockHashAndIndex_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txByBlockHashErr: errBoom})
	if _, err := api.GetTransactionByBlockHashAndIndex(context.Background(), testBlockHash, hexutil.Uint(0)); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTransactionByBlockNumberAndIndex_Happy(t *testing.T) {
	_, raw := signedRawTx(t)
	api := NewEthAPI(&stubBackend{
		txByBlockNumIdx: map[uint64]map[int64]*domain.Transaction{
			5: {0: {TxHash: testTxHash.Bytes(), RawTx: raw, FromAddress: testAddr.Bytes()}},
		},
	})
	got, err := api.GetTransactionByBlockNumberAndIndex(context.Background(), rpc.BlockNumber(5), hexutil.Uint(0))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.From != testAddr {
		t.Fatalf("tx = %+v", got)
	}
}

func TestGetTransactionByBlockNumberAndIndex_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txByBlockNumErr: errBoom})
	if _, err := api.GetTransactionByBlockNumberAndIndex(context.Background(), rpc.BlockNumber(5), hexutil.Uint(0)); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTransactionReceipt_Confirmed(t *testing.T) {
	_, raw := signedRawTx(t)
	api := NewEthAPI(&stubBackend{
		txByHash: map[common.Hash]*domain.Transaction{
			testTxHash: {
				TxHash:      testTxHash.Bytes(),
				RawTx:       raw,
				FromAddress: testAddr.Bytes(),
				ToAddress:   testAddr.Bytes(),
				BlockHash:   testBlockHash.Bytes(),
				BlockNumber: 5,
				Status:      1,
			},
		},
	})
	got, err := api.GetTransactionReceipt(context.Background(), testTxHash)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.From != testAddr {
		t.Fatalf("receipt = %+v", got)
	}
}

func TestGetTransactionReceipt_PendingReturnsNil(t *testing.T) {
	_, raw := signedRawTx(t)
	// Pending tx: BlockHash == nil -> receipt() returns nil.
	api := NewEthAPI(&stubBackend{
		txByHash: map[common.Hash]*domain.Transaction{
			testTxHash: {TxHash: testTxHash.Bytes(), RawTx: raw, FromAddress: testAddr.Bytes()},
		},
	})
	got, err := api.GetTransactionReceipt(context.Background(), testTxHash)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("want nil receipt for pending tx, got %+v", got)
	}
}

func TestGetTransactionReceipt_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{txByHashErr: errBoom})
	if _, err := api.GetTransactionReceipt(context.Background(), testTxHash); err == nil {
		t.Fatal("expected error")
	}
}

// ---- Fees ----

func TestEstimateGas_HappyReturnsConstant(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.EstimateGas(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || uint64(*got) != 10_000_000 {
		t.Errorf("gas = %v, want 10000000", got)
	}
}

func TestEstimateGas_CallErrorPropagates(t *testing.T) {
	api := NewEthAPI(&stubBackend{callErr: errBoom})
	if _, err := api.EstimateGas(context.Background(), map[string]any{}, nil); err == nil {
		t.Fatal("expected error from underlying Call")
	}
}

func TestGasPrice_ReturnsZero(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.GasPrice(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if (*big.Int)(got).Sign() != 0 {
		t.Errorf("gasPrice = %s, want 0", (*big.Int)(got))
	}
}

func TestMaxPriorityFeePerGas_ReturnsZero(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.MaxPriorityFeePerGas(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if (*big.Int)(got).Sign() != 0 {
		t.Errorf("maxPriorityFee = %s, want 0", (*big.Int)(got))
	}
}

func TestFeeHistory_ShapesArrays(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.FeeHistory(context.Background(), hexutil.Uint(3), rpc.LatestBlockNumber, []float64{25, 50, 75})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil {
		t.Fatal("nil result")
	}
	if len(got.BaseFee) != 4 { // blockCount+1
		t.Errorf("BaseFee len = %d, want 4", len(got.BaseFee))
	}
	if len(got.GasUsedRatio) != 3 {
		t.Errorf("GasUsedRatio len = %d, want 3", len(got.GasUsedRatio))
	}
	if len(got.Reward) != 3 || len(got.Reward[0]) != 3 {
		t.Errorf("Reward shape = [%d][%d], want [3][3]", len(got.Reward), func() int {
			if len(got.Reward) > 0 {
				return len(got.Reward[0])
			}
			return 0
		}())
	}
}

func TestFeeHistory_NoPercentiles(t *testing.T) {
	api := NewEthAPI(&stubBackend{})
	got, err := api.FeeHistory(context.Background(), hexutil.Uint(2), rpc.LatestBlockNumber, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Reward) != 2 || len(got.Reward[0]) != 0 {
		t.Errorf("Reward shape = [%d][?], want [2][0]", len(got.Reward))
	}
}

// ---- Logs ----

func TestGetLogs_Happy(t *testing.T) {
	api := NewEthAPI(&stubBackend{
		logs: []domain.Log{
			{
				BlockNumber: 5,
				BlockHash:   testBlockHash.Bytes(),
				TxHash:      testTxHash.Bytes(),
				Address:     testAddr.Bytes(),
				Data:        []byte{0xde, 0xad},
				Timestamp:   1700000000,
			},
		},
	})
	got, err := api.GetLogs(context.Background(), filters.FilterCriteria{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].BlockNumber != 5 || got[0].BlockTimestamp != 1700000000 {
		t.Errorf("logs = %+v", got)
	}
}

func TestGetLogs_BackendError(t *testing.T) {
	api := NewEthAPI(&stubBackend{logsErr: errBoom})
	if _, err := api.GetLogs(context.Background(), filters.FilterCriteria{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetLogs_FilterTransformationBlockRange(t *testing.T) {
	backend := &stubBackend{}
	api := NewEthAPI(backend)
	from, to := big.NewInt(10), big.NewInt(20)
	crit := filters.FilterCriteria{
		FromBlock: from,
		ToBlock:   to,
		Addresses: []common.Address{testAddr},
		Topics:    [][]common.Hash{{common.HexToHash("0xaa"), common.HexToHash("0xbb")}, {common.HexToHash("0xcc")}},
	}
	if _, err := api.GetLogs(context.Background(), crit); err != nil {
		t.Fatalf("err: %v", err)
	}
	f := backend.lastFilter
	if f.FromBlock == nil || *f.FromBlock != 10 {
		t.Errorf("FromBlock = %v, want 10", f.FromBlock)
	}
	if f.ToBlock == nil || *f.ToBlock != 20 {
		t.Errorf("ToBlock = %v, want 20", f.ToBlock)
	}
	if len(f.Addresses) != 1 || !bytes.Equal(f.Addresses[0], testAddr.Bytes()) {
		t.Errorf("Addresses = %v", f.Addresses)
	}
	if len(f.Topics) != 2 || len(f.Topics[0]) != 2 || len(f.Topics[1]) != 1 {
		t.Errorf("Topics shape wrong: %v", f.Topics)
	}
}

func TestGetLogs_FilterTransformationByBlockHash(t *testing.T) {
	backend := &stubBackend{}
	api := NewEthAPI(backend)
	crit := filters.FilterCriteria{BlockHash: &testBlockHash}
	if _, err := api.GetLogs(context.Background(), crit); err != nil {
		t.Fatalf("err: %v", err)
	}
	if backend.lastFilter.BlockHash == nil {
		t.Fatal("BlockHash not captured")
	}
	if !bytes.Equal(*backend.lastFilter.BlockHash, testBlockHash.Bytes()) {
		t.Errorf("BlockHash mismatch")
	}
	if backend.lastFilter.FromBlock != nil || backend.lastFilter.ToBlock != nil {
		t.Errorf("From/To should be nil when BlockHash present")
	}
}
