/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"path/filepath"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"
)

// retentionChain returns a trie-less chain over a fresh on-disk database.
func retentionChain(t *testing.T) *Chain {
	t.Helper()
	chain, err := NewChain(filepath.Join(t.TempDir(), "gateway.db"), "", false)
	require.NoError(t, err)
	t.Cleanup(func() { chain.Close() })
	return chain
}

// retentionHash keeps each block hash distinct so parent links stay meaningful.
func retentionHash(n uint64) []byte {
	hash := make([]byte, 32)
	hash[0] = byte(n)
	hash[1] = byte(n >> 8)
	return hash
}

// handleEmptyBlocks feeds blocks 1..n, which is enough to drive pruning: the
// trigger looks only at the block number.
func handleEmptyBlocks(t *testing.T, chain *Chain, n uint64) {
	t.Helper()
	for i := uint64(1); i <= n; i++ {
		require.NoError(t, chain.Handle(t.Context(), blocks.Block{
			Number:     i,
			Hash:       retentionHash(i),
			ParentHash: retentionHash(i - 1),
			Timestamp:  1000 + int64(i),
		}))
	}
}

// storedBlockNumbers lists what actually survived, low to high.
func storedBlockNumbers(t *testing.T, chain *Chain) []uint64 {
	t.Helper()
	rows, err := chain.Store.DB.QueryContext(t.Context(),
		"SELECT block_number FROM blocks ORDER BY block_number")
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

func TestBlockRetention_PrunesToWindow(t *testing.T) {
	chain := retentionChain(t)
	// Keep 3 blocks, checking every 2. The last prune fires at block 10 and
	// removes everything at or below 10-3=7.
	chain.SetBlockRetention(3, 2)

	handleEmptyBlocks(t, chain, 10)

	require.Equal(t, []uint64{8, 9, 10}, storedBlockNumbers(t, chain))
}

func TestBlockRetention_DisabledKeepsEveryBlock(t *testing.T) {
	chain := retentionChain(t)
	// Retention is off by default; no SetBlockRetention call at all.
	handleEmptyBlocks(t, chain, 6)

	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, storedBlockNumbers(t, chain))
}

func TestBlockRetention_ZeroKeepKeepsEveryBlock(t *testing.T) {
	chain := retentionChain(t)
	// An explicit keep of 0 means "unlimited", so the interval is irrelevant.
	chain.SetBlockRetention(0, 2)
	handleEmptyBlocks(t, chain, 6)

	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, storedBlockNumbers(t, chain))
}

func TestBlockRetention_NeverPrunesBelowWindow(t *testing.T) {
	chain := retentionChain(t)
	// Fewer blocks than the window: the guard blockNumber > keep must hold,
	// and in particular must not underflow into a huge upTo.
	chain.SetBlockRetention(10, 1)
	handleEmptyBlocks(t, chain, 4)

	require.Equal(t, []uint64{1, 2, 3, 4}, storedBlockNumbers(t, chain))
}

func TestBlockRetention_ZeroIntervalUsesDefault(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(5, 0)

	// A zero interval must be replaced, or Handle divides by zero.
	require.Equal(t, uint64(DefaultBlockPruneInterval), chain.pruneInterval)

	handleEmptyBlocks(t, chain, 8)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8}, storedBlockNumbers(t, chain))
}

func TestBlockRetention_HeadRemainsQueryableAfterPruning(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(2, 2)
	handleEmptyBlocks(t, chain, 8)

	// Pruning must not disturb the tip: prevHash is reseeded from it on restart
	// and eth_getBlockByNumber("latest") reads it.
	latest, err := chain.LatestBlock(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.EqualValues(t, 8, latest.BlockNumber)
	require.Equal(t, retentionHash(8), latest.BlockHash)

	// A pruned block reads back as absent rather than as an error.
	gone, err := chain.GetBlockByNumber(t.Context(), 1, false)
	require.NoError(t, err)
	require.Nil(t, gone)
}

// handleBlockNumbers feeds exactly the given block numbers, in order, so a test can
// reproduce the gaps the notification dispatcher produces when a ledger block
// carries no EVM transactions.
func handleBlockNumbers(t *testing.T, chain *Chain, nums ...uint64) {
	t.Helper()
	for _, n := range nums {
		require.NoError(t, chain.Handle(t.Context(), blocks.Block{
			Number:     n,
			Hash:       retentionHash(n),
			ParentHash: retentionHash(n - 1),
			Timestamp:  1000 + int64(n),
		}))
	}
}

// Blocks with no EVM transactions never reach Handle, so the numbers it sees have
// gaps. A trigger keyed on hitting an exact multiple of the interval would step over
// the gap and stop pruning entirely; this pins the threshold behaviour instead.
func TestBlockRetention_PrunesDespiteSkippedInterval(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(2, 5)

	// 5 and 10, the exact multiples, are precisely the numbers withheld.
	handleBlockNumbers(t, chain, 1, 2, 3, 4, 6, 7, 8, 9, 11, 12)

	stored := storedBlockNumbers(t, chain)
	require.NotEmpty(t, stored)
	require.Less(t, len(stored), 10, "no pruning happened: the interval multiples were skipped")

	// The last prune fires at 11 or 12, keeping a window of 2 block numbers.
	require.GreaterOrEqual(t, stored[0], uint64(9), "pruning left blocks older than the window")
	require.Equal(t, uint64(12), stored[len(stored)-1])
}

// A tip that jumps far past the window in one step must still prune, and must prune
// everything below the window rather than only the most recent interval.
func TestBlockRetention_PrunesAfterLargeGap(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(3, 10)

	handleBlockNumbers(t, chain, 1, 2, 3, 5000)

	require.Equal(t, []uint64{5000}, storedBlockNumbers(t, chain),
		"blocks below the window survived a large tip jump")
}

// Pruning must keep working across repeated intervals, not just fire once.
func TestBlockRetention_PrunesRepeatedly(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(3, 2)

	handleEmptyBlocks(t, chain, 20)
	require.Equal(t, []uint64{18, 19, 20}, storedBlockNumbers(t, chain))

	handleBlockNumbers(t, chain, 21, 22, 23, 24)
	require.Equal(t, []uint64{22, 23, 24}, storedBlockNumbers(t, chain))
}

// Re-delivery of an already-handled block is an explicit design property, and must
// not widen the prune window backwards.
func TestBlockRetention_RepeatedBlockDoesNotOverPrune(t *testing.T) {
	chain := retentionChain(t)
	chain.SetBlockRetention(3, 2)
	handleEmptyBlocks(t, chain, 10)
	before := storedBlockNumbers(t, chain)

	handleBlockNumbers(t, chain, 10, 10)

	require.Equal(t, before, storedBlockNumbers(t, chain))
}
