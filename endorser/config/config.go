/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"errors"
	"fmt"

	"github.com/hyperledger/fabric-x-evm/common"
)

// Endorser contains configuration for a single embedded endorser peer.
type Endorser struct {
	Name      string                `mapstructure:"name"      yaml:"name"`
	Identity  common.IdentityConfig `mapstructure:"identity"  yaml:"identity"`
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`
	Database  DB                    `mapstructure:"database"  yaml:"database"`
	// DebugLogs enables per-tx StateDB DEBUG logging via StateDBLogger.
	DebugLogs bool `mapstructure:"debug-logs" yaml:"debug-logs"`
}

// DB holds the database path for an endorser.
type DB struct {
	Database    string `mapstructure:"database" yaml:"database"`
	ConnString  string `mapstructure:"connection-string" yaml:"connection-string"`
	HistorySize int    `mapstructure:"history_size" yaml:"history_size"` // number of historical snapshots to keep (default: 2, use 128 for test RPC)
}

// Validate checks that required fields are set and values are within acceptable ranges.
func (cfg Endorser) Validate() error {
	var errs []error

	if cfg.Name == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if err := cfg.Identity.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("identity: %w", err))
	}
	if err := cfg.Committer.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("committer: %w", err))
	}
	if cfg.Database.Database == "" {
		errs = append(errs, errors.New("database.database is required"))
	}
	if cfg.Database.Database == "sqlite" && cfg.Database.ConnString == "" {
		errs = append(errs, errors.New("database.connection-string is required for sqlite"))
	}

	return errors.Join(errs...)
}
