/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"github.com/hyperledger/fabric-x-evm/common"
)

// Endorser contains configuration for a single endorser peer.
type Endorser struct {
	Name      string
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`
	Identity  common.IdentityConfig `mapstructure:"identity" yaml:"identity"`
	DbConnStr string                // path to the sqlite database for blocks and transactions
}
