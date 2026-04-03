/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package endorser implements the EVM state management for Fabric.
//
// UNIFIED STATEDB ARCHITECTURE:
//
// StateDB is a unified implementation that combines ledger state management
// with EVM-specific state tracking. It implements the vm.StateDB interface
// and uses a single journal for all operations.
//
// Key design principles:
// 1. Single journal tracks ALL operations (reads, writes, logs, refunds, selfDestruct)
// 2. Journal is replayed in Result() to build the final read-write set
// 3. Only reads from the underlying ReadStore create MVCC read dependencies
// 4. Blind writes (writes without prior reads) are supported
// 5. Snapshot/revert truncates the journal - simple and efficient
//
// The journal contains different entry types:
// - readEntry: Records a read from the ReadStore (creates MVCC dependency)
// - writeEntry: Records a write operation
// - refundEntry: Records gas refund changes
// - selfDestructEntry: Records SELFDESTRUCT operations
//
// When Result() is called, the journal is replayed to build:
// - Read-write set for Fabric (only reads from ReadStore, all writes)
// - Final state for queries (balance, nonce, code, storage)
//
// GetStateAndCommittedState is handled by querying the ReadStore directly
// for committed state, and replaying the journal for current state.
package endorser

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// Log is a type of event.
type Log struct {
	Address []byte
	Topics  [][]byte
	Data    []byte
}

// ReadStore is the read interface required to back a StateDB.
type ReadStore interface {
	Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error)
	BlockNumber(ctx context.Context) (uint64, error)
}

// revision represents a snapshot point in the journal.
type revision struct {
	id           int
	journalIndex int
	logIndex     int
}

// Journal entry types for different operations:

// readEntry records a read from the ReadStore.
// This creates an MVCC read dependency.
type readEntry struct {
	read blocks.KVRead
}

// writeEntry records a write operation.
type writeEntry struct {
	write blocks.KVWrite
}

// refundEntry records a gas refund change.
type refundEntry struct {
	prevRefund uint64
}

// selfDestructEntry records a SELFDESTRUCT operation.
type selfDestructEntry struct {
	addr common.Address
}

// StateDB implements ExtendedStateDB by combining ledger state management
// with EVM-specific state tracking using a single unified journal.
type StateDB struct {
	namespace         string
	store             ReadStore
	blockNum          uint64
	logs              []Log
	monotonicVersions bool // if true, KVRead.Version is built from WriteRecord.Version (fabric-x MVCC semantics)

	// EVM-specific runtime state
	refund         uint64
	selfDestructed map[common.Address]struct{}

	// Snapshot/revert support: single journal tracks ALL operations
	journal        []any
	validRevisions []revision
	nextRevisionId int
}

// NewStateDB creates a new StateDB backed by the given ReadStore.
// If blockNum is 0, the current block number is queried from store.
// monotonicVersions controls MVCC semantics: when true, KVRead versions use the per-key
// monotonic version counter (Fabric-X); when false, they use (blockNum, txNum) (standard Fabric).
func NewStateDB(ctx context.Context, store ReadStore, namespace string, blockNum uint64, monotonicVersions bool) (*StateDB, error) {
	if blockNum == 0 {
		var err error
		blockNum, err = store.BlockNumber(ctx)
		if err != nil {
			return nil, err
		}
	}
	return &StateDB{
		namespace:         namespace,
		store:             store,
		blockNum:          blockNum,
		monotonicVersions: monotonicVersions,
		selfDestructed:    make(map[common.Address]struct{}),
		journal:           make([]any, 0),
		validRevisions:    make([]revision, 0),
		nextRevisionId:    0,
	}, nil
}

// NewStateDBWithDualState creates a StateDB and wraps it with a DualStateDB for testing.
// This allows tracking both Fabric state and Ethereum trie state evolution.
// If ethStateDB is nil, a new in-memory ethStateDB is created.
func NewStateDBWithDualState(ctx context.Context, store ReadStore, namespace string, blockNum uint64, monotonicVersions bool, ethStateDB *ethstate.StateDB) (ExtendedStateDB, error) {
	fabricStateDB, err := NewStateDB(ctx, store, namespace, blockNum, monotonicVersions)
	if err != nil {
		return nil, err
	}

	// Create an in-memory ethStateDB if not provided
	if ethStateDB == nil {
		memDB := rawdb.NewMemoryDatabase()
		tconf := &triedb.Config{
			Preimages: true,
			HashDB:    hashdb.Defaults,
		}
		trieDB := triedb.NewDatabase(memDB, tconf)
		stateDB := ethstate.NewDatabase(trieDB, nil)
		ethStateDB, err = ethstate.New(types.EmptyRootHash, stateDB)
		if err != nil {
			return nil, fmt.Errorf("failed to create eth StateDB: %w", err)
		}
	}

	return NewDualStateDB(ethStateDB, fabricStateDB), nil
}

