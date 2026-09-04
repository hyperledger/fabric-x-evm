/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package fabricx builds Fabric-X endorsements for the EVM namespace: this
// repository's own version of the SDK's generic builder.
package fabricx

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"google.golang.org/protobuf/proto"
)

// NewEndorsementBuilder returns a builder that signs Fabric-X format responses.
func NewEndorsementBuilder(signer sdk.Signer) EndorsementBuilder {
	return EndorsementBuilder{signer: signer}
}

// EndorsementBuilder creates signed Fabric-X ProposalResponses.
type EndorsementBuilder struct {
	signer sdk.Signer
}

// Endorse signs the execution result, never the proposal. Every endorser of a
// transaction must produce the same bytes, so keep this deterministic.
func (e EndorsementBuilder) Endorse(inv endorsement.Invocation, res endorsement.ExecutionResult) (*peer.ProposalResponse, error) {
	metadata, err := marshalMetadata(inv, res)
	if err != nil {
		return nil, err
	}

	tx := buildTx(res.RWS, inv.CCID.Name, metadata)
	prpBytes, err := proto.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("marshal read/write set: %w", err)
	}

	digest, err := tx.Namespaces[0].ASN1Marshal(inv.TxID, metadata)
	if err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	identityBytes, err := e.signer.Serialize()
	if err != nil {
		return nil, err
	}
	sig, err := e.signer.Sign(digest)
	if err != nil {
		return nil, fmt.Errorf("sign proposal response payload: %w", err)
	}

	return &peer.ProposalResponse{
		Version: 1,
		Endorsement: &peer.Endorsement{
			Signature: sig,
			Endorser:  identityBytes,
		},
		Payload: prpBytes,
		Response: &peer.Response{
			Status:  res.Status,
			Message: res.Message,
			Payload: res.Payload,
		},
	}, nil
}

// marshalMetadata returns the signed-over metadata: input args, then the event.
// Each is left out when empty.
func marshalMetadata(inv endorsement.Invocation, res endorsement.ExecutionResult) ([][]byte, error) {
	var metadata [][]byte

	if len(inv.Args) > 0 {
		inputBytes, err := proto.Marshal(&peer.ChaincodeInput{Args: inv.Args})
		if err != nil {
			return nil, fmt.Errorf("marshal input: %w", err)
		}
		metadata = append(metadata, inputBytes)
	}

	if len(res.Event) > 0 {
		eventBytes, err := proto.Marshal(&peer.ChaincodeEvent{
			Payload:     res.Event,
			ChaincodeId: inv.CCID.Name,
			TxId:        inv.TxID,
			EventName:   "log",
		})
		if err != nil {
			return nil, fmt.Errorf("marshal events: %w", err)
		}
		metadata = append(metadata, eventBytes)
	}

	return metadata, nil
}

// buildTx splits the read-write set into the three Fabric-X access kinds,
// sorted by key so the result does not depend on the caller's ordering.
func buildTx(rws blocks.ReadWriteSet, namespace string, metadata [][]byte) *applicationpb.Tx {
	readByKey := make(map[string]blocks.KVRead, len(rws.Reads))
	for _, r := range rws.Reads {
		readByKey[r.Key] = r
	}

	readsOnly := make([]*applicationpb.Read, 0)
	readWrites := make([]*applicationpb.ReadWrite, 0)
	blindWrites := make([]*applicationpb.Write, 0)

	for _, w := range rws.Writes {
		if w.IsDelete {
			w.Value = nil
		}
		r, isRead := readByKey[w.Key]
		if !isRead {
			blindWrites = append(blindWrites, &applicationpb.Write{
				Key:   []byte(w.Key),
				Value: w.Value,
			})
			continue
		}

		delete(readByKey, w.Key)
		rw := &applicationpb.ReadWrite{
			Key:   []byte(w.Key),
			Value: w.Value,
		}
		if r.Version != nil {
			rw.Version = &r.Version.BlockNum
		}
		readWrites = append(readWrites, rw)
	}

	// Whatever is left was read but not written.
	for _, r := range readByKey {
		read := &applicationpb.Read{Key: []byte(r.Key)}
		if r.Version != nil {
			read.Version = &r.Version.BlockNum
		}
		readsOnly = append(readsOnly, read)
	}

	slices.SortFunc(readsOnly, func(a, b *applicationpb.Read) int {
		return bytes.Compare(a.Key, b.Key)
	})
	slices.SortFunc(readWrites, func(a, b *applicationpb.ReadWrite) int {
		return bytes.Compare(a.Key, b.Key)
	})
	slices.SortFunc(blindWrites, func(a, b *applicationpb.Write) int {
		return bytes.Compare(a.Key, b.Key)
	})

	return &applicationpb.Tx{
		Metadata: metadata,
		Namespaces: []*applicationpb.TxNamespace{{
			NsId:        namespace,
			NsVersion:   0,
			ReadsOnly:   readsOnly,
			ReadWrites:  readWrites,
			BlindWrites: blindWrites,
		}},
	}
}
