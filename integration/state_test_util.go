/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

This file contains code adapted from go-ethereum (https://github.com/ethereum/go-ethereum)
which is licensed under the GNU Lesser General Public License v3.0.
*/

package integration

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
)

// StateTest checks transaction processing without block context.
type StateTest struct {
	json stJSON
}

// StateSubtest selects a specific configuration of a General State Test.
type StateSubtest struct {
	Fork  string
	Index int
}

func (t *StateTest) UnmarshalJSON(in []byte) error {
	return json.Unmarshal(in, &t.json)
}

type stJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  types.GenesisAlloc       `json:"pre"`
	Tx   stTransaction            `json:"transaction"`
	Out  hexutil.Bytes            `json:"out"`
	Post map[string][]stPostState `json:"post"`
}

type stPostState struct {
	Root            common.UnprefixedHash `json:"hash"`
	Logs            common.UnprefixedHash `json:"logs"`
	State           types.GenesisAlloc    `json:"state"`
	TxBytes         hexutil.Bytes         `json:"txbytes"`
	ExpectException string                `json:"expectException"`
	Indexes         struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	}
}

type stEnv struct {
	Coinbase      common.Address `json:"currentCoinbase"`
	Difficulty    *big.Int       `json:"currentDifficulty"`
	Random        *big.Int       `json:"currentRandom"`
	GasLimit      uint64         `json:"currentGasLimit"`
	Number        uint64         `json:"currentNumber"`
	Timestamp     uint64         `json:"currentTimestamp"`
	BaseFee       *big.Int       `json:"currentBaseFee"`
	ExcessBlobGas *uint64        `json:"currentExcessBlobGas"`
}

func (s *stEnv) UnmarshalJSON(input []byte) error {
	type stEnv struct {
		Coinbase      *common.UnprefixedAddress `json:"currentCoinbase"`
		Difficulty    *math.HexOrDecimal256     `json:"currentDifficulty"`
		Random        *math.HexOrDecimal256     `json:"currentRandom"`
		GasLimit      *math.HexOrDecimal64      `json:"currentGasLimit"`
		Number        *math.HexOrDecimal64      `json:"currentNumber"`
		Timestamp     *math.HexOrDecimal64      `json:"currentTimestamp"`
		BaseFee       *math.HexOrDecimal256     `json:"currentBaseFee"`
		ExcessBlobGas *math.HexOrDecimal64      `json:"currentExcessBlobGas"`
	}
	var dec stEnv
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Coinbase != nil {
		s.Coinbase = common.Address(*dec.Coinbase)
	}
	if dec.Difficulty != nil {
		s.Difficulty = (*big.Int)(dec.Difficulty)
	}
	if dec.Random != nil {
		s.Random = (*big.Int)(dec.Random)
	}
	if dec.GasLimit != nil {
		s.GasLimit = uint64(*dec.GasLimit)
	}
	if dec.Number != nil {
		s.Number = uint64(*dec.Number)
	}
	if dec.Timestamp != nil {
		s.Timestamp = uint64(*dec.Timestamp)
	}
	if dec.BaseFee != nil {
		s.BaseFee = (*big.Int)(dec.BaseFee)
	}
	if dec.ExcessBlobGas != nil {
		val := uint64(*dec.ExcessBlobGas)
		s.ExcessBlobGas = &val
	}
	return nil
}

