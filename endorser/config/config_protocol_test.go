/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config_test

import (
	"testing"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
)

func TestValidateDatabaseProtocol(t *testing.T) {
	tests := []struct {
		name     string
		database string
		protocol string
		wantErr  string
	}{
		{"pebble with fabric-x", config.DBPebble, common.ProtocolFabricX, ""},
		{"pebble with empty (defaults to fabric-x)", config.DBPebble, "", ""},
		{"pebble with fabric", config.DBPebble, common.ProtocolFabric, "only supported with"},
		{"sqlite with fabric", config.DBSQLite, common.ProtocolFabric, ""},
		{"memory with fabric", config.DBMemory, common.ProtocolFabric, ""},
		{"sqlite with empty", config.DBSQLite, "", ""},
		{"pebble with bogus", config.DBPebble, "bogus", "network.protocol must be"},
		{"sqlite with bogus", config.DBSQLite, "bogus", "network.protocol must be"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateDatabaseProtocol(tt.database, tt.protocol)
			checkErr(t, err, tt.wantErr)
		})
	}
}
