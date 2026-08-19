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

// Endorser contains configuration for a single embedded endorser peer.
type Endorser struct {
	Name      string                `mapstructure:"name"      yaml:"name"`
	Identity  common.IdentityConfig `mapstructure:"identity"  yaml:"identity"`
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`
	Database  DB                    `mapstructure:"database"  yaml:"database"`
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
	// DBPebble is the pebble-backed PebbleKVS. Persistent; fabric-x only.
	DBPebble = "pebble"
)

// DB holds the database path for an endorser.
type DB struct {
	Database    string `mapstructure:"database" yaml:"database"`
	ConnString  string `mapstructure:"connection-string" yaml:"connection-string"`
	HistorySize int    `mapstructure:"history_size" yaml:"history_size"` // number of historical snapshots to keep (default: 2; test RPC uses a large value)
}

// ValidateDatabaseProtocol rejects endorser backend/protocol combinations that
// cannot work. The protocol may be empty, meaning the default (fabric-x).
//
// PebbleKVS assigns versions as MAX(version)+1 per key and keeps deletes as
// tombstones, mirroring the fabric-x committer's own worldstate so that the
// versions it puts in the MVCC read-set validate against it. Classic Fabric
// expects the LightKVS scheme instead (one version shared across a block's
// writes to a key, reset after a delete), so pairing pebble with fabric would
// not fail loudly — it would produce read-sets the committer rejects, or worse,
// silently accepts against the wrong version. Fail at config time instead.
func ValidateDatabaseProtocol(database, protocol string) error {
	resolved, err := common.NormalizeProtocol(protocol)
	if err != nil {
		return err
	}
	if database == DBPebble && resolved == common.ProtocolFabric {
		return fmt.Errorf(
			"database.database %q is only supported with network.protocol %q, not %q "+
				"(its MVCC version scheme matches the fabric-x committer; use %q or %q for classic Fabric)",
			DBPebble, common.ProtocolFabricX, common.ProtocolFabric, DBSQLite, DBMemory)
	}
	return nil
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
	if cfg.Database.Database == DBSQLite && cfg.Database.ConnString == "" {
		errs = append(errs, errors.New("database.connection-string is required for sqlite"))
	}
	if cfg.Database.Database == DBPebble && cfg.Database.ConnString == "" {
		errs = append(errs, errors.New("database.connection-string (data directory) is required for pebble"))
	}

	return errors.Join(errs...)
}
