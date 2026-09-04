/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package fabricx

import (
	"crypto/rand"
	"fmt"

	commonpb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nonceSize matches the SDK's, so transaction ids cover the same input width.
const nonceSize = 24

// NewInvocation builds a Fabric-X invocation, without the proposal payload,
// creator, hash or chaincode header extension that a Fabric-X envelope never reads.
func NewInvocation(signer sdk.Signer, channel, namespace, nsVersion string, args [][]byte) (endorsement.Invocation, error) {
	creator, err := signer.Serialize()
	if err != nil {
		return endorsement.Invocation{}, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return endorsement.Invocation{}, fmt.Errorf("read nonce: %w", err)
	}

	txID := protoutil.ComputeTxID(nonce, creator)
	ccid := &peer.ChaincodeID{Name: namespace, Version: nsVersion}

	chdr, err := protoutil.Marshal(&commonpb.ChannelHeader{
		Type:      int32(commonpb.HeaderType_ENDORSER_TRANSACTION),
		TxId:      txID,
		Timestamp: timestamppb.Now(),
		ChannelId: channel,
	})
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("marshal channel header: %w", err)
	}
	shdr, err := protoutil.Marshal(&commonpb.SignatureHeader{Nonce: nonce})
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("marshal signature header: %w", err)
	}
	hdr, err := protoutil.Marshal(&commonpb.Header{ChannelHeader: chdr, SignatureHeader: shdr})
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("marshal header: %w", err)
	}

	return endorsement.Invocation{
		TxID:     txID,
		Nonce:    nonce,
		Creator:  creator,
		Args:     args,
		CCID:     ccid,
		Channel:  channel,
		Proposal: &peer.Proposal{Header: hdr},
	}, nil
}
