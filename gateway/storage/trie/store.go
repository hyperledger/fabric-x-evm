/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package trie

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// Store wraps the geth TrieDB to keep track of a full MPT of the world state.
// When dbPath is non-empty it is backed by PebbleDB and survives restarts.
// When dbPath is empty it uses an in-memory database (suitable for tests).
// Blocks must be committed in order, starting from the block after initialRoot.
type Store struct {
	mu         sync.Mutex
	diskdb     ethdb.Database // underlying disk store; closed on Close()
	db         state.Database
	root       common.Hash // current committed state root
	persistent bool
}

// New initializes a TrieStore.
// dbPath=="" uses an in-memory database; dbPath!="" opens (or creates) a PebbleDB at that path.
// initialRoot is the state root of the last committed block; use types.EmptyRootHash for a fresh store.
// Returns an error if the backing store cannot be opened or if initialRoot is not accessible.
func New(dbPath string, initialRoot common.Hash) (*Store, error) {
	var diskdb ethdb.Database
	var persistent bool

	if dbPath == "" {
		diskdb = rawdb.NewMemoryDatabase()
	} else {
		kv, err := pebble.New(dbPath, 128, 64, "fabric-x-evm/trie", false)
		if err != nil {
			return nil, fmt.Errorf("open pebble trie db at %s: %w", dbPath, err)
		}
		diskdb = rawdb.NewDatabase(kv)
		persistent = true
	}

	tdb := triedb.NewDatabase(diskdb, triedb.HashDefaults)
	db := state.NewDatabase(tdb, nil)

	// Validate that initialRoot is accessible in the backing store.
	if _, err := state.New(initialRoot, db); err != nil {
		return nil, fmt.Errorf("trie root %s not found in store: %w", initialRoot, err)
	}

	return &Store{diskdb: diskdb, db: db, root: initialRoot, persistent: persistent}, nil
}

// Commit applies the write sets of all valid transactions in the block to the MPT
// and returns the resulting StateRoot.
func (s *Store) Commit(ctx context.Context, block blocks.Block) (common.Hash, error) {
	var writes []blocks.KVWrite
	for _, tx := range block.Transactions {
		if !tx.Valid() {
			// MVCC-rejected: Fabric did not apply this transaction's write set.
			continue
		}
		for _, nsRWS := range tx.NsRWS {
			writes = append(writes, nsRWS.RWS.Writes...)
		}
	}
	return s.apply(writes, params.Rules{IsEIP158: true, IsCancun: true}, block.Number)
}

// CommitGenesis applies writes as the pre-state of block 0 and returns the
// resulting StateRoot. Unlike Commit it does not remove empty accounts (EIP-158):
// like an Ethereum genesis allocation, an empty pre-state account stays until a
// later block touches it.
func (s *Store) CommitGenesis(writes []blocks.KVWrite) (common.Hash, error) {
	return s.apply(writes, params.Rules{}, 0)
}

func (s *Store) apply(writes []blocks.KVWrite, rules params.Rules, number uint64) (common.Hash, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sdb, err := state.New(s.root, s.db)
	if err != nil {
		return common.Hash{}, fmt.Errorf("open state at root %s: %w", s.root, err)
	}
	for _, w := range writes {
		applyWrite(sdb, w)
	}

	newRoot, err := sdb.Commit(rules, number)
	if err != nil {
		return common.Hash{}, fmt.Errorf("state commit block %d: %w", number, err)
	}
	if s.persistent {
		if err := s.db.TrieDB().Commit(newRoot, false); err != nil {
			return common.Hash{}, fmt.Errorf("flush trie block %d: %w", number, err)
		}
	}
	s.root = newRoot
	return newRoot, nil
}

// State opens the world state at the current root, for reading.
func (s *Store) State() (*state.StateDB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return state.New(s.root, s.db)
}

// Root returns the current committed state root.
func (s *Store) Root() common.Hash {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

// Close releases underlying trie database resources.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.TrieDB().Close()
	s.diskdb.Close()
}

// applyWrite interprets a Fabric KVWrite and applies it to the StateDB, decoding
// it with the same codec the endorser encodes it with (fc.ParseStateKey and the
// fc.Decode functions). Keys that are not state keys are ignored.
func applyWrite(sdb *state.StateDB, w blocks.KVWrite) {
	key, ok := fc.ParseStateKey(w.Key)
	if !ok {
		return
	}
	// A delete decodes like an empty value: zero, or no code.
	value := w.Value
	if w.IsDelete {
		value = nil
	}

	if key.Storage {
		sdb.SetState(key.Address, key.Slot, fc.DecodeStorage(value))
		return
	}
	switch key.Field {
	case fc.FieldBalance:
		sdb.SetBalance(key.Address, fc.DecodeBalance(value), tracing.BalanceChangeUnspecified)
	case fc.FieldNonce:
		sdb.SetNonce(key.Address, fc.DecodeNonce(value), tracing.NonceChangeUnspecified)
	case fc.FieldCode:
		sdb.SetCode(key.Address, value, tracing.CodeChangeUnspecified)
	}
}
