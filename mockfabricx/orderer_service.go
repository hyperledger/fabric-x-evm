/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"google.golang.org/grpc"
)

// ordererService owns block cutting because AtomicBroadcast is the only
// producer in this mock backend.
type ordererServiceConfig struct {
	maxTxPerBlock int
	blockTimeout  time.Duration
	queueSize     int
}

type ordererService struct {
	orderer.UnimplementedAtomicBroadcastServer

	ledger        *Ledger
	in            chan *common.Envelope
	maxTxPerBlock int
	blockTimeout  time.Duration
}

func newOrdererService(ledger *Ledger, cfg ordererServiceConfig) *ordererService {
	if cfg.maxTxPerBlock <= 0 {
		cfg.maxTxPerBlock = 1
	}
	if cfg.queueSize <= 0 {
		cfg.queueSize = 65536
	}
	return &ordererService{
		ledger:        ledger,
		in:            make(chan *common.Envelope, cfg.queueSize),
		maxTxPerBlock: cfg.maxTxPerBlock,
		blockTimeout:  cfg.blockTimeout,
	}
}

// RegisterOrdererServices registers the AtomicBroadcast service.
func RegisterOrdererServices(s grpc.ServiceRegistrar, ordererServer orderer.AtomicBroadcastServer) {
	orderer.RegisterAtomicBroadcastServer(s, ordererServer)
}

func (s *ordererService) Broadcast(stream orderer.AtomicBroadcast_BroadcastServer) error {
	for {
		env, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := s.submit(stream.Context(), env); err != nil {
			return err
		}

		if err := stream.Send(&orderer.BroadcastResponse{Status: common.Status_SUCCESS}); err != nil {
			return err
		}
	}
}

func (s *ordererService) submit(ctx context.Context, env *common.Envelope) error {
	select {
	case s.in <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ordererService) run(ctx context.Context) error {
	var pending []*common.Envelope
	var tickerC <-chan time.Time

	if s.blockTimeout > 0 {
		ticker := time.NewTicker(s.blockTimeout)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	flush := func() {
		if len(pending) == 0 {
			return
		}
		s.ledger.Commit(pending)
		pending = nil
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return nil
		case env := <-s.in:
			if env == nil {
				return fmt.Errorf("nil envelope")
			}
			pending = append(pending, env)
			if len(pending) >= s.maxTxPerBlock || s.blockTimeout == 0 {
				flush()
				continue
			}
		case <-tickerC:
			flush()
		}
	}
}
