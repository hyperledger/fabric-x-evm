/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package endorser implements the EVM state management for Fabric.
//
// SNAPSHOT/REVERT ARCHITECTURE:
//
// The EVM requires snapshot/revert support for proper execution semantics. When a subcall fails,
// all state changes made during that subcall must be reverted as if they never happened.
//
// This is implemented through a two-layer architecture:
//
// 1. SimulationStore (endorser/simulation_store.go):
//   - Handles ALL ledger state operations (balance, nonce, code, storage)
//   - Journals every operation (reads, writes, logs) as it occurs
//   - Implements Snapshot() and RevertToSnapshot() for ledger state
//   - Only non-reverted operations appear in the final read-write set (RWS)
//   - This ensures MVCC correctness: reverted reads don't create dependencies
//
// 2. SnapshotDB (this file):
//   - Thin wrapper around SimulationStore
//   - Delegates all ledger operations directly to SimulationStore (passthrough)
//   - Maintains its own journal ONLY for in-memory EVM state:
//   - refund counter (gas refunds)
//   - selfDestructed map (tracks SELFDESTRUCT calls)
//   - Coordinates snapshots with SimulationStore using the same snapshot IDs
//
// When Snapshot() is called:
//   - SnapshotDB calls store.Snapshot() to get an ID
//   - Uses the same ID to record its in-memory journal position
//   - Both layers can now revert to this point independently
//
// When RevertToSnapshot() is called:
//   - SnapshotDB calls store.RevertToSnapshot() to revert ledger state
//   - Then reverts its own in-memory state (refund, selfDestruct)
//   - All operations after the snapshot are undone in both layers
//
// This design ensures:
//   - Ledger state changes are properly journaled and can be reverted
//   - In-memory EVM state is also properly reverted
//   - The final RWS contains only non-reverted operations
//   - MVCC validation will only check non-reverted reads
package endorser