type stTransaction struct {
	GasPrice             *big.Int            `json:"gasPrice"`
	MaxFeePerGas         *big.Int            `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *big.Int            `json:"maxPriorityFeePerGas"`
	Nonce                uint64              `json:"nonce"`
	To                   string              `json:"to"`
	Data                 []string            `json:"data"`
	AccessLists          []*types.AccessList `json:"accessLists,omitempty"`
	GasLimit             []uint64            `json:"gasLimit"`
	Value                []string            `json:"value"`
	PrivateKey           []byte              `json:"secretKey"`
	Sender               *common.Address     `json:"sender"`
	BlobVersionedHashes  []common.Hash       `json:"blobVersionedHashes,omitempty"`
	BlobGasFeeCap        *big.Int            `json:"maxFeePerBlobGas,omitempty"`
	AuthorizationList    []*stAuthorization  `json:"authorizationList,omitempty"`
}

type stAuthorization struct {
	ChainID *big.Int       `json:"chainId"`
	Address common.Address `json:"address"`
	Nonce   uint64         `json:"nonce"`
	V       uint8          `json:"v"`
	R       *big.Int       `json:"r"`
	S       *big.Int       `json:"s"`
}

func (tx *stTransaction) UnmarshalJSON(data []byte) error {
	type stTransactionMarshaling struct {
		GasPrice             *math.HexOrDecimal256
		MaxFeePerGas         *math.HexOrDecimal256
		MaxPriorityFeePerGas *math.HexOrDecimal256
		Nonce                math.HexOrDecimal64
		To                   string
		Data                 []string
		AccessLists          []*types.AccessList
		GasLimit             []math.HexOrDecimal64
		Value                []string
		PrivateKey           hexutil.Bytes `json:"secretKey"`
		Sender               *common.Address
		BlobVersionedHashes  []common.Hash
		BlobGasFeeCap        *math.HexOrDecimal256 `json:"maxFeePerBlobGas"`
		AuthorizationList    []*struct {
			ChainID *math.HexOrDecimal256 `json:"chainId"`
			Address common.Address        `json:"address"`
			Nonce   math.HexOrDecimal64   `json:"nonce"`
			V       math.HexOrDecimal64   `json:"v"`
			R       *math.HexOrDecimal256 `json:"r"`
			S       *math.HexOrDecimal256 `json:"s"`
		} `json:"authorizationList"`
	}
	var dec stTransactionMarshaling
	if err := json.Unmarshal(data, &dec); err != nil {
		return err
	}
	if dec.GasPrice != nil {
		tx.GasPrice = (*big.Int)(dec.GasPrice)
	}
	if dec.MaxFeePerGas != nil {
		tx.MaxFeePerGas = (*big.Int)(dec.MaxFeePerGas)
	}
	if dec.MaxPriorityFeePerGas != nil {
		tx.MaxPriorityFeePerGas = (*big.Int)(dec.MaxPriorityFeePerGas)
	}
	tx.Nonce = uint64(dec.Nonce)
	tx.To = dec.To
	tx.Data = dec.Data
	tx.AccessLists = dec.AccessLists
	tx.GasLimit = make([]uint64, len(dec.GasLimit))
	for i, g := range dec.GasLimit {
		tx.GasLimit[i] = uint64(g)
	}
	tx.Value = dec.Value
	tx.PrivateKey = dec.PrivateKey
	tx.Sender = dec.Sender
	tx.BlobVersionedHashes = dec.BlobVersionedHashes
	if dec.BlobGasFeeCap != nil {
		tx.BlobGasFeeCap = (*big.Int)(dec.BlobGasFeeCap)
	}
	if dec.AuthorizationList != nil {
		tx.AuthorizationList = make([]*stAuthorization, len(dec.AuthorizationList))
		for i, auth := range dec.AuthorizationList {
			tx.AuthorizationList[i] = &stAuthorization{
				ChainID: (*big.Int)(auth.ChainID),
				Address: auth.Address,
				Nonce:   uint64(auth.Nonce),
				V:       uint8(auth.V),
				R:       (*big.Int)(auth.R),
				S:       (*big.Int)(auth.S),
			}
		}
	}
	return nil
}

// Subtests returns all valid subtests of the test.
func (t *StateTest) Subtests() []StateSubtest {
	var sub []StateSubtest
	for fork, pss := range t.json.Post {
		for i := range pss {
			sub = append(sub, StateSubtest{fork, i})
		}
	}
	return sub
}

// prepareTestEnvironment returns what a subtest executes in: the chain config for
// fork, the genesis block, and the EVM block context built from the fixture's env.
// postStateIndex selects the subtest's post-state, whose transaction is checked
// for a valid sender and blob count.
func (t *StateTest) prepareTestEnvironment(fork string, postStateIndex int) (
	config *params.ChainConfig,
	block *types.Block,
	context vm.BlockContext,
	err error,
) {
	config, _, err = tests.GetChainConfig(fork)
	if err != nil {
		return nil, nil, vm.BlockContext{}, tests.UnsupportedForkError{Name: fork}
	}

	block = t.genesis(config).ToBlock()

	var baseFee *big.Int
	if config.IsLondon(new(big.Int)) {
		baseFee = t.json.Env.BaseFee
		if baseFee == nil {
			baseFee = big.NewInt(0x0a)
		}
	}
	post := t.json.Post[fork][postStateIndex]
	msg, err := t.json.Tx.toMessage(post, baseFee)
	if err != nil {
		return nil, nil, vm.BlockContext{}, err
	}

	if config.IsCancun(new(big.Int), block.Time()) {
		if len(msg.BlobHashes) > eip4844.MaxBlobsPerBlock(config, block.Time()) {
			return nil, nil, vm.BlockContext{}, errors.New("blob gas exceeds maximum")
		}
	}

	if len(post.TxBytes) != 0 {
		var ttx types.Transaction
		err := ttx.UnmarshalBinary(post.TxBytes)
		if err != nil {
			return nil, nil, vm.BlockContext{}, err
		}
		if _, err := types.Sender(types.LatestSigner(config), &ttx); err != nil {
			return nil, nil, vm.BlockContext{}, err
		}
	}

	context = core.NewEVMBlockContext(block.Header(), &dummyChain{config: config}, &t.json.Env.Coinbase)
	context.GetHash = vmTestBlockHash
	context.BaseFee = baseFee
	context.Random = nil
	if t.json.Env.Difficulty != nil {
		context.Difficulty = new(big.Int).Set(t.json.Env.Difficulty)
	}
	if config.IsLondon(new(big.Int)) && t.json.Env.Random != nil {
		rnd := common.BigToHash(t.json.Env.Random)
		context.Random = &rnd
		context.Difficulty = big.NewInt(0)
	}
	if config.IsCancun(new(big.Int), block.Time()) && t.json.Env.ExcessBlobGas != nil {
		header := &types.Header{
			Time:          block.Time(),
			ExcessBlobGas: t.json.Env.ExcessBlobGas,
		}
		context.BlobBaseFee = eip4844.CalcBlobFee(config, header)
	}

	return config, block, context, nil
}

func (t *StateTest) genesis(config *params.ChainConfig) *core.Genesis {
	genesis := &core.Genesis{
		Config:     config,
		Coinbase:   t.json.Env.Coinbase,
		Difficulty: t.json.Env.Difficulty,
		GasLimit:   t.json.Env.GasLimit,
		Number:     t.json.Env.Number,
		Timestamp:  t.json.Env.Timestamp,
		Alloc:      t.json.Pre,
	}
	if t.json.Env.Random != nil {
		// Post-Merge
		genesis.Mixhash = common.BigToHash(t.json.Env.Random)
		genesis.Difficulty = big.NewInt(0)
	}
	return genesis
}

func (tx *stTransaction) toMessage(ps stPostState, baseFee *big.Int) (*core.Message, error) {
	var from common.Address
	if tx.Sender != nil {
		from = *tx.Sender
	} else if len(tx.PrivateKey) > 0 {
		key, err := crypto.ToECDSA(tx.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %v", err)
		}
		from = crypto.PubkeyToAddress(key.PublicKey)
	}
	var to *common.Address
	if tx.To != "" {
		to = new(common.Address)
		if err := to.UnmarshalText([]byte(tx.To)); err != nil {
			return nil, fmt.Errorf("invalid to address: %v", err)
		}
	}

	if ps.Indexes.Data > len(tx.Data) {
		return nil, fmt.Errorf("tx data index %d out of bounds", ps.Indexes.Data)
	}
	if ps.Indexes.Value > len(tx.Value) {
		return nil, fmt.Errorf("tx value index %d out of bounds", ps.Indexes.Value)
	}
	if ps.Indexes.Gas > len(tx.GasLimit) {
		return nil, fmt.Errorf("tx gas limit index %d out of bounds", ps.Indexes.Gas)
	}
	dataHex := tx.Data[ps.Indexes.Data]
	valueHex := tx.Value[ps.Indexes.Value]
	gasLimit := tx.GasLimit[ps.Indexes.Gas]
	value := new(big.Int)
	if valueHex != "0x" {
		v, ok := math.ParseBig256(valueHex)
		if !ok {
			return nil, fmt.Errorf("invalid tx value %q", valueHex)
		}
		value = v
	}
	data, err := hex.DecodeString(strings.TrimPrefix(dataHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid tx data %q", dataHex)
	}
	var accessList types.AccessList
	if tx.AccessLists != nil && tx.AccessLists[ps.Indexes.Data] != nil {
		accessList = *tx.AccessLists[ps.Indexes.Data]
	}
	gasPrice := tx.GasPrice
	if baseFee != nil {
		if tx.MaxFeePerGas == nil {
			tx.MaxFeePerGas = gasPrice
		}
		if tx.MaxFeePerGas == nil {
			tx.MaxFeePerGas = new(big.Int)
		}
		if tx.MaxPriorityFeePerGas == nil {
			tx.MaxPriorityFeePerGas = tx.MaxFeePerGas
		}
		gasPrice = new(big.Int).Add(tx.MaxPriorityFeePerGas, baseFee)
		if gasPrice.Cmp(tx.MaxFeePerGas) > 0 {
			gasPrice = tx.MaxFeePerGas
		}
	}
	if gasPrice == nil {
		return nil, errors.New("no gas price provided")
	}
	var authList []types.SetCodeAuthorization
	if tx.AuthorizationList != nil {
		authList = make([]types.SetCodeAuthorization, len(tx.AuthorizationList))
		for i, auth := range tx.AuthorizationList {
			authList[i] = types.SetCodeAuthorization{
				ChainID: *uint256.MustFromBig(auth.ChainID),
				Address: auth.Address,
				Nonce:   auth.Nonce,
				V:       auth.V,
				R:       *uint256.MustFromBig(auth.R),
				S:       *uint256.MustFromBig(auth.S),
			}
		}
	}

	msg := &core.Message{
		From:                  from,
		To:                    to,
		Nonce:                 tx.Nonce,
		Value:                 uint256.MustFromBig(value),
		GasLimit:              gasLimit,
		GasPrice:              uint256.MustFromBig(gasPrice),
		GasFeeCap:             uint256.MustFromBig(tx.MaxFeePerGas),
		GasTipCap:             uint256.MustFromBig(tx.MaxPriorityFeePerGas),
		Data:                  data,
		AccessList:            accessList,
		BlobHashes:            tx.BlobVersionedHashes,
		BlobGasFeeCap:         uint256.MustFromBig(tx.BlobGasFeeCap),
		SetCodeAuthorizations: authList,
	}
	return msg, nil
}

func vmTestBlockHash(n uint64) common.Hash {
	return common.BytesToHash(crypto.Keccak256([]byte(big.NewInt(int64(n)).String())))
}

func rlpHash(x any) (h common.Hash) {
	hw := sha3.NewLegacyKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

type dummyChain struct {
	config *params.ChainConfig
}

func (d *dummyChain) Engine() consensus.Engine                        { return nil }
func (d *dummyChain) GetHeader(h common.Hash, n uint64) *types.Header { return nil }
func (d *dummyChain) GetHeaderByHash(h common.Hash) *types.Header     { return nil }
func (d *dummyChain) GetHeaderByNumber(n uint64) *types.Header        { return nil }
func (d *dummyChain) Config() *params.ChainConfig                     { return d.config }
func (d *dummyChain) CurrentHeader() *types.Header                    { return nil }
