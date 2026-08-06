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
	endorser "github.com/hyperledger/fabric-x-evm/endorser/config"
)

// Config is the top-level configuration for the combined (embedded-endorsers) deployment.
type Config struct {
	Logging   Logging             `mapstructure:"logging"   yaml:"logging"`
	Network   common.Network      `mapstructure:"network"   yaml:"network"`
	Gateway   Gateway             `mapstructure:"gateway"   yaml:"gateway"`
	Endorsers []endorser.Endorser `mapstructure:"endorsers" yaml:"endorsers"`
}

// Logging is the config for the Fabric Logger
type Logging struct {
	// Format is the log record format specifier for the Logging instance. If the
	// spec is the string "json", log records will be formatted as JSON. Any
	// other string will be provided to the FormatEncoder. Please see
	// fabenc.ParseFormat for details on the supported verbs.
	//
	// If Format is not provided, a default format that provides basic information will
	// be used.
	Format string `mapstructure:"format" yaml:"format"`

	// Spec determines the log levels that are enabled for the logging system. The
	// spec must be in a format that can be processed by ActivateSpec.
	//
	// If Spec is not provided, loggers will be enabled at the INFO level.
	Spec string `mapstructure:"spec" yaml:"spec"`
}

// DB holds the database paths for the gateway.
type DB struct {
	ConnString string `mapstructure:"connection-string" yaml:"connection-string"` // SQLite connection string for blocks, transactions, and logs
	TriePath   string `mapstructure:"trie-path"         yaml:"trie-path"`         // PebbleDB directory for state root trie; empty = in-memory
}

// Gateway contains configuration for the gateway component.
type Gateway struct {
	Listen   string                `mapstructure:"listen" yaml:"listen"`
	Identity common.IdentityConfig `mapstructure:"identity" yaml:"identity"`
	Database DB                    `mapstructure:"database" yaml:"database"`

	Orderers  []common.ClientConfig `mapstructure:"orderers" yaml:"orderers"`
	Committer common.ClientConfig   `mapstructure:"committer" yaml:"committer"`

	// Endorsers, when set, switches to split deployment: the gateway dials each
	// entry over gRPC (co-located endorsers included, addressed on localhost)
	// instead of building embedded endorsers from the top-level Endorsers list.
	Endorsers   []common.ClientConfig `mapstructure:"endorsers" yaml:"endorsers"`
	SyncTimeout time.Duration         `mapstructure:"sync-timeout" yaml:"sync-timeout"`

	WorkerCount         int `mapstructure:"worker-count"  yaml:"worker-count"`
	SubmitterCount      int `mapstructure:"submitter-count" yaml:"submitter-count"`
	EndorsementChanSize int `mapstructure:"endorsement-chan-size"  yaml:"endorsement-chan-size"`
}

// Validate checks that required fields are set and values are within acceptable ranges.
func (cfg Config) Validate() error {
	var errs []error

	if cfg.Network.Channel == "" {
		errs = append(errs, errors.New("network.channel is required"))
	}
	if cfg.Network.Namespace == "" {
		errs = append(errs, errors.New("network.namespace is required"))
	}
	_, protocolErr := common.NormalizeProtocol(cfg.Network.Protocol)
	if protocolErr != nil {
		errs = append(errs, protocolErr)
	}
	if cfg.Gateway.Listen == "" {
		errs = append(errs, errors.New("gateway.listen is required"))
	} else if err := common.ValidateListenAddress(cfg.Gateway.Listen); err != nil {
		errs = append(errs, err)
	}
	if err := cfg.Gateway.Identity.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("gateway.identity: %w", err))
	}
	if cfg.Gateway.Database.ConnString == "" {
		errs = append(errs, errors.New("gateway.database.connection-string is required"))
	}
	if err := cfg.Gateway.Committer.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("gateway.committer: %w", err))
	}
	if len(cfg.Gateway.Orderers) == 0 {
		errs = append(errs, errors.New("gateway.orderers must have at least one entry"))
	}
	for i, o := range cfg.Gateway.Orderers {
		if err := o.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("gateway.orderers[%d]: %w", i, err))
		}
	}

	if len(cfg.Gateway.Endorsers) == 0 {
		if len(cfg.Endorsers) == 0 {
			errs = append(errs, errors.New("endorsers must have at least one entry"))
		}
		for i := range cfg.Endorsers {
			errs = append(errs, cfg.Endorsers[i].Validate())
			// Checked here rather than in Endorser.Validate because the backend and
			// the protocol it has to agree with live at different config levels.
			// Skipped when the protocol itself is already invalid, so one bad
			// protocol value is not also reported once per endorser.
			if protocolErr == nil {
				if err := endorser.ValidateDatabaseProtocol(cfg.Endorsers[i].Database.Database, cfg.Network.Protocol); err != nil {
					errs = append(errs, fmt.Errorf("endorsers[%d]: %w", i, err))
				}
			}
		}
	} else {
		for i, e := range cfg.Gateway.Endorsers {
			if err := e.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("gateway.endorsers[%d]: %w", i, err))
			}
		}
	}

	return errors.Join(errs...)
}
