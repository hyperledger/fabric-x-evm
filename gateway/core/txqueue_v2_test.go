/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testPrivKeyV2, _ = crypto.GenerateKey()
	testChainIDV2    = big.NewInt(1337)
)

// Helper to complete a transaction via Handle method (simulates production usage)
func completeViaHandle(q *TxQueueV2, txHash common.Hash) {
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: txHash.Bytes(), Status: 1},
		},
	}
	q.Handle(context.Background(), block)
}

// Helper to create ERC20 transfer data
func erc20TransferData(recipient common.Address, amount *big.Int) []byte {
	// transfer(address,uint256) = 0xa9059cbb
	data := make([]byte, 4+32+32)
	copy(data[0:4], []byte{0xa9, 0x05, 0x9c, 0xbb})
	copy(data[4+12:4+32], recipient.Bytes())
	copy(data[36:68], common.LeftPadBytes(amount.Bytes(), 32))
	return data
}

// Helper function to create a signed transaction
func createSignedTx(nonce uint64, recipient common.Address, privKey *ecdsa.PrivateKey) *types.Transaction {
	data := erc20TransferData(recipient, big.NewInt(100))

	tx := types.NewTransaction(
		nonce,
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		big.NewInt(1),
		21000,
		big.NewInt(1),
		data,
	)

	signer := types.LatestSignerForChainID(testChainIDV2)
	signedTx, _ := types.SignTx(tx, signer, privKey)
	return signedTx
}

// Simple test transaction helper
func testTxV2(nonce uint64) *types.Transaction {
	return createSignedTx(
		nonce,
		common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12"),
		testPrivKeyV2,
	)
}

func TestNewTxQueueV2_Initialization(t *testing.T) {
	q := NewTxQueueV2()

	require.NotNil(t, q.cond)
	require.NotNil(t, q.readyList)
	require.NotNil(t, q.waitingList)
	require.NotNil(t, q.participantMap)
	require.NotNil(t, q.hashMap)
	require.NotNil(t, q.pendingMap)

	assert.Equal(t, 0, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())
	assert.Len(t, q.participantMap, 0)
	assert.Len(t, q.hashMap, 0)
	assert.Len(t, q.pendingMap, 0)
	assert.False(t, q.done)
	assert.Equal(t, 0, q.total)
	assert.Equal(t, 0, q.invalid)
}

func TestTxQueueV2_Enqueue_SingleTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)

	// Should be in ready list (no conflicts)
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())

	// Should be in hash map
	entry, exists := q.hashMap[tx.Hash()]
	require.True(t, exists)
	assert.Equal(t, tx, entry.tx)
	assert.Len(t, entry.isBlockedBy, 0)
	assert.Len(t, entry.blocks, 0)

	// Should be in participant map
	participants := participantsForTx(tx)
	for _, p := range participants {
		entries, exists := q.participantMap[p]
		require.True(t, exists)
		assert.Contains(t, entries, entry)
	}
}

func TestTxQueueV2_Enqueue_DuplicateTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Enqueue(tx) // Enqueue same transaction again

	// Should only be added once
	assert.Equal(t, 1, q.readyList.Len())
	assert.Len(t, q.hashMap, 1)
}

func TestTxQueueV2_Enqueue_ConflictingTransactions(t *testing.T) {
	q := NewTxQueueV2()

	// Create two transactions with same participants
	tx1 := testTxV2(1)
	tx2 := testTxV2(2)

	q.Enqueue(tx1)
	q.Enqueue(tx2)

	// First should be ready, second should be waiting
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 1, q.waitingList.Len())

	// Check dependency links
	entry1 := q.hashMap[tx1.Hash()]
	entry2 := q.hashMap[tx2.Hash()]

	assert.Len(t, entry1.isBlockedBy, 0)
	assert.Contains(t, entry1.blocks, entry2)

	assert.Contains(t, entry2.isBlockedBy, entry1)
	assert.Len(t, entry2.blocks, 0)
}