// Helper functions for key generation
func accKey(addr common.Address, typ string) string {
	return "acc:" + addr.Hex() + ":" + typ
}

func storeKey(addr common.Address, slot common.Hash) string {
	return "str:" + addr.Hex() + ":" + slot.Hex()
}

// -------------------- Internal state query helpers --------------------

// getStateFromJournal scans the journal backwards to find the latest write for a key.
// Returns the value and true if found, or nil and false if not found.
func (s *StateDB) getStateFromJournal(key string) ([]byte, bool) {
	for i := len(s.journal) - 1; i >= 0; i-- {
		if w, ok := s.journal[i].(writeEntry); ok && w.write.Key == key {
			if w.write.IsDelete {
				return nil, true
			}
			return w.write.Value, true
		}
	}
	return nil, false
}

// getStateFromStore reads from the underlying ReadStore and journals the read.
// This creates an MVCC read dependency.
func (s *StateDB) getStateFromStore(key string) ([]byte, error) {
	record, err := s.store.Get(s.namespace, key, s.blockNum)
	if err != nil {
		return nil, err
	}

	var val []byte
	var read = blocks.KVRead{Key: key}
	if record != nil {
		if record.IsDelete {
			return nil, nil
		}
		if s.monotonicVersions {
			read.Version = &blocks.Version{BlockNum: record.Version}
		} else {
			read.Version = &blocks.Version{
				BlockNum: record.BlockNum,
				TxNum:    record.TxNum,
			}
		}
		val = record.Value
	}

	// Journal the read - this creates an MVCC dependency
	s.journal = append(s.journal, readEntry{read: read})
	return val, nil
}

// getState returns the current value for a key, checking journal first, then store.
// This creates an MVCC read dependency if the value is not in the journal.
func (s *StateDB) getState(key string) ([]byte, error) {
	// Check journal first
	if val, found := s.getStateFromJournal(key); found {
		return val, nil
	}

	// Not in journal, read from store (creates MVCC dependency)
	return s.getStateFromStore(key)
}

// putState writes a value to the journal (blind write - no read dependency).
// Empty values are treated as deletes (standard Fabric behavior).
func (s *StateDB) putState(key string, value []byte) {
	write := blocks.KVWrite{Key: key}
	if len(value) == 0 {
		write.IsDelete = true
	} else {
		write.Value = value
	}
	s.journal = append(s.journal, writeEntry{write: write})
}

// -------------------- vm.StateDB interface implementation --------------------

// CreateAccount creates an account with zero balance and nonce.
func (s *StateDB) CreateAccount(addr common.Address) {
	s.putState(accKey(addr, "bal"), uint256ToBytes(uint256.NewInt(0)))
	s.putState(accKey(addr, "nonce"), uint64ToBytes(0))
}

// CreateContract creates a contract account with empty code.
func (s *StateDB) CreateContract(addr common.Address) {
	s.putState(accKey(addr, "code"), []byte{})
}

// GetBalance returns the balance of an account.
func (s *StateDB) GetBalance(addr common.Address) *uint256.Int {
	val, err := s.getState(accKey(addr, "bal"))
	if err != nil {
		panic(fmt.Errorf("GetBalance failed: %w", err))
	}
	return bytesToUint256(val)
}

// AddBalance adds balance to an account.
func (s *StateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if amount.IsZero() {
		return *uint256.NewInt(0)
	}
	prev := s.GetBalance(addr)
	newBal := new(uint256.Int).Add(prev, amount)
	s.putState(accKey(addr, "bal"), uint256ToBytes(newBal))
	return *prev
}

// SubBalance subtracts balance from an account.
func (s *StateDB) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if amount.IsZero() {
		return *uint256.NewInt(0)
	}
	prev := s.GetBalance(addr)
	newBal := new(uint256.Int).Sub(prev, amount)
	s.putState(accKey(addr, "bal"), uint256ToBytes(newBal))
	return *prev
}

// GetNonce returns the nonce of an account.
func (s *StateDB) GetNonce(addr common.Address) uint64 {
	val, err := s.getState(accKey(addr, "nonce"))
	if err != nil {
		panic(fmt.Errorf("GetNonce failed: %w", err))
	}
	return bytesToUint64(val)
}

