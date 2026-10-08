/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"errors"
	"fmt"

	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset/kvrwset"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"google.golang.org/protobuf/proto"
)

// errNoReadWriteSet means the endorsement carried nothing to schedule on.
var errNoReadWriteSet = errors.New("endorsement carries no read-write set")

// txContent extracts the applicationpb.Tx the dependency manager schedules on.
//
// On the fabric-x path a ProposalResponse payload already is one, so this
// unmarshals rather than converting: the keys the manager sorts by are the same
// bytes the packager submits. On the classic Fabric path, it extracts the KVRWSet
// from the ProposalResponsePayload extension and converts it to applicationpb.Tx.
func txContent(end sdk.Endorsement) (*applicationpb.Tx, error) {
	if len(end.Responses) == 0 {
		return nil, errNoReadWriteSet
	}
	payload := end.Responses[0].GetPayload()
	if len(payload) == 0 {
		return nil, errNoReadWriteSet
	}

	tx := &applicationpb.Tx{}
	if err := proto.Unmarshal(payload, tx); err == nil && len(tx.Namespaces) > 0 {
		return tx, nil
	}

	// Try unmarshalling as classic Fabric ProposalResponsePayload
	prp, err := protoutil.UnmarshalProposalResponsePayload(payload)
	if err != nil {
		return nil, fmt.Errorf("unmarshal read-write set: %w", err)
	}

	ca, err := protoutil.UnmarshalChaincodeAction(prp.Extension)
	if err != nil {
		return nil, fmt.Errorf("unmarshal chaincode action: %w", err)
	}

	txrws := &rwset.TxReadWriteSet{}
	if err := proto.Unmarshal(ca.Results, txrws); err != nil {
		return nil, fmt.Errorf("unmarshal tx rws: %w", err)
	}

	var namespaces []*applicationpb.TxNamespace
	for _, ns := range txrws.NsRwset {
		kvRws := &kvrwset.KVRWSet{}
		if err := proto.Unmarshal(ns.Rwset, kvRws); err != nil {
			return nil, fmt.Errorf("unmarshal kv rws for ns %s: %w", ns.Namespace, err)
		}
		var rws []*applicationpb.ReadWrite
		for _, r := range kvRws.Reads {
			rws = append(rws, &applicationpb.ReadWrite{
				Key: []byte(r.Key),
			})
		}
		for _, w := range kvRws.Writes {
			rws = append(rws, &applicationpb.ReadWrite{
				Key:   []byte(w.Key),
				Value: w.Value,
			})
		}
		if len(rws) > 0 {
			namespaces = append(namespaces, &applicationpb.TxNamespace{
				NsId:       ns.Namespace,
				ReadWrites: rws,
			})
		}
	}

	if len(namespaces) == 0 {
		return nil, errNoReadWriteSet
	}
	return &applicationpb.Tx{Namespaces: namespaces}, nil
}

// readWriteKeys lists the state keys tx reads and the ones it writes. A
// read-write counts as both: its read carries the version the committer checks.
func readWriteKeys(tx *applicationpb.Tx) (reads, writes []string) {
	for _, ns := range tx.GetNamespaces() {
		for _, r := range ns.GetReadsOnly() {
			reads = append(reads, stateKey(ns.GetNsId(), r.GetKey()))
		}
		for _, rw := range ns.GetReadWrites() {
			k := stateKey(ns.GetNsId(), rw.GetKey())
			reads = append(reads, k)
			writes = append(writes, k)
		}
		for _, w := range ns.GetBlindWrites() {
			writes = append(writes, stateKey(ns.GetNsId(), w.GetKey()))
		}
	}
	return reads, writes
}

// stateKey joins a namespace and a key. Namespace ids cannot contain NUL, so
// the separator keeps "a"+"bc" and "ab"+"c" apart.
func stateKey(ns string, key []byte) string { return ns + "\x00" + string(key) }
