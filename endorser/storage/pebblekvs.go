/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/ethdb"
	gethpebble "github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// PebbleKVS is a persistent, MVCC key-value store that implements the KVS
// interface as a drop-in replacement for the in-memory LightKVS. Unlike
// LightKVS it survives process restarts: every block's writes are committed
// atomically alongside a persisted block-number checkpoint, so on reopen the
// store resumes from the last committed block.
//
// # Storage model (latest value, plus a bounded history window)
//
// The current value of a key lives under a version-free key:
//
//	'd' | be32(len(fullKey)) | fullKey
//
// where fullKey is the "namespace:key" string used throughout the endorser.
// Because the key carries no version, a rewrite overwrites, and reading the
// current value is a single point lookup that pebble's block cache and bloom
// filters serve directly — no iterator is allocated on the read path.
//
// When a write supersedes an existing value, the old record is retained under
//
//	'h' | be32(len(fullKey)) | fullKey | be64(^block) | be64(^tx)
//
// with ^x == math.MaxUint64-x, so a forward scan from be64(^wanted) lands on the
// highest retained version at or below `wanted` in one Next(). That is what serves
// time-travel reads inside the window.
//
// Retention is bounded by historySize blocks, mirroring LightKVS's snapshot ring.
// Each block also writes an index
//
//	'i' | be64(block)  ->  the history keys that block created
//
// so committing block N can delete the entries that fell out of the window at
// block N-historySize without scanning the store. Reads older than the window are
// refused rather than answered with a newer value (see NewSnapshot).
//
// This layout replaced an insert-only one that kept every version forever. That
// design made the store grow without bound as keys were rewritten, and measurably
// slowed reads as it grew: rewriting a fixed 100k-key set 50 times took reads from
// 184K/s to 44K/s and the store from 16 MiB to 206 MiB, where this layout holds
// ~212K/s and ~80 MiB.
//
// A reserved meta key 'm'|"block" holds be64(lastCommittedBlock) and is written
// in the same batch as the block's data keys, giving an atomic checkpoint for
// crash recovery.
//
// # Durability
//
// Each block commits as one atomic pebble batch, so a crash leaves either the
// whole block applied or none of it. Commits use geth's default (NoSync): the
// data survives a process restart, but an OS crash or power loss can lose the
// most recently committed blocks. Those are recoverable from the ledger — the
// persisted checkpoint tells the synchronizer where to resume — so this is safe
// for the endorser, whose state is always reconstructible from committed blocks.
//
// # Parity notes vs LightKVS
//
//   - Time-travel reads: a snapshot at block N returns, for each key, the record
//     with the highest version whose block <= N (mirroring the sqlite-backed
//     VersionedDB, see fabric-x-sdk state.VersionedDB.Get), for any N inside the
//     retained window. Outside it, NewSnapshot errors — the same behaviour as
//     LightKVS, which also refuses blocks evicted from its ring. Serving arbitrary
//     historical blocks (an archive node) is deliberately not supported.
//   - Deletes are stored as tombstone records (IsDelete=true), matching the
//     VersionedDB, rather than removing the key as LightKVS does. The execution
//     layer already treats IsDelete=true records as absent (nil value, nil
//     version in the read-set), so this is behaviorally equivalent for callers
//     but preserves the version chain for MVCC validation.
//   - Block height tracks the ledger exactly: a block that yields no writes still
//     advances the persisted checkpoint, where LightKVS leaves its height at the
//     last block that contained writes. See Handle.
//   - The write path is idempotent per write, not just per block: re-applying an
//     already-stored (key, block, tx) write is a no-op if the content matches,
//     and an error if it doesn't.
//   - Version numbers mirror the VersionedDB, not LightKVS: each write's Version
//     is MAX(version)+1 for its key, so multiple writes to one key within a block
//     get consecutive versions and the counter is monotonic across a tombstone
//     (a rewrite after a delete does not reset to 0). LightKVS instead shares one
//     version across a block's writes to a key and resets after a delete. Because
//     the fabric-x read-set carries this Version to the committer for MVCC
//     validation, matching the VersionedDB — the committer's own store — is the
//     correct behavior; the two divergences from LightKVS are intentional.
type PebbleKVS struct {
	db *gethpebble.Database

	// historySize is how many recent blocks of previous versions are retained,
	// mirroring LightKVS's snapshot ring. Older versions are pruned on commit, so
	// the store no longer grows without bound as keys are rewritten.
	historySize uint64

	// oldestRetained is the lowest block a historical read can still be answered
	// for; everything below it has been pruned. Seeded from metaOldestKey on open.
	oldestRetained atomic.Uint64

	// prunedTo carries the new oldestRetained from pruneBlock to after the batch
	// commits, so readers never see a tightened bound for a write that failed.
	// Written only under writeMu.
	prunedTo uint64

	// snapMu guards liveSnapshots.
	snapMu sync.Mutex
	// liveSnapshots counts open readers by the height each is pinned to. Prune must
	// not delete history a live reader could still need: isolation here is
	// re-derived per Get from the history entries, so unlike the old insert-only
	// layout the data a reader depends on can otherwise be deleted underneath it.
	// A reader pinned at H is served by entries indexed under blocks > H, so
	// retaining index blocks above the lowest live pin is sufficient.
	liveSnapshots map[uint64]int

	// writeMu serializes Update calls. The KVS contract already assumes a
	// single writer, but the per-key version lookup performs a read against
	// committed state that must not interleave with a concurrent commit.
	writeMu sync.Mutex

	// currentBlock is the last committed block number, cached in memory for
	// cheap BlockNumber() reads and seeded from the meta checkpoint on open.
	currentBlock atomic.Uint64

	// hasCheckpoint records whether currentBlock reflects an actual commit, as
	// opposed to the zero value of a fresh store. currentBlock alone cannot
	// distinguish the two, and the replay guard in commitBlock would otherwise
	// treat block 0 of a fresh store as already applied.
	hasCheckpoint atomic.Bool
}

