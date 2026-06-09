/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"google.golang.org/protobuf/proto"
)

// Ledger stores mock blocks and transaction event batches in memory.
type Ledger struct {
	mu sync.RWMutex

	nextBlockNum uint64
	blocks       []*common.Block
	eventBatches []*committerpb.TxEventBatch

	blockSubs map[chan *common.Block]any
	eventSubs map[chan *committerpb.TxEventBatch]any
}

// NewLedger returns an empty ledger with block numbering starting at 1.
func NewLedger() *Ledger {
	return &Ledger{
		nextBlockNum: 1,
		blockSubs:    map[chan *common.Block]any{},
		eventSubs:    map[chan *committerpb.TxEventBatch]any{},
	}
}

// Height returns the next block number, matching Fabric ledger height semantics.
func (l *Ledger) Height() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.nextBlockNum
}

// Commit appends envs as one mock block and returns its transaction event batch.
func (l *Ledger) Commit(envs []*common.Envelope) *committerpb.TxEventBatch {
	l.mu.Lock()
	defer l.mu.Unlock()

	blockNum := l.nextBlockNum
	l.nextBlockNum++

	block := &common.Block{
		Header: &common.BlockHeader{Number: blockNum},
		Data:   &common.BlockData{Data: make([][]byte, 0, len(envs))},
		Metadata: &common.BlockMetadata{
			Metadata: make([][]byte, int(common.BlockMetadataIndex_TRANSACTIONS_FILTER)+1),
		},
	}

	parsed := make([]ParsedEnvelope, 0, len(envs))
	txFilter := make([]byte, 0, len(envs))
	for _, env := range envs {
		bytes, _ := proto.Marshal(env)
		block.Data.Data = append(block.Data.Data, bytes)
		p := ParseEnvelope(env)
		parsed = append(parsed, p)
		txFilter = append(txFilter, byte(p.Status))
	}
	block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER] = txFilter

	events := make([]*committerpb.TxEvent, 0, len(envs))
	for txNum, p := range parsed {
		if p.TxID == "" {
			continue
		}
		ref := &committerpb.TxRef{TxId: p.TxID, BlockNum: blockNum, TxNum: uint32(txNum)}
		if !p.SkipEvent {
			events = append(events, &committerpb.TxEvent{
				Ref:          ref,
				Status:       p.Status,
				Namespaces:   p.Namespaces,
				Endorsements: p.Endorsements,
			})
		}
	}

	batch := &committerpb.TxEventBatch{BlockNumber: blockNum, Events: events}
	l.blocks = append(l.blocks, block)
	l.eventBatches = append(l.eventBatches, batch)
	l.notifyLocked(block, batch)
	return cloneEventBatch(batch)
}

// SubscribeBlocksFrom returns existing blocks from start plus a live block stream.
func (l *Ledger) SubscribeBlocksFrom(start uint64) ([]*common.Block, <-chan *common.Block, func()) {
	chForNewBlocks := make(chan *common.Block, 1024)

	// Snapshot and subscribe under one lock so Deliver cannot miss a block
	// committed between catch-up and live streaming.
	l.mu.Lock()
	existingBlocks := make([]*common.Block, 0, len(l.blocks))
	for _, block := range l.blocks {
		if block.GetHeader().GetNumber() >= start {
			existingBlocks = append(existingBlocks, proto.Clone(block).(*common.Block))
		}
	}
	l.blockSubs[chForNewBlocks] = nil
	l.mu.Unlock()

	cancel := func() {
		l.mu.Lock()
		delete(l.blockSubs, chForNewBlocks)
		close(chForNewBlocks)
		l.mu.Unlock()
	}
	return existingBlocks, chForNewBlocks, cancel
}

// SubscribeEventBatches returns existing transaction batches plus a live batch stream.
func (l *Ledger) SubscribeEventBatches() ([]*committerpb.TxEventBatch, <-chan *committerpb.TxEventBatch, func()) {
	chForNewBatches := make(chan *committerpb.TxEventBatch, 1024)

	// Same atomic catch-up/live handoff as blocks; filtering belongs to the
	// committer service because it is request-specific.
	l.mu.Lock()
	existingBatches := make([]*committerpb.TxEventBatch, 0, len(l.eventBatches))
	for _, batch := range l.eventBatches {
		existingBatches = append(existingBatches, cloneEventBatch(batch))
	}
	l.eventSubs[chForNewBatches] = nil
	l.mu.Unlock()

	cancel := func() {
		l.mu.Lock()
		delete(l.eventSubs, chForNewBatches)
		close(chForNewBatches)
		l.mu.Unlock()
	}
	return existingBatches, chForNewBatches, cancel
}

func cloneEventBatch(batch *committerpb.TxEventBatch) *committerpb.TxEventBatch {
	if batch == nil {
		return nil
	}
	return proto.Clone(batch).(*committerpb.TxEventBatch)
}

func (l *Ledger) notifyLocked(block *common.Block, batch *committerpb.TxEventBatch) {
	for ch := range l.blockSubs {
		select {
		case ch <- proto.Clone(block).(*common.Block):
		default:
		}
	}
	for ch := range l.eventSubs {
		select {
		case ch <- cloneEventBatch(batch):
		default:
		}
	}
}

// ParsedEnvelope captures fields extracted from an orderer envelope.
type ParsedEnvelope struct {
	TxID         string
	Status       committerpb.Status
	Namespaces   []*applicationpb.TxNamespace
	Endorsements []*applicationpb.Endorsements
	SkipEvent    bool
	Err          error
}

// ParseEnvelope extracts transaction metadata and validation status from env.
func ParseEnvelope(env *common.Envelope) ParsedEnvelope {
	if env == nil {
		return ParsedEnvelope{Status: committerpb.Status_MALFORMED_BAD_ENVELOPE, Err: fmt.Errorf("nil envelope")}
	}

	payload := &common.Payload{}
	if err := proto.Unmarshal(env.Payload, payload); err != nil {
		return ParsedEnvelope{Status: committerpb.Status_MALFORMED_BAD_ENVELOPE, Err: fmt.Errorf("payload: %w", err)}
	}

	if payload.Header == nil {
		return ParsedEnvelope{Status: committerpb.Status_MALFORMED_MISSING_TX_ID, Err: fmt.Errorf("missing header")}
	}

	chdr := &common.ChannelHeader{}
	if err := proto.Unmarshal(payload.Header.ChannelHeader, chdr); err != nil {
		return ParsedEnvelope{Status: committerpb.Status_MALFORMED_MISSING_TX_ID, Err: fmt.Errorf("channel header: %w", err)}
	}

	result := ParsedEnvelope{TxID: chdr.TxId, Status: committerpb.Status_COMMITTED}
	if result.TxID == "" {
		result.Status = committerpb.Status_MALFORMED_MISSING_TX_ID
		result.Err = fmt.Errorf("missing tx id")
		return result
	}

	if chdr.Type != int32(common.HeaderType_MESSAGE) {
		result.SkipEvent = true
		return result
	}

	tx := &applicationpb.Tx{}
	if err := proto.Unmarshal(payload.Data, tx); err != nil {
		result.Status = committerpb.Status_MALFORMED_BAD_ENVELOPE_PAYLOAD
		result.Err = fmt.Errorf("transaction: %w", err)
		return result
	}

	result.Namespaces = tx.Namespaces
	result.Endorsements = tx.Endorsements
	return result
}
