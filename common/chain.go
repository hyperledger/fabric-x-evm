/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// BuildChainConfig creates an Ethereum chain configuration with the specified chain ID.
// All EVM hard forks are enabled from block 0 for maximum compatibility.
// The chainID can be overridden with the CHAIN_ID environment variable.
func BuildChainConfig(chainID int64) *params.ChainConfig {
	if env := os.Getenv("CHAIN_ID"); env != "" {
		if id, err := strconv.ParseInt(env, 10, 64); err != nil {
			panic("invalid chain id " + env)
		} else {
			chainID = id
		}
	}

	return &params.ChainConfig{
		ChainID:                 big.NewInt(chainID),
		HomesteadBlock:          big.NewInt(0),
		DAOForkBlock:            nil,
		DAOForkSupport:          false,
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		ArrowGlacierBlock:       big.NewInt(0),
		GrayGlacierBlock:        big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(math.MaxInt64),
		MergeNetsplitBlock:      nil,
		ShanghaiTime:            nil,
		CancunTime:              nil,
		PragueTime:              nil,
		OsakaTime:               nil,
		VerkleTime:              nil,
		Ethash:                  new(params.EthashConfig),
		Clique:                  nil,
	}
}

// ChainConfig is the default chain configuration.
var ChainConfig = BuildChainConfig(31337) // TODO: remove.

// ValidateEthTx validates that the supplied ethereum transaction bytes and signature header
// It returns the deserialized ethereum transaction.
func ValidateEthTx(ethTxBytes []byte, ethChainConfig *params.ChainConfig) (*types.Transaction, error) {
	ethTx := &types.Transaction{}
	if err := ethTx.UnmarshalBinary(ethTxBytes); err != nil {
		return nil, err
	}

	// validate ethereum signature
	if ethChainConfig == nil {
		ethChainConfig = ChainConfig
	}
	ethSigner := types.LatestSigner(ethChainConfig)

	// This validates the signature
	if _, err := types.Sender(ethSigner, ethTx); err != nil {
		return nil, fmt.Errorf("invalid ethereum signature: %w", err)
	}

	return ethTx, nil
}