func TestTxQueueV2_Dequeue_SingleTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)
	q.Enqueue(tx)

	got, ok := q.Dequeue()

	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, tx.Hash(), got.Hash())

	// Should be moved to pending map
	pendingTx, exists := q.pendingMap[tx.Hash()]
	require.True(t, exists)
	assert.Equal(t, tx.Hash(), pendingTx.Hash())

	// Should be removed from ready list
	assert.Equal(t, 0, q.readyList.Len())

	// Should still be in hash map and participant map
	_, exists = q.hashMap[tx.Hash()]
	assert.True(t, exists)
}

func TestTxQueueV2_Dequeue_BlocksWhenEmpty(t *testing.T) {
	q := NewTxQueueV2()

	done := make(chan bool)
	var tx *types.Transaction
	var ok bool

	// Start dequeue in goroutine
	go func() {
		tx, ok = q.Dequeue()
		done <- true
	}()

	// Give it time to block
	time.Sleep(50 * time.Millisecond)

	// Enqueue a transaction
	testTx := testTxV2(1)
	q.Enqueue(testTx)

	// Wait for dequeue to complete
	select {
	case <-done:
		require.True(t, ok)
		require.NotNil(t, tx)
		assert.Equal(t, testTx.Hash(), tx.Hash())
	case <-time.After(1 * time.Second):
		t.Fatal("Dequeue did not unblock")
	}
}

func TestTxQueueV2_Dequeue_ReturnsWhenClosed(t *testing.T) {
	q := NewTxQueueV2()

	done := make(chan bool)
	var tx *types.Transaction
	var ok bool

	// Start dequeue in goroutine
	go func() {
		tx, ok = q.Dequeue()
		done <- true
	}()

	// Give it time to block
	time.Sleep(50 * time.Millisecond)

	// Close the queue
	q.Close()

	// Wait for dequeue to complete
	select {
	case <-done:
		assert.False(t, ok)
		assert.Nil(t, tx)
	case <-time.After(1 * time.Second):
		t.Fatal("Dequeue did not unblock after close")
	}
}

func TestTxQueueV2_Complete_SingleTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue()
	completeViaHandle(q, tx.Hash())

	// Should be removed from all maps
	_, exists := q.hashMap[tx.Hash()]
	assert.False(t, exists)

	_, exists = q.pendingMap[tx.Hash()]
	assert.False(t, exists)

	participants := participantsForTx(tx)
	for _, p := range participants {
		_, exists := q.participantMap[p]
		assert.False(t, exists)
	}
}

func TestTxQueueV2_Complete_PromotesOneTransaction(t *testing.T) {
	q := NewTxQueueV2()

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)

	q.Enqueue(tx1)
	q.Enqueue(tx2)

	// tx1 ready, tx2 waiting
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 1, q.waitingList.Len())

	q.Dequeue() // Dequeue tx1
	completeViaHandle(q, tx1.Hash())

	// tx2 should be promoted to ready
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())

	entry2 := q.hashMap[tx2.Hash()]
	assert.Len(t, entry2.isBlockedBy, 0)
}

func TestTxQueueV2_Complete_PromotesMultipleTransactions(t *testing.T) {
	q := NewTxQueueV2()

	// With the new queue jumping logic, when transactions share participants:
	// - tx1 goes to ready (no conflicts)
	// - tx2 waits for tx1 (same sender)
	// - tx3 waits for tx1 (same sender) AND makes tx2 also wait for tx3
	//
	// This creates a dependency structure where:
	// - tx1 blocks both tx2 and tx3
	// - tx3 also blocks tx2
	//
	// So the completion order is: tx1 -> tx3 -> tx2 (or tx1 -> tx2 -> tx3 depending on which becomes ready first)

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)
	tx3 := testTxV2(3)

	q.Enqueue(tx1)
	q.Enqueue(tx2) // blocked by tx1 (same sender)
	q.Enqueue(tx3) // blocked by tx1 (same sender), and makes tx2 also wait for tx3

	// Verify initial structure
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 2, q.waitingList.Len())

	q.Dequeue() // Dequeue tx1
	completeViaHandle(q, tx1.Hash())

	// After tx1 completes, both tx2 and tx3 lose one blocker
	// But tx2 still waits for tx3, so only tx3 should be promoted
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 1, q.waitingList.Len())

	// Now dequeue and complete tx3
	q.Dequeue()
	completeViaHandle(q, tx3.Hash())

	// Now tx2 should be promoted
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())
}