var pebbleLogger = flogging.MustGetLogger("endorser.storage.pebblekvs")

const (
	// prefixData tags the latest-value key for each state key.
	prefixData = 'd'
	// prefixHistory tags retained previous versions, for in-window time travel.
	prefixHistory = 'h'
	// prefixBlockIndex tags the per-block list of keys written, used to prune.
	prefixBlockIndex = 'i'
	// prefixMeta tags reserved metadata keys.
	prefixMeta = 'm'

	// defaultHistorySize is the retained time-travel window when the caller does
	// not specify one. It matches the value the endorser config defaults to.
	defaultHistorySize = 128
)

// metaBlockKey holds the last committed block number.
var metaBlockKey = []byte{prefixMeta, 'b', 'l', 'o', 'c', 'k'}

// metaOldestKey holds the lowest block number still answerable by a historical
// read. It is persisted rather than derived from historySize, because what has
// actually been pruned depends on the historySize in force at the time — deriving
// it would let a reopen with a larger window accept reads whose versions are gone.
var metaOldestKey = []byte{prefixMeta, 'o', 'l', 'd', 'e', 's', 't'}

// Compile-time assertion that PebbleKVS satisfies the KVS interface.
var _ KVS = (*PebbleKVS)(nil)

// NewPebbleKVS opens (or creates) a pebble-backed KVS rooted at dir.
//
// historySize is the number of recent blocks whose previous versions are retained
// for time-travel reads, matching LightKVS's ring-buffer semantics. Versions older
// than that window are pruned on commit, which is what bounds the store's growth.
// A non-positive value falls back to defaultHistorySize.
func NewPebbleKVS(dir string, historySize int) (*PebbleKVS, error) {
	if dir == "" {
		return nil, errors.New("pebble kvs requires a non-empty data directory (connection-string)")
	}
	if historySize <= 0 {
		historySize = defaultHistorySize
	}

	// cache is in MB; handles is the max open-file budget. Modest defaults
	// that keep the working set in memory without a large FD footprint.
	db, err := gethpebble.New(dir, 128, 512, "endorser/pebblekvs", false)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db at %q: %w", dir, err)
	}

	kvs := &PebbleKVS{db: db, historySize: uint64(historySize), liveSnapshots: map[uint64]int{}}

	// Recover the last committed block number from the atomic checkpoint.
	block, found, err := kvs.readMetaBlock()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read block checkpoint: %w", err)
	}
	kvs.currentBlock.Store(block)
	kvs.hasCheckpoint.Store(found)

	oldest, err := kvs.readMetaUint64(metaOldestKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read oldest-retained marker: %w", err)
	}
	kvs.oldestRetained.Store(oldest)
	if found {
		pebbleLogger.Infof("opened pebble kvs at %q, resuming from block %d", dir, block)
	} else {
		pebbleLogger.Infof("opened fresh pebble kvs at %q", dir)
	}

	return kvs, nil
}