// SetNonce sets the nonce of an account.
func (s *StateDB) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	s.putState(accKey(addr, "nonce"), uint64ToBytes(nonce))
}

// GetCode returns the code of an account.
func (s *StateDB) GetCode(addr common.Address) []byte {
	val, err := s.getState(accKey(addr, "code"))
	if err != nil {
		panic(fmt.Errorf("GetCode failed: %w", err))
	}
	return val
}

// GetCodeHash returns the code hash of an account.
func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	code := s.GetCode(addr)
	return crypto.Keccak256Hash(code)
}

// GetCodeSize returns the code size of an account.
func (s *StateDB) GetCodeSize(addr common.Address) int {
	code := s.GetCode(addr)
	return len(code)
}

// SetCode sets the code of an account.
func (s *StateDB) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	prev := s.GetCode(addr)
	s.putState(accKey(addr, "code"), code)
	return prev
}

// GetState returns the current storage value for a slot.
func (s *StateDB) GetState(addr common.Address, slot common.Hash) common.Hash {
	key := storeKey(addr, slot)
	val, err := s.getState(key)
	if err != nil {
		panic(fmt.Errorf("GetState failed: %w", err))
	}
	if len(val) == 0 {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

// GetStateAndCommittedState returns both current and committed state.
// Current state is from the journal, committed state is from the ReadStore.
// This DOES create a read dependency for the committed state.
func (s *StateDB) GetStateAndCommittedState(addr common.Address, slot common.Hash) (common.Hash, common.Hash) {
	key := storeKey(addr, slot)

	// Get current state from journal (no store read)
	currentVal, found := s.getStateFromJournal(key)
	var current common.Hash
	if found && len(currentVal) > 0 {
		current = common.BytesToHash(currentVal)
	}

	// Get committed state from store and journal the read (creates MVCC dependency)
	committedVal, err := s.getStateFromStore(key)
	if err != nil {
		panic(fmt.Errorf("GetStateAndCommittedState failed: %w", err))
	}

	var committed common.Hash
	if len(committedVal) > 0 {
		committed = common.BytesToHash(committedVal)
	}

	// If not found in journal, current equals committed
	if !found {
		current = committed
	}

	return current, committed
}

// SetState sets the storage value for a slot.
// This is a blind write - it does NOT create a read dependency.
// The previous value is returned from the journal if it exists, otherwise from the store.
func (s *StateDB) SetState(addr common.Address, slot common.Hash, value common.Hash) common.Hash {
	key := storeKey(addr, slot)

	// Get previous value from journal first (no read dependency)
	prevVal, found := s.getStateFromJournal(key)
	var prev common.Hash
	if found {
		if len(prevVal) > 0 {
			prev = common.BytesToHash(prevVal)
		}
	} else {
		// Not in journal, read directly from store WITHOUT creating a read dependency
		record, err := s.store.Get(s.namespace, key, s.blockNum)
		if err != nil {
			panic(fmt.Errorf("SetState failed to get previous value: %w", err))
		}
		if record != nil && !record.IsDelete && len(record.Value) > 0 {
			prev = common.BytesToHash(record.Value)
		}
	}

	// Write new value
	s.putState(key, value.Bytes())

	return prev
}

// GetStorageRoot returns the storage root (stub for now).
func (s *StateDB) GetStorageRoot(addr common.Address) common.Hash {
	return common.Hash{}
}

// SelfDestruct marks an account as self-destructed.
func (s *StateDB) SelfDestruct(addr common.Address) uint256.Int {
	// Journal the self-destruct
	s.journal = append(s.journal, selfDestructEntry{addr: addr})
	s.selfDestructed[addr] = struct{}{}
	return *uint256.NewInt(0)
}

// SelfDestruct6780 implements EIP-6780 self-destruct.
func (s *StateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	return *uint256.NewInt(0), false
}

// HasSelfDestructed checks if an account has self-destructed.
func (s *StateDB) HasSelfDestructed(addr common.Address) bool {
	_, ok := s.selfDestructed[addr]
	return ok
}

// Exist checks if an account exists.
func (s *StateDB) Exist(addr common.Address) bool {
	// Check if any account field exists
	if val, err := s.getState(accKey(addr, "bal")); err == nil && val != nil {
		return true
	}
	if val, err := s.getState(accKey(addr, "nonce")); err == nil && val != nil {
		return true
	}
	if val, err := s.getState(accKey(addr, "code")); err == nil && val != nil {
		return true
	}
	return false
}

// Empty checks if an account is empty (EIP-161).
func (s *StateDB) Empty(addr common.Address) bool {
	balance := s.GetBalance(addr)
	if balance != nil && !balance.IsZero() {
		return false
	}
	nonce := s.GetNonce(addr)
	if nonce > 0 {
		return false
	}
	codeSize := s.GetCodeSize(addr)
	return codeSize == 0
}

// GetRefund returns the current gas refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// AddRefund adds to the gas refund counter.
func (s *StateDB) AddRefund(gas uint64) {
	s.journal = append(s.journal, refundEntry{prevRefund: s.refund})
	s.refund += gas
}

// SubRefund subtracts from the gas refund counter.
func (s *StateDB) SubRefund(gas uint64) {
	if gas > s.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund))
	}
	s.journal = append(s.journal, refundEntry{prevRefund: s.refund})
	s.refund -= gas
}