import (
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

type Backend interface {
	DelState(key string) error
	GetState(key string) ([]byte, error)
	PutState(key string, value []byte) error
	AddLog(address []byte, topics [][]byte, data []byte)
	Version() uint64
	Result() blocks.ReadWriteSet
	Logs() []Log
	Snapshot() int
	RevertToSnapshot(int)
}

// NewSnapshotDB returns a state DB backed by the supplied store
func NewSnapshotDB(store Backend) ExtendedStateDB {
	return &SnapshotDB{
		store:          store,
		selfDestructed: make(map[common.Address]struct{}),
		committedState: make(map[string]common.Hash),
		journal:        []snapshotJournalEntry{},
		validRevisions: []snapshotRevision{},
		nextRevisionId: 0,
	}
}

// NewSnapshotDBWithDualState returns a state DB backed by the supplied store
// The returned state DB is actually a dual state DB, containing both ethereum
// state DB and fabric's. This is used in testing to assert facts about the
// state of the root trie which is kept by the ethereum KVS.
//
// NOTE: this constructor is meant to be used in testing only.
func NewSnapshotDBWithDualState(store Backend, ethStateDB *ethstate.StateDB) ExtendedStateDB {
	// Create the SnapshotDB
	snapshotDB := &SnapshotDB{
		store:          store,
		selfDestructed: make(map[common.Address]struct{}),
		committedState: make(map[string]common.Hash),
		journal:        []snapshotJournalEntry{},
		validRevisions: []snapshotRevision{},
		nextRevisionId: 0,
	}

	// If ethStateDB is not provided, create a new in-memory one
	if ethStateDB == nil {
		// Create the eth StateDB following the pattern from tests/state_test_util.go
		// Use an in-memory database
		memDB := rawdb.NewMemoryDatabase()

		// Configure the trie database with hash scheme and preimages enabled
		tconf := &triedb.Config{
			Preimages: true,
			HashDB:    hashdb.Defaults,
		}
		trieDB := triedb.NewDatabase(memDB, tconf)

		// Create the state database
		stateDB := ethstate.NewDatabase(trieDB, nil)

		// Create a new state with empty root
		var err error
		ethStateDB, err = ethstate.New(types.EmptyRootHash, stateDB)
		if err != nil {
			panic(fmt.Errorf("failed to create eth StateDB: %w", err))
		}
	}

	// Return the dual state DB wrapping both implementations
	return NewDualStateDB(ethStateDB, snapshotDB)
}

// snapshotJournalEntry is a modification entry for in-memory state that can be reverted.
// Only used for refund counter and selfDestruct tracking, which are EVM-specific runtime state
// not stored in the ledger. Ledger state (balance, nonce, code, storage) is handled by SimulationStore.
type snapshotJournalEntry interface {
	revert(*SnapshotDB)
}

// refundChangeEntry records a change to the refund counter
type refundChangeEntry struct {
	prevRefund uint64
}

func (e refundChangeEntry) revert(s *SnapshotDB) {
	s.refund = e.prevRefund
}

// selfDestructEntry records a self-destruct operation
type selfDestructEntry struct {
	addr common.Address
}

func (e selfDestructEntry) revert(s *SnapshotDB) {
	delete(s.selfDestructed, e.addr)
}

// snapshotRevision tracks a snapshot point in the journal
type snapshotRevision struct {
	id           int
	journalIndex int
}

// SnapshotDB wraps a Backend (SimulationStore) and provides the ExtendedStateDB interface.
// It delegates ledger state operations (balance, nonce, code, storage) to the store.
// It maintains its own journal for in-memory EVM state (refund counter, selfDestruct tracking).
type SnapshotDB struct {
	store Backend
	// selfDestructed tracks contracts that called SELFDESTRUCT in this transaction
	selfDestructed map[common.Address]struct{}
	// refund is the gas refund counter
	refund uint64
	// committedState caches the original committed values from the store before any modifications
	// Key format: "addr:slot" -> committed value
	committedState map[string]common.Hash

	// Journal for tracking in-memory state changes (refund, selfDestruct)
	// Ledger state changes are journaled in SimulationStore
	journal        []snapshotJournalEntry
	validRevisions []snapshotRevision
	nextRevisionId int
}

func accKey(addr common.Address, typ string) string {
	return "acc:" + addr.Hex() + ":" + typ
}
func storeKey(addr common.Address, slot common.Hash) string {
	return "str:" + addr.Hex() + ":" + slot.Hex()
}

// CreateAccount logs creation
func (d *SnapshotDB) CreateAccount(addr common.Address) {
	must(d.store.PutState(accKey(addr, "bal"), uint256ToBytes(uint256.MustFromBig(big.NewInt(0)))))
	must(d.store.PutState(accKey(addr, "nonce"), uint64ToBytes(0)))
}

// CreateContract logs contract creation
func (d *SnapshotDB) CreateContract(addr common.Address) {
	must(d.store.PutState(accKey(addr, "code"), []byte{}))
}

// -------------------- State reads --------------------

func (d *SnapshotDB) GetBalance(addr common.Address) *uint256.Int {
	res, err := d.store.GetState(accKey(addr, "bal"))
	must(err)
	return bytesToUint256(res)
}

func (d *SnapshotDB) GetCode(addr common.Address) []byte {
	res, err := d.store.GetState(accKey(addr, "code"))
	must(err)
	return res
}

func (d *SnapshotDB) GetCodeHash(addr common.Address) common.Hash {
	c := d.GetCode(addr)
	return crypto.Keccak256Hash(c)
}

func (d *SnapshotDB) GetCodeSize(addr common.Address) int {
	c := d.GetCode(addr)
	return len(c)
}

// GetState returns the current in-flight state.
func (d *SnapshotDB) GetState(addr common.Address, slot common.Hash) common.Hash {
	key := storeKey(addr, slot)
	res, err := d.store.GetState(key)
	must(err)

	// Convert bytes directly to hash (res is the raw 32-byte value, or empty if not set)
	var value common.Hash
	if len(res) > 0 {
		value = common.BytesToHash(res)
	}
	// If res is empty/nil, value remains zero hash (correct default)

	// Cache the committed value on first read (before any modifications)
	cacheKey := addr.Hex() + ":" + slot.Hex()
	if _, exists := d.committedState[cacheKey]; !exists {
		d.committedState[cacheKey] = value
	}

	return value
}

// GetStateAndCommittedState returns both current and committed state.
// The current state is what's in the store now (may include modifications from this transaction).
// The committed state is what was in the store when the transaction started (cached on first read).
func (d *SnapshotDB) GetStateAndCommittedState(addr common.Address, slot common.Hash) (common.Hash, common.Hash) {
	// Get current state
	current := d.GetState(addr, slot)

	// Get committed state from cache
	key := addr.Hex() + ":" + slot.Hex()
	committed, exists := d.committedState[key]
	if !exists {
		// If not in cache, current IS the committed (no modifications yet)
		committed = current
	}

	return current, committed
}

// HasSelfDestructed tracks whether a contract account (one with code) has executed a SELFDESTRUCT in the current transaction.
// It’s not persisted to the world state — it only exists in memory during transaction execution.
func (d *SnapshotDB) HasSelfDestructed(addr common.Address) bool {
	_, ok := d.selfDestructed[addr]
	return ok
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for self-destructed accounts within the current transaction.
func (d *SnapshotDB) Exist(addr common.Address) bool {
	// An account exists if it has been created (has balance or nonce entry) OR has code
	// This matches the go-ethereum implementation which checks if getStateObject(addr) != nil
	// Note: CreateAccount writes balance and nonce, so checking either is sufficient
	balRaw, err := d.store.GetState(accKey(addr, "bal"))
	if err == nil && balRaw != nil {
		return true
	}
	nonceRaw, err := d.store.GetState(accKey(addr, "nonce"))
	if err == nil && nonceRaw != nil {
		return true
	}
	codeRaw, err := d.store.GetState(accKey(addr, "code"))
	if err == nil && codeRaw != nil {
		return true
	}
	return false
}

// Empty is for EIP-161 rules (empty account): balance == 0, nonce == 0, and code length == 0.
func (d *SnapshotDB) Empty(addr common.Address) bool {
	// Get account fields
	balance := d.GetBalance(addr)
	if balance != nil && !balance.IsZero() {
		return false
	}
	nonce := d.GetNonce(addr)
	if nonce > 0 {
		return false
	}

	code := d.GetCodeSize(addr)
	return code == 0
}

// -------------------- Writes --------------------

func (d *SnapshotDB) AddBalance(addr common.Address, bal *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	// ignore adding zero balance; this is something the EVM does but creates unnecessary writes.
	if bal.IsZero() {
		return *uint256.NewInt(0)
	}
	prev := d.GetBalance(addr)
	if prev == nil {
		prev = uint256.NewInt(0) // TODO: this creates an account, do we want that?
	}

	newBal := new(uint256.Int).Add(prev, bal)
	must(d.store.PutState(accKey(addr, "bal"), uint256ToBytes(newBal)))

	return *prev
}

func (d *SnapshotDB) SubBalance(addr common.Address, bal *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	// ignore subtracting zero balance; this is something the EVM does but creates unnecessary writes.
	if bal.IsZero() {
		return *uint256.NewInt(0)
	}
	prev := d.GetBalance(addr)
	if prev == nil {
		prev = uint256.NewInt(0) // TODO: this creates an account, do we want that?
	}

	newBal := new(uint256.Int).Sub(prev, bal)
	must(d.store.PutState(accKey(addr, "bal"), uint256ToBytes(newBal)))

	return *prev
}

func (d *SnapshotDB) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	prev := d.GetCode(addr)
	must(d.store.PutState(accKey(addr, "code"), code))
	return prev
}

func (d *SnapshotDB) SetState(addr common.Address, slot common.Hash, value common.Hash) common.Hash {
	prev := d.GetState(addr, slot) // ! we have to return the previous value, this adds a read.

	// Store the raw 32-byte value directly, not as a hex string
	key := storeKey(addr, slot)
	valueBytes := value.Bytes()
	must(d.store.PutState(key, valueBytes))

	return prev
}

// Nonce
func (d *SnapshotDB) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	must(d.store.PutState(accKey(addr, "nonce"), uint64ToBytes(nonce)))
}