// readMetaBlock returns the persisted last-committed block and whether a
// checkpoint was found at all. The two are distinct: a fresh store and a store
// whose last commit was block 0 both report block 0.
func (p *PebbleKVS) readMetaBlock() (uint64, bool, error) {
	raw, err := p.db.Get(metaBlockKey)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(raw) != 8 {
		return 0, false, fmt.Errorf("corrupt block checkpoint: got %d bytes, want 8", len(raw))
	}
	return binary.BigEndian.Uint64(raw), true, nil
}

// commitBlock applies a single block's writes (possibly none) and advances
// the persisted checkpoint to blockNum, in one atomic pebble batch.
//
// A write whose (key, block, tx) coordinate the store still holds — as the key's
// latest value, or as a previous one retained in the history window — is a
// replay: it's verified against the incoming write and skipped rather than
// re-versioned, erroring if the content differs. Only a coordinate below the
// retained window cannot be verified, its record having been pruned.
//
// The checkpoint advances monotonically, independent of the per-write check:
// a block at or below it may still be processed to verify its writes, but
// never moves the checkpoint backward.
func (p *PebbleKVS) commitBlock(blockNum uint64, updates []KeyValueVersion) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	batch := p.db.NewBatch()
	defer batch.Close()

	// advance is false only for a genuine replay (blockNum at or below the
	// persisted checkpoint).
	advance := !p.hasCheckpoint.Load() || blockNum > p.currentBlock.Load()

	// nextVersion holds the version to assign to the *next* new write of each
	// key within this batch.
	nextVersion := make(map[string]uint64, len(updates))

	// touched collects the history keys this block created, so the prune that runs
	// historySize blocks later can delete exactly those entries.
	var touched []string

	for i := range updates {
		u := &updates[i]

		// sameness check on existing writes
		if !advance {
			rec, found, err := p.storedWrite(u.Key, u.BlockNum, u.TxNum)
			if err != nil {
				return fmt.Errorf("failed to check existing write for %q: %w", u.Key, err)
			}
			if !found {
				// A write from a block still inside the history window must be
				// there, either as the key's latest value or as a retained
				// previous one; if it isn't, we have a consistency issue. Below
				// the window it has been pruned, so nothing is left to verify the
				// replay against and it is accepted as already applied.
				if u.BlockNum >= p.oldestRetained.Load() {
					return fmt.Errorf(
						"missing write for %q at block=%d tx=%d: block %d is already checkpointed but this write was never committed for it",
						u.Key, u.BlockNum, u.TxNum, blockNum)
				}
				continue
			}
			if !bytes.Equal(rec.Value, u.Value) || rec.IsDelete != u.IsDelete || rec.TxID != u.TxID {
				return fmt.Errorf(
					"conflicting write for %q at block=%d tx=%d: existing (is_delete=%v tx_id=%s) differs from replayed (is_delete=%v tx_id=%s)",
					u.Key, u.BlockNum, u.TxNum, rec.IsDelete, rec.TxID, u.IsDelete, u.TxID)
			}
			continue // identical replay: already stored, don't re-version it
		}

		version, seen := nextVersion[u.Key]
		if !seen {
			latest, err := p.latestVersion(u.Key)
			if err != nil {
				return fmt.Errorf("failed to read latest version for %q: %w", u.Key, err)
			}
			version = uint64(latest + 1) // latest is -1 for a fresh key → version 0
		}
		nextVersion[u.Key] = version + 1

		value := encodeRecord(record{
			BlockNum: u.BlockNum,
			TxNum:    u.TxNum,
			Version:  version,
			TxID:     u.TxID,
			IsDelete: u.IsDelete,
			Value:    u.Value,
		})

		// Before overwriting, retain the record this write supersedes so reads
		// inside the history window can still see it. Only the first write to a
		// key in this block needs to do so: later writes in the same block
		// supersede a value that was itself created in this block.
		if !seen {
			if prev, err := p.db.Get(dataKey(u.Key)); err == nil {
				prevRec, derr := decodeRecord(prev)
				if derr != nil {
					return fmt.Errorf("failed to decode superseded record for %q: %w", u.Key, derr)
				}
				hk := historyKey(u.Key, prevRec.BlockNum, prevRec.TxNum)
				if err := batch.Put(hk, prev); err != nil {
					return fmt.Errorf("failed to stage history for %q: %w", u.Key, err)
				}
				touched = append(touched, string(hk))
			} else if !errors.Is(err, pebble.ErrNotFound) {
				return fmt.Errorf("failed to read superseded record for %q: %w", u.Key, err)
			}
		}

		if err := batch.Put(dataKey(u.Key), value); err != nil {
			return fmt.Errorf("failed to stage write for %q: %w", u.Key, err)
		}
	}

	// Index the keys whose previous version this block pushed into history, so the
	// prune below can find them again without scanning the store.
	if len(touched) > 0 {
		if err := batch.Put(blockIndexKey(blockNum), encodeKeyList(touched)); err != nil {
			return fmt.Errorf("failed to stage block index: %w", err)
		}
	}

	// Drop the versions that just fell out of the window. This is what keeps the
	// store from growing with rewrite count.
	//
	// snapMu is held from here through the marker publish below so that pruning and
	// snapshot admission cannot interleave; see pruneBlock.
	p.snapMu.Lock()
	defer p.snapMu.Unlock()
	if err := p.pruneBlock(batch, blockNum); err != nil {
		return err
	}

	if advance {
		// Checkpoint the block number in the same batch for an atomic commit.
		if err := batch.Put(metaBlockKey, u64be(blockNum)); err != nil {
			return fmt.Errorf("failed to stage block checkpoint: %w", err)
		}
	}

	if err := batch.Write(); err != nil {
		// The staged marker never landed, so drop the pending in-memory update too.
		p.prunedTo = 0
		return fmt.Errorf("failed to commit block %d: %w", blockNum, err)
	}

	if advance {
		p.currentBlock.Store(blockNum)
		p.hasCheckpoint.Store(true)
	}
	if p.prunedTo > 0 {
		p.oldestRetained.Store(p.prunedTo)
		p.prunedTo = 0
	}
	return nil
}