func TestTxQueueV2_Complete_Idempotent(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue()

	// Call completeViaHandle multiple times (idempotent)
	completeViaHandle(q, tx.Hash())
	completeViaHandle(q, tx.Hash())
	completeViaHandle(q, tx.Hash())

	// Should not panic
	assert.Len(t, q.hashMap, 0)
	assert.Len(t, q.pendingMap, 0)
}

func TestTxQueueV2_Complete_NonExistentTransaction(t *testing.T) {
	q := NewTxQueueV2()

	// Complete a transaction that was never enqueued
	fakeHash := common.HexToHash("0xdeadbeef")
	completeViaHandle(q, fakeHash)

	// Should not panic
	assert.Len(t, q.hashMap, 0)
}

func TestTxQueueV2_Complete_DirectRemovesFromPending(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	_, ok := q.Dequeue()
	require.True(t, ok)
	require.NotNil(t, q.IsPending(tx.Hash()))

	q.Complete(tx.Hash())

	assert.Nil(t, q.IsPending(tx.Hash()))
	_, exists := q.pendingMap[tx.Hash()]
	assert.False(t, exists)
}

func TestTxQueueV2_Complete_SignalsOneWorkerForOnePromotion(t *testing.T) {
	q := NewTxQueueV2()

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)

	q.Enqueue(tx1)
	q.Enqueue(tx2)

	q.Dequeue() // Dequeue tx1

	// Track how many workers wake up
	wakeups := 0
	var mu sync.Mutex

	// Start multiple workers
	for i := 0; i < 5; i++ {
		go func() {
			q.mu.Lock()
			for q.readyList.Len() == 0 && !q.done {
				q.cond.Wait()
				mu.Lock()
				wakeups++
				mu.Unlock()
			}
			q.mu.Unlock()
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Complete tx1 - should promote tx2 and signal once
	completeViaHandle(q, tx1.Hash())

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	// Only one worker should wake up (Signal, not Broadcast)
	assert.Equal(t, 1, wakeups)
	mu.Unlock()
}

func TestTxQueueV2_IsPending_InReadyList(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)

	result := q.IsPending(tx.Hash())
	require.NotNil(t, result)
	assert.Equal(t, tx.Hash(), result.Hash())
}

func TestTxQueueV2_IsPending_InWaitingList(t *testing.T) {
	q := NewTxQueueV2()

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)

	q.Enqueue(tx1)
	q.Enqueue(tx2) // tx2 will be in waiting list

	result := q.IsPending(tx2.Hash())
	require.NotNil(t, result)
	assert.Equal(t, tx2.Hash(), result.Hash())
}

func TestTxQueueV2_IsPending_InPendingMap(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue() // Moves to pending map

	result := q.IsPending(tx.Hash())
	require.NotNil(t, result)
	assert.Equal(t, tx.Hash(), result.Hash())
}

func TestTxQueueV2_IsPending_NotFound(t *testing.T) {
	q := NewTxQueueV2()

	fakeHash := common.HexToHash("0xdeadbeef")
	result := q.IsPending(fakeHash)
	assert.Nil(t, result)
}

func TestTxQueueV2_IsPending_AfterComplete(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue()
	completeViaHandle(q, tx.Hash())

	result := q.IsPending(tx.Hash())
	assert.Nil(t, result)
}

func TestTxQueueV2_Close_WakesUpWorkers(t *testing.T) {
	q := NewTxQueueV2()

	done := make(chan bool, 3)

	// Start multiple workers
	for i := 0; i < 3; i++ {
		go func() {
			_, ok := q.Dequeue()
			assert.False(t, ok)
			done <- true
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Close the queue
	q.Close()

	// All workers should wake up
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			// Worker woke up
		case <-time.After(1 * time.Second):
			t.Fatal("Worker did not wake up after close")
		}
	}

	assert.True(t, q.done)
}

