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
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
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

func NewSnapshotDB(store Backend) *SnapshotDB {
	return &SnapshotDB{
		store:          store,
		ops:            make([]StateOp, 0),
		selfDestructed: make(map[common.Address]struct{}),
	}
}

type SnapshotDB struct {
	ops   []StateOp
	store Backend
	// selfDestructed is kept in memory to determine whether SelfDestruct was called on this contract
	// during this transaction. It is only accurate if SnapshotDB is recreated for each transaction!
	selfDestructed map[common.Address]struct{}
}

// AppendOp records an operation
func (d *SnapshotDB) appendOp(op StateOp) {
	d.ops = append(d.ops, op)
}

func accKey(addr common.Address, typ string) string {
	return "acc:" + addr.Hex() + ":" + typ
}
func storeKey(addr common.Address, slot common.Hash) string {
	return "str:" + addr.Hex() + ":" + slot.Hex()
}

// CreateAccount logs creation
func (d *SnapshotDB) CreateAccount(addr common.Address) {
	d.appendOp(StateOp{Type: OpCreateAccount, Address: addr})
	must(d.store.PutState(accKey(addr, "bal"), uint256ToBytes(uint256.MustFromBig(big.NewInt(0)))))
	must(d.store.PutState(accKey(addr, "nonce"), uint256ToBytes(uint256.MustFromBig(big.NewInt(0)))))
}

// CreateContract logs contract creation
func (d *SnapshotDB) CreateContract(addr common.Address) {
	d.appendOp(StateOp{Type: OpCreateContract, Address: addr})
	must(d.store.PutState(accKey(addr, "code"), []byte{}))
}

// -------------------- State reads --------------------

func (d *SnapshotDB) GetBalance(addr common.Address) *uint256.Int {
	d.appendOp(StateOp{Type: OpGetBalance, Address: addr})
	res, err := d.store.GetState(accKey(addr, "bal"))
	must(err)
	return bytesToUint256(res)
}

func (d *SnapshotDB) GetCode(addr common.Address) []byte {
	d.appendOp(StateOp{Type: OpGetCode, Address: addr})
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
	d.appendOp(StateOp{Type: OpGetState, Address: addr, Slot: &slot})
	res, err := d.store.GetState(storeKey(addr, slot))
	must(err)
	return common.HexToHash(string(res))
}

// NOTE: we only care about the current state.
func (d *SnapshotDB) GetStateAndCommittedState(addr common.Address, slot common.Hash) (common.Hash, common.Hash) {
	d.appendOp(StateOp{Type: OpGetState, Address: addr, Slot: &slot})
	state := d.GetState(addr, slot)
	return state, common.Hash{} // dummy committed
}

// HasSelfDestructed tracks whether a contract account (one with code) has executed a SELFDESTRUCT in the current transaction.
// It’s not persisted to the world state — it only exists in memory during transaction execution.
func (d *SnapshotDB) HasSelfDestructed(addr common.Address) bool {
	_, ok := d.selfDestructed[addr]
	return ok
}

// Exist is true if contract/account exists.
func (d *SnapshotDB) Exist(addr common.Address) bool {
	d.appendOp(StateOp{Type: OpExist, Address: addr})
	raw, _ := d.store.GetState(accKey(addr, "bal"))
	if raw != nil {
		return true
	}
	return d.GetCodeSize(addr) > 0
}

// Empty is for EIP-161 rules (empty account): balance == 0, nonce == 0, and code length == 0.
func (d *SnapshotDB) Empty(addr common.Address) bool {
	d.appendOp(StateOp{Type: OpEmpty, Address: addr})

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
	d.appendOp(StateOp{Type: OpAddBalance, Address: addr, IntValue: bal, Reason: byte(reason)})
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
	d.appendOp(StateOp{Type: OpSubBalance, Address: addr, IntValue: bal, Reason: byte(reason)})
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
	d.appendOp(StateOp{Type: OpSetCode, Address: addr, Data: &code, Reason: byte(reason)})
	prev := d.GetCode(addr)
	must(d.store.PutState(accKey(addr, "code"), code))

	return prev
}

func (d *SnapshotDB) SetState(addr common.Address, slot common.Hash, value common.Hash) common.Hash {
	d.appendOp(StateOp{Type: OpSetState, Address: addr, Slot: &slot, Value: &value})
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
	d.appendOp(StateOp{Type: OpSelfDestruct, Address: addr})

	// TODO: Removes code, storage, balance; marks account as dead.
	// Set in-memory flag for HasSelfDestructed
	d.selfDestructed[addr] = struct{}{}

	return *uint256.NewInt(0) // TODO
}

func (d *SnapshotDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	d.appendOp(StateOp{Type: OpSelfDestruct6780, Address: addr})
	return *uint256.NewInt(0), false // TODO
}

