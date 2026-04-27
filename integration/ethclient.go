/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"crypto/ecdsa"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/utils"
)

// NonceProvider is an interface for getting account nonces.
// Gateway implements this interface.
type NonceProvider interface {
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
}

// defaultGas matches the endorser's default gas limit (executor.go) and is
// large enough to cover contract deployments used in the integration tests.
// We set it explicitly so the gateway's pre-flight intrinsic-gas check (which
// mirrors geth) does not reject these transactions.
const defaultGas uint64 = 5_000_000

// EthClient is used in testing to generate ethereum artefacts
// (e.g. signed transactions or arguments to call a smart contract)
type EthClient struct {
	priv           *ecdsa.PrivateKey
	abi            *abi.ABI
	bytecode       []byte
	ethChainConfig *params.ChainConfig
}

// NewEthClient returns a new ethereum client used to transact
// with the smart contract whose metadata is supplied as argument.
// The objects is just meant to be used for testing as it
// will generate an ephemeral identity (private key)
func NewEthClient(md *bind.MetaData, ethChainConfig *params.ChainConfig) (*EthClient, error) {
	if ethChainConfig == nil {
		// Default to AllEthashProtocolChanges
		ethChainConfig = params.AllEthashProtocolChanges
	}
	priv, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	contractABI, err := md.GetAbi()
	if err != nil {
		return nil, err
	}

	return &EthClient{
		priv:           priv,
		abi:            contractABI,
		bytecode:       common.FromHex(md.Bin),
		ethChainConfig: ethChainConfig,
	}, nil
}

func (e *EthClient) address() common.Address {
	return crypto.PubkeyToAddress(e.priv.PublicKey)
}

func (e *EthClient) txForDeploy(ctx context.Context, nonceProvider NonceProvider, blockInfo *utils.BlockInfo, args ...any) (*types.Transaction, common.Address, error) {
	constructorInput, err := e.abi.Pack("", args...)
	if err != nil {
		return nil, common.Address{}, err
	}

	callData := append(e.bytecode, constructorInput...)

	var bn *big.Int
	var bt uint64

	if blockInfo == nil {
		bn, bt = GetCtxForSigner()
	} else {
		bn = blockInfo.BlockNumber
		bt = blockInfo.BlockTime
	}

	// Determine the from address to get the nonce
	from := crypto.PubkeyToAddress(e.priv.PublicKey)

	// Get the nonce from the provider
	nonce, err := nonceProvider.NonceAt(ctx, from, bn)
	if err != nil {
		return nil, common.Address{}, err
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce: nonce,
		To:    nil, // Nil for a deploy
		Data:  callData,
		Gas:   defaultGas,
		// Value:    value,
		// GasPrice: gasPrice,
	})

	ethSigner := types.MakeSigner(e.ethChainConfig, bn, bt)

	signedTx, err := types.SignTx(tx, ethSigner, e.priv)
	if err != nil {
		return nil, common.Address{}, err
	}

	addr := crypto.CreateAddress(from, nonce)

	return signedTx, addr, nil
}

func (e *EthClient) argsForCall(to *common.Address, method string, args ...any) (*ethereum.CallMsg, error) {
	data, err := e.abi.Pack(method, args...)
	if err != nil {
		return nil, err
	}

	from := crypto.PubkeyToAddress(e.priv.PublicKey)

	return &ethereum.CallMsg{
		From: from,
		To:   to,
		Data: data,
	}, nil
}

func (e *EthClient) getResult(method string, output []byte) ([]any, error) {
	return e.abi.Unpack(method, output)
}

func (e *EthClient) txForCall(ctx context.Context, nonceProvider NonceProvider, addr *common.Address, method string, blockInfo *utils.BlockInfo, args ...any) (*types.Transaction, error) {
	data, err := e.abi.Pack(method, args...)
	if err != nil {
		return nil, err
	}

	var bn *big.Int
	var bt uint64

	if blockInfo == nil {
		bn, bt = GetCtxForSigner()
	} else {
		bn = blockInfo.BlockNumber
		bt = blockInfo.BlockTime
	}

	// Determine the from address to get the nonce
	from := crypto.PubkeyToAddress(e.priv.PublicKey)

	// Get the nonce from the provider
	nonce, err := nonceProvider.NonceAt(ctx, from, bn)
	if err != nil {
		return nil, err
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce: nonce,
		To:    addr,
		Data:  data,
		Gas:   defaultGas,
		// Value:    value,
		// GasPrice: gasPrice,
	})

	ethSigner := types.MakeSigner(e.ethChainConfig, bn, bt)

	signedTx, err := types.SignTx(tx, ethSigner, e.priv)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}

// GetCtxForSigner returns the (block, time) context used when no explicit
// blockInfo is supplied. Returning a non-nil block number is required so that
// types.MakeSigner picks an EIP-155 (replay-protected) signer; otherwise the
// gateway's pre-flight validation rejects the transaction the same way geth
// rejects unprotected transactions over RPC.
func GetCtxForSigner() (blockNumber *big.Int, blockTime uint64) {
	return big.NewInt(0), 0
}