func TestTxQueueV2_Handle_SingleTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue()

	// Create block with transaction
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx.Hash().Bytes(), Status: 1},
		},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	// Transaction should be complete
	assert.Nil(t, q.IsPending(tx.Hash()))

	// Stats should be updated
	total, invalid, _, _ := q.Stats()
	assert.Equal(t, 1, total)
	assert.Equal(t, 0, invalid)
}

func TestTxQueueV2_Handle_InvalidTransaction(t *testing.T) {
	q := NewTxQueueV2()
	tx := testTxV2(1)

	q.Enqueue(tx)
	q.Dequeue()

	// Create block with invalid transaction (status = 0)
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx.Hash().Bytes(), Status: 0},
		},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	// Stats should count invalid
	total, invalid, _, _ := q.Stats()
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, invalid)
}

func TestTxQueueV2_Handle_MultipleTransactions(t *testing.T) {
	q := NewTxQueueV2()

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)
	tx3 := testTxV2(3)

	q.Enqueue(tx1)
	q.Enqueue(tx2)
	q.Enqueue(tx3)

	q.Dequeue() // tx1

	// Create block with all transactions
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx1.Hash().Bytes(), Status: 1},
			{TxHash: tx2.Hash().Bytes(), Status: 1},
			{TxHash: tx3.Hash().Bytes(), Status: 0}, // invalid
		},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	// All should be complete
	assert.Nil(t, q.IsPending(tx1.Hash()))
	assert.Nil(t, q.IsPending(tx2.Hash()))
	assert.Nil(t, q.IsPending(tx3.Hash()))

	// Stats should be correct
	total, invalid, _, _ := q.Stats()
	assert.Equal(t, 3, total)
	assert.Equal(t, 1, invalid)
}

func TestTxQueueV2_Handle_EmptyBlock(t *testing.T) {
	q := NewTxQueueV2()

	block := &domain.Block{
		BlockNumber:  1,
		Transactions: []domain.Transaction{},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	total, invalid, _, _ := q.Stats()
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, invalid)
}

func TestTxQueueV2_Handle_PromotesMultipleTransactions(t *testing.T) {
	q := NewTxQueueV2()

	// Create transactions with different senders but same recipient
	privKey1, _ := crypto.GenerateKey()
	privKey2, _ := crypto.GenerateKey()

	addr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	tx1 := createSignedTx(1, addr, privKey1)
	tx2 := createSignedTx(1, addr, privKey2) // Different sender, same recipient

	q.Enqueue(tx1)
	q.Enqueue(tx2) // blocked by tx1 (same recipient)

	q.Dequeue() // tx1

	// Handle block with tx1
	block := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx1.Hash().Bytes(), Status: 1},
		},
	}

	err := q.Handle(context.Background(), block)
	require.NoError(t, err)

	// tx2 should be promoted
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())
}

func TestTxQueueV2_Stats(t *testing.T) {
	q := NewTxQueueV2()

	// Initially zero
	total, invalid, _, _ := q.Stats()
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, invalid)

	// Process some transactions with different senders so they don't block each other
	privKey1, _ := crypto.GenerateKey()
	privKey2, _ := crypto.GenerateKey()

	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx1 := createSignedTx(1, addr, privKey1)
	tx2 := createSignedTx(1, addr, privKey2)

	q.Enqueue(tx1)
	q.Enqueue(tx2) // tx2 conflicts with tx1 on recipient, so it waits

	// Dequeue tx1
	dequeuedTx1, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx1.Hash(), dequeuedTx1.Hash())

	// Complete tx1, which will promote tx2
	block1 := &domain.Block{
		BlockNumber: 1,
		Transactions: []domain.Transaction{
			{TxHash: tx1.Hash().Bytes(), Status: 1},
		},
	}
	q.Handle(context.Background(), block1)

	// Now dequeue tx2
	dequeuedTx2, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, tx2.Hash(), dequeuedTx2.Hash())

	// Complete tx2 with invalid status
	block2 := &domain.Block{
		BlockNumber: 2,
		Transactions: []domain.Transaction{
			{TxHash: tx2.Hash().Bytes(), Status: 0},
		},
	}
	q.Handle(context.Background(), block2)

	// Check stats
	total, invalid, _, _ = q.Stats()
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, invalid)
}