// pruneBlock deletes the retained versions that leave the history window when
// blockNum is committed, plus the index entry that listed them. The departing
// block is the one historySize blocks back: with historySize=128 and a commit of
// block 200, block 72's superseded values are no longer reachable by any in-window
// read, so they go.
//
// Staged into the caller's batch so pruning is atomic with the commit — a crash
// cannot leave the index describing entries that are already gone.
func (p *PebbleKVS) pruneBlock(batch ethdb.Batch, blockNum uint64) error {
	// Retain historySize historical blocks plus the current one, so the readable
	// range is [blockNum-historySize, blockNum] — the same count LightKVS keeps in
	// its ring for the same historySize.
	if blockNum < p.historySize+1 {
		return nil // window not full yet; nothing has aged out
	}
	cutoff := blockNum - p.historySize - 1 // every index at or below this has aged out

	// Never prune below an open reader: its history entries are what make it
	// isolated. A long-lived reader therefore holds history back, which is the
	// usual cost of an MVCC read view.
	//
	// The caller holds snapMu for the whole commit, so a reader cannot be admitted
	// between this sample and the marker being published — otherwise it could pass
	// NewSnapshot's check against a marker this commit is about to advance, and have
	// its history deleted underneath it.
	if pin := p.lowestLivePinLocked(); pin < cutoff {
		cutoff = pin
	}

	// Sweep the index in ascending block order rather than probing the single
	// block cutoff. Delivered block numbers are not guaranteed contiguous — a
	// ledger reset, channel join or replay wrap-around leaves gaps — and probing
	// one number would silently skip a gap and leak those entries forever.
	// Index keys are prefixBlockIndex|be64(block), so ascending key order is
	// ascending block order and the scan stops at the first live block.
	it := p.db.NewIterator([]byte{prefixBlockIndex}, nil)
	defer it.Release()

	for it.Next() {
		k := it.Key()
		if len(k) != 1+8 {
			return fmt.Errorf("corrupt block index key of length %d", len(k))
		}
		block := binary.BigEndian.Uint64(k[1:])
		if block > cutoff {
			break
		}
		histKeys, err := decodeKeyList(it.Value())
		if err != nil {
			return fmt.Errorf("block index %d: %w", block, err)
		}
		for _, hk := range histKeys {
			if err := batch.Delete([]byte(hk)); err != nil {
				return fmt.Errorf("failed to stage history delete for block %d: %w", block, err)
			}
		}
		if err := batch.Delete(blockIndexKey(block)); err != nil {
			return fmt.Errorf("failed to stage index delete for block %d: %w", block, err)
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("failed to scan block index: %w", err)
	}

	// Record how far history now reaches, in the same batch, so the bound survives
	// a restart and a later change to historySize. The marker only ever moves
	// forward: enlarging historySize shrinks cutoff, and letting the marker follow
	// it back down would re-accept reads whose versions are already gone.
	oldest := cutoff + 1
	if cur := p.oldestRetained.Load(); oldest <= cur {
		return nil
	}
	if err := batch.Put(metaOldestKey, u64be(oldest)); err != nil {
		return fmt.Errorf("failed to stage oldest-retained marker: %w", err)
	}
	p.prunedTo = oldest
	return nil
}

// storedWrite returns the record this store holds for the write at (fullKey,
// block, tx), looking first at the key's latest value and then at the history
// window it is pushed into once superseded. found is false when neither holds
// it, which for a coordinate below oldestRetained only means it was pruned.
func (p *PebbleKVS) storedWrite(fullKey string, block, tx uint64) (*blocks.WriteRecord, bool, error) {
	raw, err := p.db.Get(dataKey(fullKey))
	if err == nil {
		rec, derr := decodeRecord(raw)
		if derr != nil {
			return nil, false, derr
		}
		if rec.BlockNum == block && rec.TxNum == tx {
			return rec, true, nil
		}
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return nil, false, err
	}

	raw, err = p.db.Get(historyKey(fullKey, block, tx))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	rec, err := decodeRecord(raw)
	if err != nil {
		return nil, false, err
	}
	return rec, true, nil
}

// latestVersion returns the version of the committed record for fullKey, or -1 if
// the key has never been written. One point lookup — with a single record per key
// there is nothing to scan.
func (p *PebbleKVS) latestVersion(fullKey string) (int64, error) {
	raw, err := p.db.Get(dataKey(fullKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return -1, nil
		}
		return 0, err
	}
	rec, err := decodeRecord(raw)
	if err != nil {
		return 0, err
	}
	return int64(rec.Version), nil
}

// NewSnapshot returns a read view of the store as of blockNumber. nil means the
// latest committed block at call time, pinning the view so later commits
// (higher block numbers) are naturally excluded. A non-nil value is that exact
// height (including 0 for genesis). Any historical block is serviceable; unlike
// LightKVS this never errors for an "evicted" block.
func (p *PebbleKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	head := p.currentBlock.Load()
	bn := head
	if blockNumber != nil {
		bn = *blockNumber
	}

	// Register the pin and validate under the same lock a commit holds while it
	// prunes, so a reader cannot be admitted against a marker that is concurrently
	// being advanced past it.
	//
	// Reads older than what is still retained cannot be answered correctly: the
	// versions they need have been pruned. Erroring matches LightKVS, which also
	// refuses blocks evicted from its ring, rather than silently reporting a key as
	// absent. The bound comes from the persisted marker, not from historySize, so
	// reopening with a larger window does not start accepting unanswerable reads.
	p.snapMu.Lock()
	if oldest := p.oldestRetained.Load(); bn < head && bn < oldest {
		p.snapMu.Unlock()
		return nil, fmt.Errorf("snapshot not found for block number %d (retained history starts at %d, head is %d)",
			bn, oldest, head)
	}
	p.liveSnapshots[bn]++
	p.snapMu.Unlock()

	return &pebbleSnapshot{
		db:        p.db,
		owner:     p,
		lastBlock: bn,
	}, nil
}

// Get returns the record for (namespace, key) as of lastBlock, or nil if no
// such record exists at or before that block. A lastBlock of 0 resolves to the
// latest committed block (legacy Get convention).
func (p *PebbleKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r, err := p.NewSnapshot(blockRefFromLastBlock(lastBlock))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Get(namespace, key)
}

// Handle implements blocks.BlockHandler: it extracts all valid transaction
// writes from a block and applies them atomically. Called by the synchronizer.
//
// A block that yields no writes — empty, config-only, or carrying nothing but
// invalid transactions — still advances the persisted checkpoint. LightKVS can
// leave its height behind in that case because its state does not outlive the
// process, but for a persistent store the checkpoint is the resume point: were
// it to lag, a restart would re-deliver every block since the last one that
// happened to contain writes, and the store's reported height would disagree
// with the block DB's, which does log empty blocks. Keeping height equal to
// ledger height is what lets this KVS serve as the synchronizer's height reader.
func (p *PebbleKVS) Handle(ctx context.Context, b blocks.Block) error {
	var updates []KeyValueVersion
	for _, tx := range b.Transactions {
		collectWrites(&updates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid)
	}
	return p.commitBlock(b.Number, updates)
}

// BlockNumber returns the last committed block number.
func (p *PebbleKVS) BlockNumber(ctx context.Context) (uint64, error) {
	return p.currentBlock.Load(), nil
}

// Close closes the underlying pebble database.
func (p *PebbleKVS) Close() error {
	return p.db.Close()
}

// pebbleSnapshot is a point-in-time read view pinned to a block number. It
// implements execution.ReadStore.
//
// Isolation is not inherent to the storage layout: the current value is
// overwritten in place, so it is re-derived per read (see Get — take the current
// record only when it is not newer than the pinned height, else fall back to
// history). The entries that fallback depends on are protected for the reader's
// lifetime by the pin registered in NewSnapshot and dropped in Close, so a reader
// that is never closed holds history back.
type pebbleSnapshot struct {
	db *gethpebble.Database
	// owner is notified on Close so prune can resume below this view's height.
	owner *PebbleKVS
	// lastBlock is the height this view is pinned to.
	lastBlock uint64
	// closed is atomic because Close may be called concurrently; releasing the pin
	// twice would drop a pin another reader at the same height still holds.
	closed atomic.Bool
}

// Get returns the record for (namespace, key) as of the snapshot's block, or
// nil if none exists at or before it. Tombstone records are returned as-is
// (IsDelete=true); the execution layer treats them as absent.
func (s *pebbleSnapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if s.closed.Load() {
		return nil, errors.New("reader is closed")
	}

	fullKey := namespace + ":" + key

	// Fast path: one point lookup. The latest record answers the read whenever it
	// was written at or before the pinned block, which is the common case.
	//
	// The block check is not optional. Because a rewrite now overwrites in place,
	// the record under dataKey can belong to a block committed *after* this
	// snapshot was taken — the endorser simulates while the synchronizer commits.
	// Without the check a snapshot would observe future writes, and the read-set it
	// builds goes to the committer for MVCC validation.
	rec, err := s.latest(namespace, key, fullKey)
	if err != nil {
		return nil, err
	}
	if rec != nil && rec.BlockNum <= s.lastBlock {
		return rec, nil
	}

	// The current value is newer than this view (or the key is absent). Whichever
	// block superseded it copied the previous record into history in the same
	// atomic batch, so the value as of lastBlock is there if it exists at all.
	it := s.db.NewIterator(historyPrefix(fullKey), u64be(^s.lastBlock))
	defer it.Release()
	if it.Next() {
		hrec, err := decodeRecord(it.Value())
		if err != nil {
			return nil, err
		}
		hrec.Namespace = namespace
		hrec.Key = key
		return hrec, nil
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	// Nothing at or below lastBlock: the key did not exist yet at that height.
	return nil, nil
}

// latest reads the current record with a single point lookup — the common path,
// and the reason the version is no longer part of the key.
func (s *pebbleSnapshot) latest(namespace, key, fullKey string) (*blocks.WriteRecord, error) {
	raw, err := s.db.Get(dataKey(fullKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	rec, err := decodeRecord(raw)
	if err != nil {
		return nil, err
	}
	rec.Namespace = namespace
	rec.Key = key
	return rec, nil
}

// Close releases the snapshot. After Close the snapshot cannot be used.
func (s *pebbleSnapshot) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil // already closed; the pin was released exactly once
	}
	if s.owner != nil {
		s.owner.releaseSnapshot(s.lastBlock)
	}
	return nil
}

// --- key/value encoding ---------------------------------------------------

// record is the decoded form of a stored value. Namespace and Key are not
// stored (they are recoverable from the query) and are filled in by the reader.
type record struct {
	BlockNum uint64
	TxNum    uint64
	Version  uint64
	TxID     string
	IsDelete bool
	Value    []byte
}

// releaseSnapshot drops a reader's pin, letting prune advance past its height.
func (p *PebbleKVS) releaseSnapshot(bn uint64) {
	p.snapMu.Lock()
	defer p.snapMu.Unlock()
	if n := p.liveSnapshots[bn]; n <= 1 {
		delete(p.liveSnapshots, bn)
	} else {
		p.liveSnapshots[bn] = n - 1
	}
}

// lowestLivePinLocked returns the lowest height any open reader is pinned to, or
// math.MaxUint64 when there are none. Caller must hold snapMu.
func (p *PebbleKVS) lowestLivePinLocked() uint64 {
	lowest := uint64(math.MaxUint64)
	for bn := range p.liveSnapshots {
		if bn < lowest {
			lowest = bn
		}
	}
	return lowest
}

// readMetaUint64 reads an 8-byte big-endian meta value, returning 0 when absent.
func (p *PebbleKVS) readMetaUint64(key []byte) (uint64, error) {
	raw, err := p.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("corrupt meta value for %q: got %d bytes, want 8", key, len(raw))
	}
	return binary.BigEndian.Uint64(raw), nil
}

// dataPrefix returns the unambiguous per-key prefix: 'd' | be32(len) | fullKey.
// The returned slice has len == cap so geth's NewIterator, which does
// append(prefix, start...), reallocates instead of clobbering our buffer.
func dataPrefix(fullKey string) []byte {
	b := make([]byte, 0, 1+4+len(fullKey))
	b = append(b, prefixData)
	b = binary.BigEndian.AppendUint32(b, uint32(len(fullKey)))
	b = append(b, fullKey...)
	return b
}

// dataKey returns the key for a write. There is no version suffix: a key holds
// exactly one record, so a rewrite overwrites it. This makes a read a point lookup
// (see pebbleSnapshot.Get) and keeps the store from growing with rewrite count.
func dataKey(fullKey string) []byte {
	return dataPrefix(fullKey)
}

// historyPrefix returns the per-key prefix for retained previous versions.
func historyPrefix(fullKey string) []byte {
	b := make([]byte, 0, 1+4+len(fullKey))
	b = append(b, prefixHistory)
	b = binary.BigEndian.AppendUint32(b, uint32(len(fullKey)))
	b = append(b, fullKey...)
	return b
}

// historyKey returns the key for one retained version. (block, tx) are encoded
// descending so a forward scan from be64(^wanted) lands on the highest version at
// or below `wanted` — the same trick the single-record layout no longer needs, but
// which still makes an in-window historical read one Next().
func historyKey(fullKey string, block, tx uint64) []byte {
	b := historyPrefix(fullKey)
	b = binary.BigEndian.AppendUint64(b, ^block)
	b = binary.BigEndian.AppendUint64(b, ^tx)
	return b
}

// blockIndexKey returns the key holding the list of state keys written by a block.
// Pruning needs to know which keys a departing block touched without scanning the
// whole store.
func blockIndexKey(block uint64) []byte {
	b := make([]byte, 0, 1+8)
	b = append(b, prefixBlockIndex)
	b = binary.BigEndian.AppendUint64(b, block)
	return b
}

// encodeKeyList / decodeKeyList store a block's written keys as a sequence of
// uvarint-length-prefixed strings.
func encodeKeyList(keys []string) []byte {
	buf := make([]byte, 0, 16*len(keys))
	for _, k := range keys {
		buf = binary.AppendUvarint(buf, uint64(len(k)))
		buf = append(buf, k...)
	}
	return buf
}

func decodeKeyList(raw []byte) ([]string, error) {
	var keys []string
	for pos := 0; pos < len(raw); {
		n, used := binary.Uvarint(raw[pos:])
		if used <= 0 {
			return nil, errors.New("corrupt block index: key length")
		}
		pos += used
		if pos+int(n) > len(raw) {
			return nil, errors.New("corrupt block index: key overruns value")
		}
		keys = append(keys, string(raw[pos:pos+int(n)]))
		pos += int(n)
	}
	return keys, nil
}

// u64be returns the 8-byte big-endian encoding of v.
func u64be(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// record value layout (compact, varint-based; encoding cost is on the hot path
// so JSON is deliberately avoided):
//
//	flags byte      : bit0 = IsDelete, bit1 = value is non-nil
//	uvarint BlockNum
//	uvarint TxNum
//	uvarint Version
//	uvarint len(TxID) | TxID bytes
//	[ uvarint len(Value) | Value bytes ]   -- present iff value non-nil
const (
	flagIsDelete    = 1 << 0
	flagValueNonNil = 1 << 1
)

// encodeRecord serializes a record into a compact binary value.
func encodeRecord(r record) []byte {
	var flags byte
	if r.IsDelete {
		flags |= flagIsDelete
	}
	if r.Value != nil {
		flags |= flagValueNonNil
	}

	// Rough capacity estimate: flags + 3 uvarints + txid + value.
	buf := make([]byte, 0, 1+3*binary.MaxVarintLen64+len(r.TxID)+len(r.Value)+2*binary.MaxVarintLen64)
	buf = append(buf, flags)
	buf = binary.AppendUvarint(buf, r.BlockNum)
	buf = binary.AppendUvarint(buf, r.TxNum)
	buf = binary.AppendUvarint(buf, r.Version)
	buf = binary.AppendUvarint(buf, uint64(len(r.TxID)))
	buf = append(buf, r.TxID...)
	if r.Value != nil {
		buf = binary.AppendUvarint(buf, uint64(len(r.Value)))
		buf = append(buf, r.Value...)
	}
	return buf
}

// decodeRecord parses a stored value. The returned WriteRecord's Namespace and
// Key are left empty for the caller to populate. Value bytes are copied out so
// the result is safe to retain after the iterator advances.
func decodeRecord(raw []byte) (*blocks.WriteRecord, error) {
	if len(raw) < 1 {
		return nil, errors.New("corrupt record: empty value")
	}
	flags := raw[0]
	pos := 1

	blockNum, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: block num")
	}
	pos += n

	txNum, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: tx num")
	}
	pos += n

	version, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: version")
	}
	pos += n

	txIDLen, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: txid length")
	}
	pos += n
	if pos+int(txIDLen) > len(raw) {
		return nil, errors.New("corrupt record: txid overruns value")
	}
	txID := string(raw[pos : pos+int(txIDLen)])
	pos += int(txIDLen)

	var value []byte
	if flags&flagValueNonNil != 0 {
		valLen, n := binary.Uvarint(raw[pos:])
		if n <= 0 {
			return nil, errors.New("corrupt record: value length")
		}
		pos += n
		if pos+int(valLen) > len(raw) {
			return nil, errors.New("corrupt record: value overruns value")
		}
		// Copy so the result survives the next iterator step.
		value = make([]byte, valLen)
		copy(value, raw[pos:pos+int(valLen)])
	}

	return &blocks.WriteRecord{
		BlockNum: blockNum,
		TxNum:    txNum,
		Version:  version,
		Value:    value,
		IsDelete: flags&flagIsDelete != 0,
		TxID:     txID,
	}, nil
}
