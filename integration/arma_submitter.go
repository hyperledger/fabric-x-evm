/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

// ArMASubmitter is a core.Submitter compatible with the ArMA orderer-router's pipelined
// broadcast protocol.
//
// The standard fabric-x-sdk FabricSubmitter uses the sequence:
//   Send(env) → CloseSend() → Recv()
//
// The ArMA orderer-router does not send a response after the client half-closes the stream
// (END_STREAM). It only sends a response while the stream is still fully open. This causes the
// standard submitter to receive io.EOF instead of the actual response.
//
// ArMASubmitter uses the correct sequence:
//   Send(env) → Recv() → CloseSend()
//
// This matches the pattern used by the orderer-loadgen, which keeps streams open and receives
// responses on the same open stream.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// armaOrderer is an orderer connection that uses the ArMA-compatible broadcast sequence.
type armaOrderer struct {
	conn   *grpc.ClientConn
	client orderer.AtomicBroadcastClient
	addr   string
}

func newArmaOrderer(conf network.OrdererConf) (*armaOrderer, error) {
	if err := conf.TLS.Validate(); err != nil {
		return nil, fmt.Errorf("orderer %s: invalid TLS config: %w", conf.Address, err)
	}

	host, _, err := net.SplitHostPort(conf.Address)
	if err != nil {
		return nil, fmt.Errorf("orderer %s: address must contain port: %w", conf.Address, err)
	}

	creds := insecure.NewCredentials()
	if conf.TLS.Mode != "" && conf.TLS.Mode != network.TLSModeNone {
		tlsCfg, err := conf.TLS.LoadClientTLSConfig(host)
		if err != nil {
			return nil, fmt.Errorf("orderer %s: failed to load TLS config: %w", conf.Address, err)
		}
		creds = credentials.NewTLS(tlsCfg)
	}

	conn, err := grpc.NewClient(conf.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial orderer %s: %w", conf.Address, err)
	}

	return &armaOrderer{
		conn:   conn,
		client: orderer.NewAtomicBroadcastClient(conn),
		addr:   conf.Address,
	}, nil
}

// broadcast sends env and waits for a response using the ArMA-compatible sequence:
// Send → Recv → CloseSend. The router only sends the response while the stream is open.
func (o *armaOrderer) broadcast(ctx context.Context, env *common.Envelope) error {
	stream, err := o.client.Broadcast(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	if err := stream.Send(env); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	// Recv BEFORE CloseSend: the ArMA router only sends the response while the
	// stream is open. Calling CloseSend first causes the router to close the
	// stream without sending a response (io.EOF).
	resp, err := stream.Recv()
	if err != nil {
		return err
	}
	_ = stream.CloseSend() // optional cleanup; ignore error
	if resp.Status != common.Status_SUCCESS {
		return fmt.Errorf("orderer rejected: %s", resp.Status.String())
	}
	return nil
}

func (o *armaOrderer) close() error {
	return o.conn.Close()
}

// ArMASubmitter implements core.Submitter for the ArMA orderer-router.
//
// ArMA uses a fixed batcher leader (leaderrotation: false in the staging config).
// Submitting to all orderers in parallel ensures the leader batcher always receives
// the tx directly, while non-leader batchers quickly detect it as a duplicate once
// the leader has processed it. This gives better latency than round-robin, where
// hitting a non-leader causes a slow forward attempt (requestforwardtimeout: 10s).
type ArMASubmitter struct {
	orderers []*armaOrderer
	packager nfabx.TxPackager
	logger   sdk.Logger
}

// NewArMASubmitter creates a new ArMASubmitter for the given orderers and signer.
// It is a drop-in replacement for nfabx.NewSubmitter when the backend is an ArMA orderer-router.
func NewArMASubmitter(config []network.OrdererConf, s sdk.Signer, logger sdk.Logger) (*ArMASubmitter, error) {
	if len(config) == 0 {
		return nil, errors.New("no orderers configured")
	}
	orderers := make([]*armaOrderer, len(config))
	for i, c := range config {
		o, err := newArmaOrderer(c)
		if err != nil {
			return nil, err
		}
		orderers[i] = o
	}
	return &ArMASubmitter{
		orderers: orderers,
		packager: nfabx.NewTxPackager(s),
		logger:   logger,
	}, nil
}

// Submit broadcasts the endorsement to all orderers in parallel and returns an error
// only if more than half fail. Broadcasting to all ensures the batcher leader always
// receives the tx directly regardless of which orderer is currently leading.
func (s *ArMASubmitter) Submit(ctx context.Context, end sdk.Endorsement) error {
	env, err := s.packager.PackageTx(end)
	if err != nil {
		return fmt.Errorf("package proposal: %w", err)
	}

	var wg sync.WaitGroup
	var errs int32
	for i, o := range s.orderers {
		wg.Go(func() {
			if err := o.broadcast(ctx, env); err != nil {
				s.logger.Warnf("orderer %d (%s): broadcast failed: %v", i, o.addr, err)
				atomic.AddInt32(&errs, 1)
			}
		})
	}
	wg.Wait()

	if int(errs)*2 > len(s.orderers) {
		return errors.New("error broadcasting")
	}
	return nil
}

// Close closes all orderer connections.
func (s *ArMASubmitter) Close() error {
	var errs []error
	for _, o := range s.orderers {
		if err := o.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
