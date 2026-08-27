/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-x-evm/common"
)

// Defaults for gateway-supplied EVM block.timestamp validation.
const (
	// DefaultTimestampFutureSkew is the max allowed request time ahead of the
	// endorser's clock (tight: limits early unlock / SWC-116 exposure).
	DefaultTimestampFutureSkew = 10 * time.Second
	// DefaultTimestampPastSkew is the max allowed request time behind the
	// endorser's clock (looser: hygiene only; replay is covered by nonce/TxID).
	DefaultTimestampPastSkew = 60 * time.Second
)

// Endorser contains configuration for the embedded endorser peer.
type Endorser struct {
	// Name is purely used for logging.
	Name string `mapstructure:"name"      yaml:"name"`
	// Identity is the signing identity of the endorser.
	Identity common.IdentityConfig `mapstructure:"identity"  yaml:"identity"`
	// Database stores the world state.
	Database DB `mapstructure:"database"  yaml:"database"`
	// DebugLogs enables per-tx StateDB DEBUG logging via StateDBLogger.
	DebugLogs bool `mapstructure:"debug-logs" yaml:"debug-logs"`
	// MaxTimestampFuture is how far ahead of local time a request timestamp may
	// be. Zero means DefaultTimestampFutureSkew.
	MaxTimestampFuture time.Duration `mapstructure:"max-timestamp-future" yaml:"max-timestamp-future"`
	// MaxTimestampPast is how far behind local time a request timestamp may be.
	// Zero means DefaultTimestampPastSkew.
	MaxTimestampPast time.Duration `mapstructure:"max-timestamp-past" yaml:"max-timestamp-past"`
}

// TimestampFutureSkew returns the configured future skew, or the default.
func (cfg Endorser) TimestampFutureSkew() time.Duration {
	if cfg.MaxTimestampFuture <= 0 {
		return DefaultTimestampFutureSkew
	}
	return cfg.MaxTimestampFuture
}

// TimestampPastSkew returns the configured past skew, or the default.
func (cfg Endorser) TimestampPastSkew() time.Duration {
	if cfg.MaxTimestampPast <= 0 {
		return DefaultTimestampPastSkew
	}
	return cfg.MaxTimestampPast
}

// Supported values for DB.Database.
const (
	// DBSQLite is the sqlite-backed VersionedDB. Persistent; supports both protocols.
	DBSQLite = "sqlite"
	// DBMemory is the in-memory LightKVS. Lost on restart; supports both protocols.
	DBMemory = "memory"
	// DBPebble is the pebble-backed PebbleKVS. Persistent; supports both protocols.
	DBPebble = "pebble"
)

// DB holds the database path for an endorser.
type DB struct {
	Database    string `mapstructure:"database" yaml:"database"`
	ConnString  string `mapstructure:"connection-string" yaml:"connection-string"`
	HistorySize int    `mapstructure:"history_size" yaml:"history_size"` // number of historical snapshots to keep (default: 2; test RPC uses a large value)
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
	if cfg.Database.Database == "" {
		errs = append(errs, errors.New("database.database is required"))
	}
	if cfg.Database.Database == DBSQLite && cfg.Database.ConnString == "" {
		errs = append(errs, errors.New("database.connection-string is required for sqlite"))
	}
	if cfg.Database.Database == DBPebble && cfg.Database.ConnString == "" {
		errs = append(errs, errors.New("database.connection-string (data directory) is required for pebble"))
	}

	return errors.Join(errs...)
}
