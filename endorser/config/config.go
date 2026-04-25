/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"github.com/hyperledger/fabric-x-evm/common"
)

// Endorser contains configuration for a single embedded endorser peer.
type Endorser struct {
	Name      string                `mapstructure:"name"      yaml:"name"`
	Identity  common.IdentityConfig `mapstructure:"identity"  yaml:"identity"`
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`
	Database  DB                    `mapstructure:"database"  yaml:"database"`
}

// DB holds the database path for an endorser.
type DB struct {
	ConnString string `mapstructure:"connection-string" yaml:"connection-string"`
}