// AddLog adds a log entry.
func (s *StateDB) AddLog(log *types.Log) {
	topics := make([][]byte, len(log.Topics))
	for i, t := range log.Topics {
		topics[i] = t.Bytes()
	}
	s.logs = append(s.logs, Log{
		Address: log.Address.Bytes(),
		Topics:  topics,
		Data:    log.Data,
	})
}

// Snapshot creates a snapshot of the current state.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{
		id:           id,
		journalIndex: len(s.journal),
		logIndex:     len(s.logs),
	})
	return id
}

// RevertToSnapshot reverts to a previous snapshot.
func (s *StateDB) RevertToSnapshot(revid int) {
	// Find the snapshot
	idx := -1
	for i, rev := range s.validRevisions {
		if rev.id == revid {
			idx = i
			break
		}
	}
	if idx == -1 {
		return
	}

	snapshot := s.validRevisions[idx]

	// Revert in-memory state by replaying journal entries in reverse
	for i := len(s.journal) - 1; i >= snapshot.journalIndex; i-- {
		switch e := s.journal[i].(type) {
		case refundEntry:
			s.refund = e.prevRefund
		case selfDestructEntry:
			delete(s.selfDestructed, e.addr)
		}
	}

	// Truncate journal, logs, and revisions
	s.journal = s.journal[:snapshot.journalIndex]
	s.logs = s.logs[:snapshot.logIndex]
	s.validRevisions = s.validRevisions[:idx]
}

// Result returns the read-write set containing all non-reverted operations.
func (s *StateDB) Result() blocks.ReadWriteSet {
	reads := make(map[string]blocks.KVRead)
	writes := make(map[string]blocks.KVWrite)

	for _, entry := range s.journal {
		switch e := entry.(type) {
		case readEntry:
			reads[e.read.Key] = e.read
		case writeEntry:
			writes[e.write.Key] = e.write
		}
	}

	rws := blocks.ReadWriteSet{
		Reads:  make([]blocks.KVRead, 0, len(reads)),
		Writes: make([]blocks.KVWrite, 0, len(writes)),
	}
	for _, r := range reads {
		rws.Reads = append(rws.Reads, r)
	}
	for _, w := range writes {
		rws.Writes = append(rws.Writes, w)
	}
	return rws
}

// Logs returns all non-reverted logs.
func (s *StateDB) Logs() []Log {
	return s.logs
}

// Version returns the block height of this snapshot.
func (s *StateDB) Version() uint64 {
	return s.blockNum
}

// -------------------- Stub implementations for unused methods --------------------

func (s *StateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	return common.Hash{}
}

func (s *StateDB) SetTransientState(addr common.Address, key, value common.Hash) {}

func (s *StateDB) AddPreimage(hash common.Hash, preimage []byte) {}

func (s *StateDB) AddressInAccessList(addr common.Address) bool { return false }

func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	return false, false
}

func (s *StateDB) AddAddressToAccessList(addr common.Address) {}

func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
}

func (s *StateDB) PointCache() *utils.PointCache { return nil }

func (s *StateDB) Witness() *stateless.Witness { return nil }

func (s *StateDB) AccessEvents() *ethstate.AccessEvents { return nil }

func (s *StateDB) Finalise(deleteEmptyObjects bool) {}

// -------------------- Helper functions --------------------

func uint256ToBytes(u *uint256.Int) []byte {
	if u == nil {
		return nil
	}
	return u.ToBig().Bytes()
}

func bytesToUint256(b []byte) *uint256.Int {
	if len(b) == 0 {
		return new(uint256.Int)
	}
	u, _ := uint256.FromBig(new(big.Int).SetBytes(b))
	return u
}

func uint64ToBytes(val uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, val)
	return b
}

func bytesToUint64(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}
