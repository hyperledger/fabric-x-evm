/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// retentionChain returns a trie-less chain that keeps keep block numbers.
func retentionChain(t *testing.T, keep uint64) *Chain {
	t.Helper()
	chain, err := NewChain(filepath.Join(t.TempDir(), "gateway.db"), "", false, keep)
	require.NoError(t, err)
	t.Cleanup(func() { chain.Close() })
	return chain
}

// handleBlocks feeds empty blocks with the given numbers, in order.
func handleBlocks(t *testing.T, chain *Chain, nums ...uint64) {
	t.Helper()
	for _, n := range nums {
		hash := make([]byte, 32)
		binary.BigEndian.PutUint64(hash, n)
		require.NoError(t, chain.Handle(t.Context(), blocks.Block{Number: n, Hash: hash}))
	}
}

func blockRange(from, to uint64) []uint64 {
	var nums []uint64
	for n := from; n <= to; n++ {
		nums = append(nums, n)
	}
	return nums
}

func storedBlocks(t *testing.T, chain *Chain) []uint64 {
	t.Helper()
	rows, err := chain.Store.DB.QueryContext(t.Context(), "SELECT block_number FROM blocks ORDER BY block_number")
	require.NoError(t, err)
	defer rows.Close()
	var nums []uint64
	for rows.Next() {
		var n uint64
		require.NoError(t, rows.Scan(&n))
		nums = append(nums, n)
	}
	require.NoError(t, rows.Err())
	return nums
}

func TestBlockRetention(t *testing.T) {
	t.Run("prunes to the window", func(t *testing.T) {
		chain := retentionChain(t, 3)
		handleBlocks(t, chain, blockRange(1, 10)...)
		require.Equal(t, []uint64{8, 9, 10}, storedBlocks(t, chain))
	})

	t.Run("zero keeps every block", func(t *testing.T) {
		chain := retentionChain(t, 0)
		handleBlocks(t, chain, blockRange(1, 6)...)
		require.Equal(t, blockRange(1, 6), storedBlocks(t, chain))
	})

	t.Run("prunes when block numbers are skipped", func(t *testing.T) {
		// Handle can skip block numbers (hybridx drops blocks without EVM txs).
		chain := retentionChain(t, 2)
		handleBlocks(t, chain, 1, 2, 3, 4, 6, 7, 8, 9, 11, 12)
		require.Equal(t, []uint64{11, 12}, storedBlocks(t, chain))
	})
}
