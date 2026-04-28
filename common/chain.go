/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

// BuildChainConfig creates an Ethereum chain configuration with the specified chain ID.
// We support Shanghai and Cancun forks. Note that having a fork enabled doesn't mean
// full compatibility; see COMPATIBILITY.md.
func BuildChainConfig(chainID int64) *params.ChainConfig {
	zero := uint64(0)
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
		TerminalTotalDifficulty: big.NewInt(0), // shanghai is post-fork (mining is disabled)
		MergeNetsplitBlock:      nil,
		ShanghaiTime:            &zero,
		CancunTime:              &zero, // Enable Cancun for MCOPY, TSTORE, TLOAD opcodes
		PragueTime:              nil,
		OsakaTime:               nil,
		VerkleTime:              nil,
		BlobScheduleConfig:      params.DefaultBlobSchedule,
		Ethash:                  new(params.EthashConfig),
		Clique:                  nil,
	}
}

// Made with Bob