func (d *SnapshotDB) GetNonce(addr common.Address) uint64 {
	val, err := d.store.GetState(accKey(addr, "nonce"))
	must(err)
	return bytesToUint64(val)
}

// Removes code, storage, balance; marks account as dead.
func (d *SnapshotDB) SelfDestruct(addr common.Address) uint256.Int {
	// Journal the self-destruct for potential revert
	d.journal = append(d.journal, selfDestructEntry{addr: addr})

	// TODO: Removes code, storage, balance; marks account as dead.
	// Set in-memory flag for HasSelfDestructed
	d.selfDestructed[addr] = struct{}{}

	return *uint256.NewInt(0) // TODO
}

func (d *SnapshotDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	return *uint256.NewInt(0), false // TODO
}

func (d *SnapshotDB) AddLog(log *types.Log) {
	topics := make([][]byte, len(log.Topics))
	for i, t := range log.Topics {
		topics[i] = t.Bytes()
	}

	d.store.AddLog(log.Address.Bytes(), topics, log.Data)
}

// -------------------- Dummy / gas ops --------------------

// SetTransientState only for transient (EIP-1153). Can skip for prototype.
func (d *SnapshotDB) SetTransientState(addr common.Address, slot common.Hash, value common.Hash) {
}
func (d *SnapshotDB) GetTransientState(addr common.Address, slot common.Hash) common.Hash {
	return common.Hash{}
}

