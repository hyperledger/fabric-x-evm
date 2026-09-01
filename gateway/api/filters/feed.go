/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

const internalBuffer = 32

var feedLogger = flogging.MustGetLogger("gateway.api.filters")

// blockSink receives committed blocks off the synchronizer hot path.
type blockSink interface {
	onBlock(b blocks.Block)
}

// BlockFeed implements blocks.BlockHandler. Handle never blocks on slow RPC
// consumers: it does a non-blocking send into an internal buffer and returns.
// A dedicated drain goroutine fans blocks out to the registered sink and to
// any Subscribe callers (newHeads).
type BlockFeed struct {
	in   chan blocks.Block
	quit chan struct{}
	done chan struct{}

	sink atomic.Value // blockSink

	dropMu   sync.Mutex
	dropping bool

	subMu  sync.Mutex
	subs   map[uint64]*headSub
	nextID uint64
}

type headSub struct {
	id       uint64
	ch       chan blocks.Block
	dropping bool
}

// Subscription is a live BlockFeed consumer. Call Unsubscribe when done.
type Subscription struct {
	feed *BlockFeed
	id   uint64
	ch   <-chan blocks.Block
}

// Chan returns the receive channel for committed blocks.
func (s *Subscription) Chan() <-chan blocks.Block { return s.ch }

// Unsubscribe removes this subscriber. Safe to call more than once.
func (s *Subscription) Unsubscribe() {
	if s == nil || s.feed == nil {
		return
	}
	s.feed.unsubscribe(s.id)
}

// NewBlockFeed starts the drain goroutine. Call Close on shutdown.
func NewBlockFeed() *BlockFeed {
	f := &BlockFeed{
		in:   make(chan blocks.Block, internalBuffer),
		quit: make(chan struct{}),
		done: make(chan struct{}),
		subs: make(map[uint64]*headSub),
	}
	go f.drain()
	return f
}

// SetSink registers the consumer that receives blocks from the drain loop.
// Typically the FilterAPI. Safe to call once during wiring before traffic.
func (f *BlockFeed) SetSink(s blockSink) {
	f.sink.Store(s)
}

// Subscribe registers a buffered consumer. Delivery is non-blocking: if the
// buffer is full the block is dropped for that subscriber only.
func (f *BlockFeed) Subscribe(buffer int) *Subscription {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan blocks.Block, buffer)
	f.subMu.Lock()
	f.nextID++
	id := f.nextID
	f.subs[id] = &headSub{id: id, ch: ch}
	f.subMu.Unlock()
	return &Subscription{feed: f, id: id, ch: ch}
}

// SubscriberCount is the number of active Subscribe callers (for tests).
func (f *BlockFeed) SubscriberCount() int {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	return len(f.subs)
}

func (f *BlockFeed) unsubscribe(id uint64) {
	f.subMu.Lock()
	sub, ok := f.subs[id]
	if ok {
		delete(f.subs, id)
	}
	f.subMu.Unlock()
	if ok {
		close(sub.ch)
	}
}

// Handle enqueues the block without waiting for filter delivery.
func (f *BlockFeed) Handle(_ context.Context, b blocks.Block) error {
	select {
	case f.in <- b:
		f.clearDrop()
	default:
		f.noteDrop()
	}
	return nil
}

// Close stops the drain goroutine and waits for it to exit.
func (f *BlockFeed) Close() {
	select {
	case <-f.quit:
		return
	default:
		close(f.quit)
	}
	<-f.done
}

// Done is closed after Close finishes draining.
func (f *BlockFeed) Done() <-chan struct{} {
	return f.done
}

func (f *BlockFeed) drain() {
	defer close(f.done)
	for {
		select {
		case <-f.quit:
			for {
				select {
				case b := <-f.in:
					f.deliver(b)
				default:
					f.closeAllSubs()
					return
				}
			}
		case b := <-f.in:
			f.deliver(b)
		}
	}
}

func (f *BlockFeed) closeAllSubs() {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	for id, sub := range f.subs {
		close(sub.ch)
		delete(f.subs, id)
	}
}

func (f *BlockFeed) deliver(b blocks.Block) {
	if v := f.sink.Load(); v != nil {
		v.(blockSink).onBlock(b)
	}
	f.fanOut(b)
}

func (f *BlockFeed) fanOut(b blocks.Block) {
	f.subMu.Lock()
	defer f.subMu.Unlock()
	for _, s := range f.subs {
		select {
		case s.ch <- b:
			s.dropping = false
		default:
			if !s.dropping {
				s.dropping = true
				feedLogger.Warnf("newHeads subscriber %d falling behind; dropping notifications", s.id)
			}
		}
	}
}

func (f *BlockFeed) noteDrop() {
	f.dropMu.Lock()
	defer f.dropMu.Unlock()
	if f.dropping {
		return
	}
	f.dropping = true
	feedLogger.Warnf("block filter feed falling behind; dropping notifications until consumers catch up")
}

func (f *BlockFeed) clearDrop() {
	f.dropMu.Lock()
	defer f.dropMu.Unlock()
	f.dropping = false
}
