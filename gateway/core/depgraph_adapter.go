/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"errors"
	"fmt"

	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"google.golang.org/protobuf/proto"
)

// errNoReadWriteSet means the endorsement carried nothing to schedule on.
var errNoReadWriteSet = errors.New("endorsement carries no read-write set")

// txContent extracts the applicationpb.Tx the dependency manager schedules on.
//
// On the fabric-x path a ProposalResponse payload already is one, so this
// unmarshals rather than converting: the keys the manager sorts by are the same
// bytes the packager submits. A classic Fabric payload is a different message
// and falls out as an empty namespace list.
func txContent(end sdk.Endorsement) (*applicationpb.Tx, error) {
	if len(end.Responses) == 0 {
		return nil, errNoReadWriteSet
	}
	payload := end.Responses[0].GetPayload()
	if len(payload) == 0 {
		return nil, errNoReadWriteSet
	}

	tx := &applicationpb.Tx{}
	if err := proto.Unmarshal(payload, tx); err != nil {
		return nil, fmt.Errorf("unmarshal read-write set: %w", err)
	}
	if len(tx.Namespaces) == 0 {
		return nil, errNoReadWriteSet
	}
	return tx, nil
}
