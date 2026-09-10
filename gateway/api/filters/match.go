/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// logsFromBlock extracts Ethereum logs from a Fabric SDK block, attaching
// block/tx context. Non-EVM and revert txs are skipped.
func logsFromBlock(b blocks.Block) []*types.Log {
	blockHash := common.BytesToHash(b.Hash)
	out := make([]*types.Log, 0)
	logIndex := uint(0)

	for _, tx := range b.Transactions {
		if len(tx.InputArgs) < 2 || len(tx.InputArgs[0]) != 1 || tx.InputArgs[0][0] != byte(fc.ProposalTypeEVMTx) {
			continue
		}
		if !tx.Valid || fc.IsRevertEvent(tx.Events) || len(tx.Events) == 0 {
			continue
		}
		raw, err := fc.UnmarshalLogs(tx.Events)
		if err != nil || len(raw) == 0 {
			continue
		}

		ethTx := new(types.Transaction)
		if err := ethTx.UnmarshalBinary(tx.InputArgs[1]); err != nil {
			continue
		}
		txHash := ethTx.Hash()
		txIndex := uint(tx.Number)

		for _, l := range raw {
			topics := make([]common.Hash, len(l.Topics))
			for i, t := range l.Topics {
				topics[i] = common.BytesToHash(t)
			}
			out = append(out, &types.Log{
				Address:     common.BytesToAddress(l.Address),
				Topics:      topics,
				Data:        l.Data,
				BlockNumber: b.Number,
				TxHash:      txHash,
				TxIndex:     txIndex,
				BlockHash:   blockHash,
				Index:       logIndex,
			})
			logIndex++
		}
	}
	return out
}

// matchLogs filters logs against FilterCriteria. Mirrors geth's filterLogs
// rules for address/topics/block range (live path uses the block's own number).
func matchLogs(logs []*types.Log, crit gethfilters.FilterCriteria) []*types.Log {
	var from, to *big.Int
	if crit.FromBlock != nil {
		from = crit.FromBlock
	}
	if crit.ToBlock != nil {
		to = crit.ToBlock
	}
	return filterLogs(logs, from, to, crit.Addresses, crit.Topics)
}

func filterLogs(logs []*types.Log, fromBlock, toBlock *big.Int, addresses []common.Address, topics [][]common.Hash) []*types.Log {
	check := func(log *types.Log) bool {
		if fromBlock != nil && fromBlock.Sign() >= 0 && fromBlock.Uint64() > log.BlockNumber {
			return false
		}
		if toBlock != nil && toBlock.Sign() >= 0 && toBlock.Uint64() < log.BlockNumber {
			return false
		}
		if len(addresses) > 0 && !slices.Contains(addresses, log.Address) {
			return false
		}
		if len(topics) > len(log.Topics) {
			return false
		}
		for i, sub := range topics {
			if len(sub) == 0 {
				continue
			}
			if !slices.Contains(sub, log.Topics[i]) {
				return false
			}
		}
		return true
	}
	var ret []*types.Log
	for _, log := range logs {
		if check(log) {
			ret = append(ret, log)
		}
	}
	return ret
}
