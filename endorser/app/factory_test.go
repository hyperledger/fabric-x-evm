/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"strings"
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
)

func TestNewEndorserCore_QueryServiceRequiresFabricX(t *testing.T) {
	db := config.DB{
		Database:     config.DBQueryService,
		QueryService: &common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 7001}},
	}
	_, _, _, err := NewEndorserCore(db, "mychannel", "evm", common.ProtocolFabric, nil, execution.EVMConfig{}, false, config.Endorser{})
	if err == nil || !strings.Contains(err.Error(), "requires protocol") {
		t.Fatalf("err = %v, want a protocol error", err)
	}
}