// AddPreimage is only used for keccak preimage caching; can stub.
func (d *SnapshotDB) AddPreimage(hash common.Hash, preimage []byte) {}

// Access list and gas-related calls just return dummy values
func (d *SnapshotDB) AddressInAccessList(addr common.Address) bool { return false }
func (d *SnapshotDB) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	return false, false
}
func (d *SnapshotDB) AddAddressToAccessList(addr common.Address)                {}
func (d *SnapshotDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {}
func (d *SnapshotDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
}

// PointCache is for pre-compile optimizations
func (d *SnapshotDB) PointCache() *utils.PointCache { return nil }

func (d *SnapshotDB) AddRefund(gas uint64) {
	// Journal the refund change for potential revert
	d.journal = append(d.journal, refundChangeEntry{prevRefund: d.refund})
	d.refund += gas
}

func (d *SnapshotDB) SubRefund(gas uint64) {
	if gas > d.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, d.refund))
	}
	// Journal the refund change for potential revert
	d.journal = append(d.journal, refundChangeEntry{prevRefund: d.refund})
	d.refund -= gas
}

func (d *SnapshotDB) GetRefund() uint64 {
	return d.refund
}

// Witness is used for stateless execution; stub.
func (d *SnapshotDB) Witness() *stateless.Witness { return nil }

// AccessEvents are only for tracing; stub.
func (d *SnapshotDB) AccessEvents() *ethstate.AccessEvents { return nil }
func (d *SnapshotDB) Finalise(b bool)                      {}

func (d *SnapshotDB) Result() blocks.ReadWriteSet { return d.store.Result() }
func (d *SnapshotDB) Logs() []Log                 { return d.store.Logs() }

// -------------------- Snapshots  --------------------
// Snapshot and RevertToSnapshot are now simple passthroughs to the underlying store.
// The store (SimulationStore) handles all the snapshot/revert logic internally.

// Snapshot creates a snapshot of both the store and in-memory state.
// Returns a snapshot ID that can be used with RevertToSnapshot.
func (d *SnapshotDB) Snapshot() int {
	// Create snapshot in the store (for ledger state)
	storeID := d.store.Snapshot()

	// Use the same ID for our in-memory journal
	// Record the current journal position
	d.validRevisions = append(d.validRevisions, snapshotRevision{
		id:           storeID,
		journalIndex: len(d.journal),
	})
	if storeID >= d.nextRevisionId {
		d.nextRevisionId = storeID + 1
	}
	return storeID
}

// RevertToSnapshot reverts both the store and in-memory state to the snapshot.
func (d *SnapshotDB) RevertToSnapshot(revid int) {
	// Revert the store (for ledger state)
	d.store.RevertToSnapshot(revid)

	// Revert our in-memory state (refund, selfDestruct)
	// Find the snapshot in the stack of valid snapshots
	idx := -1
	for i, rev := range d.validRevisions {
		if rev.id == revid {
			idx = i
			break
		}
	}
	if idx == -1 {
		return // Snapshot not found
	}

	snapshot := d.validRevisions[idx].journalIndex

	// Replay the journal in reverse to undo in-memory changes
	for i := len(d.journal) - 1; i >= snapshot; i-- {
		d.journal[i].revert(d)
	}

	// Truncate journal and revisions
	d.journal = d.journal[:snapshot]
	d.validRevisions = d.validRevisions[:idx]
}

// GetStorageRoot is for trie db
func (d *SnapshotDB) GetStorageRoot(addr common.Address) common.Hash {
	return common.Hash{}
}

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

// uint64ToBytes converts the given uint64 value to slice of bytes.
func uint64ToBytes(val uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, val)
	return b
}

// Uint64ToBytes converts the given uint64 value to slice of bytes.
func bytesToUint64(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func must(err error) {
	if err != nil {
		panic(fmt.Errorf("irrecoverable: %s", err.Error()))
	}
}