func TestTxQueueV2_ConcurrentEnqueueDequeue(t *testing.T) {
	q := NewTxQueueV2()

	const numTxs = 100
	const numWorkers = 5

	var wg sync.WaitGroup

	// Enqueue transactions with unique senders AND recipients so they don't block each other
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numTxs; i++ {
			// Create unique private key and recipient for each transaction
			privKey, _ := crypto.GenerateKey()
			// Use i to create unique recipient addresses
			addrBytes := [20]byte{}
			addrBytes[0] = byte(i >> 8)
			addrBytes[1] = byte(i)
			addr := common.Address(addrBytes)
			tx := createSignedTx(uint64(i), addr, privKey)
			q.Enqueue(tx)
		}
	}()

	// Dequeue transactions
	dequeued := make([]*types.Transaction, 0, numTxs)
	var dequeueMu sync.Mutex

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				tx, ok := q.Dequeue()
				if !ok {
					return
				}
				dequeueMu.Lock()
				dequeued = append(dequeued, tx)
				dequeueMu.Unlock()
			}
		}()
	}

	// Wait for all enqueues
	time.Sleep(200 * time.Millisecond)

	// Close queue
	q.Close()

	// Wait for all workers
	wg.Wait()

	// All transactions should be dequeued
	assert.Equal(t, numTxs, len(dequeued))
}

func TestTxQueueV2_RemoveFromSlice(t *testing.T) {
	// Test helper function
	slice := []*txEntry{
		{tx: testTxV2(1)},
		{tx: testTxV2(2)},
		{tx: testTxV2(3)},
	}

	target := slice[1]
	result := removeFromSlice(slice, target)

	assert.Len(t, result, 2)
	assert.NotContains(t, result, target)
}

func TestTxQueueV2_RemoveFromSlice_NotFound(t *testing.T) {
	slice := []*txEntry{
		{tx: testTxV2(1)},
		{tx: testTxV2(2)},
	}

	target := &txEntry{tx: testTxV2(3)}
	result := removeFromSlice(slice, target)

	assert.Len(t, result, 2)
	assert.Equal(t, slice, result)
}

func TestTxQueueV2_ComplexDependencyChain(t *testing.T) {
	q := NewTxQueueV2()

	// With the new queue jumping logic, when all transactions share the same sender:
	// - tx1 goes to ready
	// - tx2 waits for tx1
	// - tx3 waits for tx1 AND makes tx2 also wait for tx3
	// - tx4 waits for tx1 AND makes both tx2 and tx3 also wait for tx4
	//
	// Dependency structure after all enqueues:
	// - tx1 blocks: tx2, tx3, tx4
	// - tx3 blocks: tx2
	// - tx4 blocks: tx2, tx3
	//
	// So completion order is: tx1 -> tx4 -> tx3 -> tx2

	tx1 := testTxV2(1)
	tx2 := testTxV2(2)
	tx3 := testTxV2(3)
	tx4 := testTxV2(4)

	q.Enqueue(tx1)
	q.Enqueue(tx2) // blocked by tx1 (same sender)
	q.Enqueue(tx3) // blocked by tx1, and makes tx2 wait for tx3
	q.Enqueue(tx4) // blocked by tx1, and makes tx2 and tx3 wait for tx4

	// Verify initial structure
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 3, q.waitingList.Len())

	// Dequeue and complete tx1
	q.Dequeue()
	completeViaHandle(q, tx1.Hash())

	// After tx1 completes, tx4 should be promoted (tx2 still waits for tx3 and tx4, tx3 still waits for tx4)
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 2, q.waitingList.Len())

	// Dequeue and complete tx4
	q.Dequeue()
	completeViaHandle(q, tx4.Hash())

	// After tx4 completes, tx3 should be promoted (tx2 still waits for tx3)
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 1, q.waitingList.Len())

	// Dequeue and complete tx3
	q.Dequeue()
	completeViaHandle(q, tx3.Hash())

	// Now tx2 should be promoted
	assert.Equal(t, 1, q.readyList.Len())
	assert.Equal(t, 0, q.waitingList.Len())
}
