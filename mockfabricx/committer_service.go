/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"context"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RegisterCommitterServices registers replay-minimal committer and peer services.
func RegisterCommitterServices(s grpc.ServiceRegistrar, ledger *Ledger) {
	svc := &committerService{ledger: ledger}
	committerpb.RegisterBlockQueryServiceServer(s, svc)
	committerpb.RegisterQueryServiceServer(s, svc)
	committerpb.RegisterNotifierServer(s, svc)
	peer.RegisterDeliverServer(s, svc)
}

type committerService struct {
	committerpb.UnimplementedBlockQueryServiceServer
	committerpb.UnimplementedQueryServiceServer
	committerpb.UnimplementedNotifierServer
	peer.UnimplementedDeliverServer

	ledger *Ledger
}

func (s *committerService) GetBlockchainInfo(context.Context, *emptypb.Empty) (*common.BlockchainInfo, error) {
	return &common.BlockchainInfo{Height: s.ledger.Height()}, nil
}

func (s *committerService) Deliver(stream peer.Deliver_DeliverServer) error {
	env, err := stream.Recv()
	if err != nil {
		return err
	}

	startBlockNumber := seekStartBlockNumber(env)
	existingBlocks, chForNewBlocks, cancel := s.ledger.SubscribeBlocksFrom(startBlockNumber)
	defer cancel()

	for _, block := range existingBlocks {
		if err := stream.Send(&peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: block}}); err != nil {
			return err
		}
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case block, ok := <-chForNewBlocks:
			if !ok {
				return nil
			}

			if block.GetHeader().GetNumber() < startBlockNumber {
				continue
			}

			if err := stream.Send(&peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: block}}); err != nil {
				return err
			}
		}
	}
}

func seekStartBlockNumber(env *common.Envelope) uint64 {
	payload := &common.Payload{}
	if proto.Unmarshal(env.GetPayload(), payload) != nil {
		return 1
	}

	seek := &orderer.SeekInfo{}
	if proto.Unmarshal(payload.GetData(), seek) != nil {
		return 1
	}

	specified := seek.GetStart().GetSpecified()
	if specified == nil || specified.Number == 0 {
		return 1
	}
	return specified.Number
}

func (s *committerService) StreamAllTransactions(req *committerpb.StreamAllRequest, stream committerpb.Notifier_StreamAllTransactionsServer) error {
	existingBatches, chForNewBatches, cancel := s.ledger.SubscribeEventBatches()
	defer cancel()

	for _, batch := range existingBatches {
		filtered := filterBatch(batch, req)
		if len(filtered.Events) == 0 {
			continue
		}
		if err := stream.Send(filtered); err != nil {
			return err
		}
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case batch, ok := <-chForNewBatches:
			if !ok {
				return nil
			}
			filtered := filterBatch(batch, req)
			if len(filtered.Events) == 0 {
				continue
			}
			if err := stream.Send(filtered); err != nil {
				return err
			}
		}
	}
}

func filterBatch(batch *committerpb.TxEventBatch, req *committerpb.StreamAllRequest) *committerpb.TxEventBatch {
	if req == nil {
		req = &committerpb.StreamAllRequest{}
	}
	out := &committerpb.TxEventBatch{BlockNumber: batch.GetBlockNumber()}
	for _, event := range batch.GetEvents() {
		if !matchesStatus(event.GetStatus(), req.GetFilterStatus()) {
			continue
		}
		namespaces := filterNamespaces(event.GetNamespaces(), req.GetFilterNamespaces())
		if len(req.GetFilterNamespaces()) > 0 && len(namespaces) == 0 {
			continue
		}
		copied := &committerpb.TxEvent{Status: event.GetStatus()}
		if event.GetRef() != nil {
			copied.Ref = proto.Clone(event.GetRef()).(*committerpb.TxRef)
		}
		if req.GetIncludeReadWriteSets() {
			copied.Namespaces = namespaces
		}
		if req.GetIncludeEndorsements() {
			copied.Endorsements = append([]*applicationpb.Endorsements(nil), event.GetEndorsements()...)
		}
		out.Events = append(out.Events, copied)
	}
	return out
}

func matchesStatus(status committerpb.Status, allowed []committerpb.Status) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == status {
			return true
		}
	}
	return false
}

func filterNamespaces(namespaces []*applicationpb.TxNamespace, allowed []string) []*applicationpb.TxNamespace {
	if len(allowed) == 0 {
		return append([]*applicationpb.TxNamespace(nil), namespaces...)
	}
	allow := map[string]any{}
	for _, nsID := range allowed {
		allow[nsID] = nil
	}
	out := []*applicationpb.TxNamespace{}
	for _, ns := range namespaces {
		if _, ok := allow[ns.GetNsId()]; ok {
			out = append(out, ns)
		}
	}
	return out
}
