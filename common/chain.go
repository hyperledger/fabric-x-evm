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
// All forks through Osaka are active from block/time 0. Note that having a fork enabled
// doesn't mean full compatibility; see COMPATIBILITY.md.
func BuildChainConfig(chainID int64) *params.ChainConfig {
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
		ShanghaiTime:            new(uint64(0)),
		CancunTime:              new(uint64(0)),
		PragueTime:              new(uint64(0)),
		OsakaTime:               new(uint64(0)),
		VerkleTime:              nil,
		BlobScheduleConfig:      params.DefaultBlobSchedule,
		Ethash:                  new(params.EthashConfig),
		Clique:                  nil,
	}
}
