/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

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
	"github.com/hyperledger/fabric-x-sdk/state"
)

type Backend interface {
	DelState(key string) error
	GetState(key string) ([]byte, error)
	PutState(key string, value []byte) error
	AddLog(address []byte, topics [][]byte, data []byte)
	Version() uint64
	Result() blocks.ReadWriteSet
	Logs() []state.Log
}

// NewSnapshotDB returns a state DB backed by the supplied store
func NewSnapshotDB(store Backend) ExtendedStateDB {
	return &SnapshotDB{
		store:          store,
		selfDestructed: make(map[common.Address]struct{}),
		committedState: make(map[string]common.Hash),
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

type SnapshotDB struct {
	store Backend
	// selfDestructed is kept in memory to determine whether SelfDestruct was called on this contract
	// during this transaction. It is only accurate if SnapshotDB is recreated for each transaction!
	selfDestructed map[common.Address]struct{}
	// refund is the gas refund counter
	refund uint64
	// refundSnapshots stores refund values at each snapshot point
	refundSnapshots []uint64
	// committedState caches the original committed values from the store before any modifications
	// Key format: "addr:slot" -> committed value
	committedState map[string]common.Hash
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
	must(d.store.PutState(accKey(addr, "nonce"), uint256ToBytes(uint256.MustFromBig(big.NewInt(0)))))
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
	res, err := d.store.GetState(storeKey(addr, slot))
	must(err)
	value := common.HexToHash(string(res))

	// Cache the committed value on first read (before any modifications)
	key := addr.Hex() + ":" + slot.Hex()
	if _, exists := d.committedState[key]; !exists {
		d.committedState[key] = value
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

// Exist is true if contract/account exists.
func (d *SnapshotDB) Exist(addr common.Address) bool {
	raw, _ := d.store.GetState(accKey(addr, "bal"))
	if raw != nil {
		return true
	}
	return d.GetCodeSize(addr) > 0
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

	must(d.store.PutState(storeKey(addr, slot), []byte(value.Hex())))
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
	d.refund += gas
}

func (d *SnapshotDB) SubRefund(gas uint64) {
	if gas > d.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, d.refund))
	}
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
func (d *SnapshotDB) Logs() []state.Log           { return d.store.Logs() }

// -------------------- Snapshots  --------------------
func (d *SnapshotDB) RevertToSnapshot(ss int) {
	if ss < 0 || ss >= len(d.refundSnapshots) {
		return
	}
	// Restore the refund counter to the snapshot value
	d.refund = d.refundSnapshots[ss]
	// Truncate the snapshots array to remove snapshots after this point
	d.refundSnapshots = d.refundSnapshots[:ss]
}

func (d *SnapshotDB) Snapshot() int {
	// Save the current refund value
	d.refundSnapshots = append(d.refundSnapshots, d.refund)
	// Return the snapshot ID (index in the snapshots array)
	return len(d.refundSnapshots) - 1
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
