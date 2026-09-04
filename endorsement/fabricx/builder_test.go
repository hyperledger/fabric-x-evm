/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package fabricx_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	evmfabx "github.com/hyperledger/fabric-x-evm/endorsement/fabricx"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	"google.golang.org/protobuf/proto"
)

// fixedSigner signs deterministically, so two builders can only differ in what
// they serialize.
type fixedSigner struct{}

func (fixedSigner) Sign(msg []byte) ([]byte, error) { return append([]byte("sig:"), msg...), nil }
func (fixedSigner) Serialize() ([]byte, error)      { return []byte("identity"), nil }

func version(block uint64) *blocks.Version {
	return &blocks.Version{BlockNum: block}
}

func invocation() endorsement.Invocation {
	return endorsement.Invocation{
		TxID: "d34db33f",
		Args: [][]byte{{0xfb}, []byte("tx")},
		CCID: &peer.ChaincodeID{Name: "evm", Version: "1.0"},
	}
}

// The port must be byte-for-byte what the SDK builder produced, or a network
// running both would fail the packager's comparison.
func TestEndorse_MatchesSDKBuilder(t *testing.T) {
	tests := []struct {
		name string
		res  endorsement.ExecutionResult
	}{
		{
			name: "empty execution",
			res:  endorsement.Success(blocks.ReadWriteSet{}, nil, nil),
		},
		{
			name: "blind writes only",
			res: endorsement.Success(blocks.ReadWriteSet{
				Writes: []blocks.KVWrite{{Key: "b", Value: []byte("2")}, {Key: "a", Value: []byte("1")}},
			}, nil, nil),
		},
		{
			name: "reads, read-writes and blind writes",
			res: endorsement.Success(blocks.ReadWriteSet{
				Reads: []blocks.KVRead{
					{Key: "read-only", Version: version(7)},
					{Key: "read-write", Version: version(9)},
					{Key: "another-read-write", Version: version(4)},
					{Key: "unversioned"},
				},
				// Written out of key order, and more than one of each kind, so
				// every sort has something to do.
				Writes: []blocks.KVWrite{
					{Key: "read-write", Value: []byte("new")},
					{Key: "zeroth-blind", Value: []byte("last")},
					{Key: "another-read-write", Value: []byte("also new")},
					{Key: "blind", Value: []byte("fresh")},
				},
			}, nil, nil),
		},
		{
			name: "delete drops the value",
			res: endorsement.Success(blocks.ReadWriteSet{
				Reads:  []blocks.KVRead{{Key: "gone", Version: version(3)}},
				Writes: []blocks.KVWrite{{Key: "gone", Value: []byte("ignored"), IsDelete: true}},
			}, nil, nil),
		},
		{
			name: "event is signed over",
			res: endorsement.Success(blocks.ReadWriteSet{
				Writes: []blocks.KVWrite{{Key: "k", Value: []byte("v")}},
			}, []byte("log-payload"), []byte("return-data")),
		},
		{
			name: "rejected transaction",
			res:  endorsement.BadRequest("nonce too low"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := invocation()

			want, err := efabx.NewEndorsementBuilder(fixedSigner{}).Endorse(inv, tt.res)
			if err != nil {
				t.Fatalf("sdk builder: %v", err)
			}
			got, err := evmfabx.NewEndorsementBuilder(fixedSigner{}).Endorse(inv, tt.res)
			if err != nil {
				t.Fatalf("evm builder: %v", err)
			}

			if !proto.Equal(got, want) {
				t.Errorf("response differs from the SDK builder\n got: %v\nwant: %v", got, want)
			}
			// proto.Equal accepts different encodings of the same message; the
			// packager compares raw bytes.
			if !bytes.Equal(got.Payload, want.Payload) {
				t.Errorf("payload bytes differ\n got: %x\nwant: %x", got.Payload, want.Payload)
			}
			if !bytes.Equal(got.Endorsement.Signature, want.Endorsement.Signature) {
				t.Errorf("signed digest differs\n got: %x\nwant: %x", got.Endorsement.Signature, want.Endorsement.Signature)
			}
		})
	}
}

// Endorsers reach the same execution in their own iteration order, so the
// payload must be sorted rather than insertion-ordered.
func TestEndorse_PayloadIsOrderIndependent(t *testing.T) {
	forward := endorsement.Success(blocks.ReadWriteSet{
		Reads: []blocks.KVRead{{Key: "a", Version: version(1)}, {Key: "b", Version: version(2)}},
		Writes: []blocks.KVWrite{
			{Key: "b", Value: []byte("2")},
			{Key: "z", Value: []byte("26")},
			{Key: "a", Value: []byte("1")},
			{Key: "c", Value: []byte("3")},
		},
	}, nil, nil)
	reversed := endorsement.Success(blocks.ReadWriteSet{
		Reads: []blocks.KVRead{{Key: "b", Version: version(2)}, {Key: "a", Version: version(1)}},
		Writes: []blocks.KVWrite{
			{Key: "c", Value: []byte("3")},
			{Key: "a", Value: []byte("1")},
			{Key: "z", Value: []byte("26")},
			{Key: "b", Value: []byte("2")},
		},
	}, nil, nil)

	builder := evmfabx.NewEndorsementBuilder(fixedSigner{})
	first, err := builder.Endorse(invocation(), forward)
	if err != nil {
		t.Fatal(err)
	}
	second, err := builder.Endorse(invocation(), reversed)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Payload, second.Payload) {
		t.Errorf("payload depends on input order\n got: %x\nwant: %x", second.Payload, first.Payload)
	}
}

// A signer can fail in production, and must not yield an unsigned endorsement.
func TestEndorse_SignerFailurePropagates(t *testing.T) {
	tests := map[string]sdk.Signer{
		"serialize fails": failingSigner{serialize: errors.New("no identity")},
		"sign fails":      failingSigner{sign: errors.New("hsm unavailable")},
	}

	for name, signer := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := evmfabx.NewEndorsementBuilder(signer).Endorse(invocation(), endorsement.Success(
				blocks.ReadWriteSet{Writes: []blocks.KVWrite{{Key: "k", Value: []byte("v")}}}, nil, nil,
			))
			if err == nil {
				t.Fatal("expected an error")
			}
			if resp != nil {
				t.Errorf("response = %v, want nil", resp)
			}
		})
	}
}

// failingSigner fails whichever operation it is given an error for.
type failingSigner struct {
	sign      error
	serialize error
}

func (f failingSigner) Sign([]byte) ([]byte, error) { return nil, f.sign }
func (f failingSigner) Serialize() ([]byte, error)  { return []byte("identity"), f.serialize }

// The in-process harness hands one result to every endorser, so the builder
// must not write through to it.
func TestEndorse_DoesNotMutateResult(t *testing.T) {
	res := endorsement.Success(blocks.ReadWriteSet{
		Writes: []blocks.KVWrite{{Key: "gone", Value: []byte("still here"), IsDelete: true}},
	}, nil, nil)

	if _, err := evmfabx.NewEndorsementBuilder(fixedSigner{}).Endorse(invocation(), res); err != nil {
		t.Fatal(err)
	}

	if got := string(res.RWS.Writes[0].Value); got != "still here" {
		t.Errorf("caller's write was modified: value = %q", got)
	}
}
