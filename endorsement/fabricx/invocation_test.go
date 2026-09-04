/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package fabricx_test

import (
	"bytes"
	"errors"
	"testing"

	commonpb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/msppb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	evmfabx "github.com/hyperledger/fabric-x-evm/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"google.golang.org/protobuf/proto"
)

const (
	channel   = "mychannel"
	namespace = "evm"
	nsVersion = "1.0"
)

func newInvocation(t *testing.T, args [][]byte) endorsement.Invocation {
	t.Helper()
	inv, err := evmfabx.NewInvocation(fixedSigner{}, channel, namespace, nsVersion, args)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func headers(t *testing.T, prop *peer.Proposal) (*commonpb.ChannelHeader, *commonpb.SignatureHeader) {
	t.Helper()
	hdr, err := protoutil.UnmarshalHeader(prop.Header)
	if err != nil {
		t.Fatal(err)
	}
	chdr, err := protoutil.UnmarshalChannelHeader(hdr.ChannelHeader)
	if err != nil {
		t.Fatal(err)
	}
	shdr, err := protoutil.UnmarshalSignatureHeader(hdr.SignatureHeader)
	if err != nil {
		t.Fatal(err)
	}
	return chdr, shdr
}

// The transaction id is derived from the creator, so a signer that cannot
// serialize one has to fail rather than produce an unaddressable invocation.
func TestNewInvocation_SignerFailurePropagates(t *testing.T) {
	_, err := evmfabx.NewInvocation(failingSigner{serialize: errors.New("no identity")}, channel, namespace, nsVersion, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}

// The packager copies the header into the envelope with only the type changed,
// so it must still say everything the SDK's does apart from what it never reads.
func TestNewInvocation_ChannelHeaderMatchesSDK(t *testing.T) {
	args := [][]byte{{0xfb}, []byte("tx")}

	got := newInvocation(t, args)
	want, err := endorsement.NewInvocation(fixedSigner{}, channel, namespace, nsVersion, args)
	if err != nil {
		t.Fatal(err)
	}

	gotHdr, gotSig := headers(t, got.Proposal)
	wantHdr, _ := headers(t, want.Proposal)

	if gotHdr.Type != wantHdr.Type {
		t.Errorf("Type = %d, want %d", gotHdr.Type, wantHdr.Type)
	}
	if gotHdr.ChannelId != wantHdr.ChannelId {
		t.Errorf("ChannelId = %q, want %q", gotHdr.ChannelId, wantHdr.ChannelId)
	}
	if gotHdr.Epoch != wantHdr.Epoch {
		t.Errorf("Epoch = %d, want %d", gotHdr.Epoch, wantHdr.Epoch)
	}
	// Fabric-X packaging never reads the extension, so we leave it out.
	if len(gotHdr.Extension) != 0 {
		t.Errorf("Extension = %x, want it left out", gotHdr.Extension)
	}
	if gotHdr.TxId != got.TxID {
		t.Errorf("header TxId = %q, want the invocation's %q", gotHdr.TxId, got.TxID)
	}
	if gotHdr.Timestamp == nil {
		t.Error("Timestamp = nil, want the time the invocation was created")
	}
	if !bytes.Equal(gotSig.Nonce, got.Nonce) {
		t.Errorf("header Nonce = %x, want the invocation's %x", gotSig.Nonce, got.Nonce)
	}
	if len(gotSig.Creator) != 0 {
		t.Errorf("Creator = %x, want it left out", gotSig.Creator)
	}
	if len(got.Proposal.Payload) != 0 {
		t.Errorf("Payload = %x, want it left out", got.Proposal.Payload)
	}
}

// Restoring the creator and the payload must not change the envelope. This
// fails if the packager ever starts reading either.
func TestNewInvocation_EnvelopeSurvivesTheMissingProposal(t *testing.T) {
	args := [][]byte{{0xfb}, bytes.Repeat([]byte{0xab}, 4096)}
	inv := newInvocation(t, args)

	resps := []*peer.ProposalResponse{endorse(t, inv)}
	restored := restoreProposal(t, inv, args)
	if proto.Size(restored) <= proto.Size(inv.Proposal) {
		t.Fatal("the restored proposal is not bigger, so this proves nothing")
	}

	lean, err := nfabx.CreateTx(inv.Proposal, resps...)
	if err != nil {
		t.Fatalf("package from the lean proposal: %v", err)
	}
	full, err := nfabx.CreateTx(restored, resps...)
	if err != nil {
		t.Fatalf("package from the full proposal: %v", err)
	}

	if !proto.Equal(lean, full) {
		t.Error("envelope changed when the proposal payload and creator were restored")
	}
}

// The endorsement waits in the submit queue until its block commits, so what it
// carries must not scale with the transaction.
func TestNewInvocation_ProposalSizeIsIndependentOfArgs(t *testing.T) {
	small := newInvocation(t, [][]byte{{0xfb}, []byte("tx")})
	large := newInvocation(t, [][]byte{{0xfb}, bytes.Repeat([]byte{0xab}, 8192)})

	smallSize := proto.Size(small.Proposal)
	largeSize := proto.Size(large.Proposal)
	if smallSize != largeSize {
		t.Errorf("proposal grew with the transaction: %d bytes vs %d", largeSize, smallSize)
	}
}

// endorse signs an arbitrary result, enough for the packager to accept it.
func endorse(t *testing.T, inv endorsement.Invocation) *peer.ProposalResponse {
	t.Helper()
	resp, err := evmfabx.NewEndorsementBuilder(identitySigner{}).Endorse(inv, endorsement.Success(
		blocks.ReadWriteSet{Writes: []blocks.KVWrite{{Key: "k", Value: []byte("v")}}}, nil, nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// restoreProposal rebuilds what the SDK would have produced: the same header,
// plus the creator and the chaincode payload.
func restoreProposal(t *testing.T, inv endorsement.Invocation, args [][]byte) *peer.Proposal {
	t.Helper()

	chdr, shdr := headers(t, inv.Proposal)
	shdr.Creator = inv.Creator

	payload, err := protoutil.Marshal(&peer.ChaincodeProposalPayload{
		Input: protoutil.MarshalOrPanic(&peer.ChaincodeInvocationSpec{
			ChaincodeSpec: &peer.ChaincodeSpec{
				Type:        peer.ChaincodeSpec_CAR,
				ChaincodeId: inv.CCID,
				Input:       &peer.ChaincodeInput{Args: args},
			},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	return &peer.Proposal{
		Header: protoutil.MarshalOrPanic(&commonpb.Header{
			ChannelHeader:   protoutil.MarshalOrPanic(chdr),
			SignatureHeader: protoutil.MarshalOrPanic(shdr),
		}),
		Payload: payload,
	}
}

// identitySigner serializes a real msp identity, which the packager unmarshals.
type identitySigner struct{}

func (identitySigner) Sign([]byte) ([]byte, error) { return []byte("signature"), nil }
func (identitySigner) Serialize() ([]byte, error) {
	return protoutil.Marshal(&msppb.Identity{MspId: "Org1MSP"})
}