func (d *SnapshotDB) AddLog(log *types.Log) {
	topics := make([][]byte, len(log.Topics))
	for i, t := range log.Topics {
		topics[i] = t.Bytes()
	}

	d.store.AddLog(log.Address.Bytes(), topics, log.Data)
	d.appendOp(StateOp{Type: OpAddLog, Address: log.Address, Log: log})
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

func (d *SnapshotDB) AddRefund(r uint64) {}
func (d *SnapshotDB) SubRefund(r uint64) {}
func (d *SnapshotDB) GetRefund() uint64  { return 0 }

// Witness is used for stateless execution; stub.
func (d *SnapshotDB) Witness() *stateless.Witness { return nil }

// AccessEvents are only for tracing; stub.
func (d *SnapshotDB) AccessEvents() *ethstate.AccessEvents { return nil }
func (d *SnapshotDB) Finalise(b bool)                      {}

func (d *SnapshotDB) Result() blocks.ReadWriteSet { return d.store.Result() }
func (d *SnapshotDB) Logs() []state.Log           { return d.store.Logs() }

// -------------------- Snapshots  --------------------
func (d *SnapshotDB) RevertToSnapshot(ss int) {}
func (d *SnapshotDB) Snapshot() int           { return 0 }

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

// -------------------- State Ops are recorded for debugging --------------------

// OpType represents the type of operation we record
type OpType string

type StateOp struct {
	Type     OpType         `json:"type"`
	Address  common.Address `json:"address"`
	Slot     *common.Hash   `json:"slot,omitempty"`
	Value    *common.Hash   `json:"value,omitempty"`
	IntValue *uint256.Int   `json:"int_value,omitempty"`
	Data     *[]byte        `json:"data,omitempty"`
	Reason   byte           `json:"reason"`
	Log      *types.Log     `json:"log,omitempty"`
}

func (op StateOp) String() string {
	slot := ""
	if op.Slot != nil {
		n := new(big.Int)
		n.SetString(op.Slot.Hex()[2:], 16)
		if n.Cmp(big.NewInt(9999)) == -1 {
			// represent low slots as decimal for readability
			slot = n.String()
		} else {
			slot = op.Slot.Hex()
		}
	}

	dataLen := 0
	if op.Data != nil {
		dataLen = len(*op.Data)
	}
	return fmt.Sprintf("%s: addr=%s slot=%s val=%s intval=%s data=%db reason=%v log=%v", op.Type, op.Address, slot, op.Value, op.IntValue, dataLen, op.Reason, op.Log)
}

const (
	OpCreateAccount             OpType = "CreateAccount"
	OpCreateContract            OpType = "CreateContract"
	OpAddBalance                OpType = "AddBalance"
	OpSubBalance                OpType = "SubBalance"
	OpGetBalance                OpType = "GetBalance"
	OpSetNonce                  OpType = "SetNonce"
	OpGetNonce                  OpType = "GetNonce"
	OpSetCode                   OpType = "SetCode"
	OpGetCode                   OpType = "GetCode"
	OpGetCodeSize               OpType = "GetCodeSize"
	OpGetCodeHash               OpType = "GetCodeHash"
	OpSetState                  OpType = "SetState"
	OpGetState                  OpType = "GetState"
	OpGetStateAndCommittedState OpType = "GetStateAndCommittedState"
	OpSelfDestruct              OpType = "SelfDestruct"
	OpSelfDestruct6780          OpType = "SelfDestruct6780"
	OpHasSelfDestructed         OpType = "HasSelfDestructed"
	OpExist                     OpType = "Exist"
	OpEmpty                     OpType = "Empty"
	OpAddLog                    OpType = "AddLog"
	// OpGetStorageRoot            OpType = "GetStorageRoot"
	// OpRevertToSnapshot          OpType = "RevertToSnapshot"
	// OpSnapshot                  OpType = "Snapshot"
	// No need to capture:
	// OpGetTransientState         OpType = "GetTransientState"
	// OpSetTransientState         OpType = "SetTransientState"
	// OpAddRefund                 OpType = "AddRefund"
	// OpGetRefund                 OpType = "GetRefund"
	// OpSubRefund                 OpType = "SubRefund"
	// OpAddressInAccessList       OpType = "AddressInAccessList"
	// OpAddAddressToAccessList    OpType = "AddAddressToAccessList"
	// OpAddSlotToAccessList       OpType = "AddSlotToAccessList"
	// OpSlotInAccessList          OpType = "SlotInAccessList"
	// OpAddPreimage               OpType = "AddPreimage"
)

func (d *SnapshotDB) Ops() []StateOp {
	return d.ops
}

// Apply executes a write transaction on the database or silently ignores it.
func (d *SnapshotDB) Apply(op StateOp) {
	switch op.Type {
	case OpCreateAccount:
		d.CreateAccount(op.Address)
	case OpCreateContract:
		d.CreateContract(op.Address)
	case OpAddBalance:
		d.AddBalance(op.Address, op.IntValue, tracing.BalanceChangeReason(op.Reason))
	case OpSubBalance:
		d.SubBalance(op.Address, op.IntValue, tracing.BalanceChangeReason(op.Reason))
	case OpSetNonce:
		d.SetNonce(op.Address, op.IntValue.Uint64(), tracing.NonceChangeReason(op.Reason))
	case OpSetState:
		d.SetState(op.Address, *op.Slot, *op.Value)
	case OpSetCode:
		d.SetCode(op.Address, *op.Data, tracing.CodeChangeReason(op.Reason))
	case OpSelfDestruct:
		d.SelfDestruct(op.Address)
	case OpAddLog:
		d.AddLog(op.Log)
	default:
		// no op
	}
}
