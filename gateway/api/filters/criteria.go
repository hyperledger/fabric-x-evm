/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"math/big"

	gethfilters "github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// CriteriaToLogFilter maps geth FilterCriteria onto the store's LogFilter.
// head is the current chain tip, used when fromBlock is latest/pending/omitted.
// Same rules as gateway/api.EthAPI.filterCriteriaToLogFilter.
func CriteriaToLogFilter(crit gethfilters.FilterCriteria, head uint64) domain.LogFilter {
	filter := domain.LogFilter{}

	if crit.BlockHash != nil {
		hash := crit.BlockHash.Bytes()
		filter.BlockHash = &hash
	} else {
		filter.FromBlock = resolveFromBlock(crit.FromBlock, head)
		filter.ToBlock = resolveToBlock(crit.ToBlock)
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

func resolveFromBlock(n *big.Int, head uint64) *uint64 {
	if n != nil && n.Sign() >= 0 {
		v := n.Uint64()
		return &v
	}
	if n != nil && n.Cmp(big.NewInt(int64(rpc.EarliestBlockNumber))) == 0 {
		zero := uint64(0)
		return &zero
	}
	return &head
}

func resolveToBlock(n *big.Int) *uint64 {
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
