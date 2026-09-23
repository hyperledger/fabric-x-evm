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

// Config is the top-level configuration for a process: either a gateway (with
// its own embedded endorser, plus optionally dialing remote ones) or a
// standalone endorser.
//
// Gateway is optional — nil means an endorser-only process. Endorser is
// mandatory whenever Gateway is set: a gateway always embeds its own org's
// endorser, called directly in-process. Gateway.Endorsers, when present, are
// strictly additional remote endorsers (other orgs) dialed over gRPC — never
// a substitute for the always-present local one.
type Config struct {
	Logging Logging        `mapstructure:"logging"   yaml:"logging"`
	Network common.Network `mapstructure:"network"   yaml:"network"`

	// Committer is this process's connection to the committer it stays in
	// sync with. One synchronizer per process means one committer
	// connection per process, so it lives here rather than under gateway:
	// or endorser: even though today only the synchronizer consumes it.
	Committer common.ClientConfig `mapstructure:"committer" yaml:"committer"`
	// Synchronizer configures this process's single synchronizer, which
	// delivers committed blocks to the chain, the embedded endorser's KVS,
	// and the gateway.
	Synchronizer Synchronizer `mapstructure:"synchronizer" yaml:"synchronizer"`

	// Gateway configures the gateway component. Nil means this process runs
	// only a standalone endorser.
	Gateway *Gateway `mapstructure:"gateway" yaml:"gateway"`

	// Endorser configures this process's own, embedded endorser. A real
	// deployment never embeds more than one.
	Endorser *endorser.Endorser `mapstructure:"endorser" yaml:"endorser"`
}

// Synchronizer configures the process-wide synchronizer.
type Synchronizer struct {
	// Timeout bounds how long Run waits for the initial sync to complete
	// before returning an error. Zero means DefaultSyncTimeout.
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout"`
}

// DefaultSyncTimeout is used when Synchronizer.Timeout is unset.
const DefaultSyncTimeout = 10 * time.Minute

// SyncTimeout returns the configured timeout, or DefaultSyncTimeout.
func (s Synchronizer) SyncTimeout() time.Duration {
	if s.Timeout <= 0 {
		return DefaultSyncTimeout
	}
	return s.Timeout
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

	Orderers []common.ClientConfig `mapstructure:"orderers" yaml:"orderers"`

	// Endorsers, when set, are additional remote endorsers (other orgs) the
	// gateway dials over gRPC — never a substitute for the always-present
	// local endorser configured at the top level. Endorsement is N-of-N, so
	// each entry here makes every transaction depend on that org's liveness.
	Endorsers []common.ClientConfig `mapstructure:"endorsers" yaml:"endorsers"`

	// Vhosts lists the Host header values the JSON-RPC HTTP server accepts
	// from non-IP clients, guarding against DNS-rebinding attacks (a hostile
	// web page pointing a domain at 127.0.0.1 to reach the RPC port from a
	// browser). Requests with an IP Host header (127.0.0.1, 10.0.0.5, ...)
	// are always allowed regardless of this list. Unset defaults to
	// ["localhost"]; "*" allows any host (only behind a reverse proxy or
	// network you already trust, e.g. a locked-down k8s namespace) — add the
	// specific hostname (ingress host, Service DNS name, ...) instead where
	// possible.
	Vhosts []string `mapstructure:"vhosts" yaml:"vhosts"`

	WorkerCount         int `mapstructure:"worker-count"  yaml:"worker-count"`
	SubmitterCount      int `mapstructure:"submitter-count" yaml:"submitter-count"`
	EndorsementChanSize int `mapstructure:"endorsement-chan-size"  yaml:"endorsement-chan-size"`

	// Filters caps eth_*Filter and eth_subscribe resource use. Nil means
	// package defaults. Pointer fields distinguish omitted (nil → default)
	// from an explicit 0 (allow none).
	Filters *Filters `mapstructure:"filters" yaml:"filters"`
}

// Filters configures server-side filter and subscription resource caps.
// Pointer fields: nil = omitted (use default), non-nil 0 = actually zero.
type Filters struct {
	// MaxFilters is the global concurrent cap on eth_newBlockFilter + eth_newFilter.
	MaxFilters *int `mapstructure:"max-filters" yaml:"max-filters"`
	// MaxSubscriptionsPerConnection caps eth_subscribe("newHeads") per WS connection.
	MaxSubscriptionsPerConnection *int `mapstructure:"max-subscriptions-per-connection" yaml:"max-subscriptions-per-connection"`
	// MaxSubscriptionsGlobal caps newHeads subscribers across all connections.
	MaxSubscriptionsGlobal *int `mapstructure:"max-subscriptions-global" yaml:"max-subscriptions-global"`
}

// ResolvedLimits merges this config with defaults. Nil Filters or nil fields
// keep the corresponding default; an explicit 0 is preserved.
func (f *Filters) ResolvedLimits(defMaxFilters, defPerConn, defGlobal int) (maxFilters, maxSubsPerConn, maxSubsGlobal int) {
	maxFilters, maxSubsPerConn, maxSubsGlobal = defMaxFilters, defPerConn, defGlobal
	if f == nil {
		return
	}
	if f.MaxFilters != nil {
		maxFilters = *f.MaxFilters
	}
	if f.MaxSubscriptionsPerConnection != nil {
		maxSubsPerConn = *f.MaxSubscriptionsPerConnection
	}
	if f.MaxSubscriptionsGlobal != nil {
		maxSubsGlobal = *f.MaxSubscriptionsGlobal
	}
	return
}

// DefaultVhosts is used when Gateway.Vhosts is unset.
var DefaultVhosts = []string{"localhost"}

// VHosts returns the configured RPC Host-header allowlist, or DefaultVhosts
// when unset.
func (g Gateway) VHosts() []string {
	if len(g.Vhosts) == 0 {
		return DefaultVhosts
	}
	return g.Vhosts
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
	if err := cfg.Committer.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("committer: %w", err))
	}

	if cfg.Gateway == nil && cfg.Endorser == nil {
		errs = append(errs, errors.New("one of gateway or endorser is required"))
	}

	if cfg.Gateway != nil {
		if cfg.Endorser == nil {
			errs = append(errs, errors.New("endorser is required when gateway is present"))
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
		if len(cfg.Gateway.Orderers) == 0 {
			errs = append(errs, errors.New("gateway.orderers must have at least one entry"))
		}
		for i, o := range cfg.Gateway.Orderers {
			if err := o.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("gateway.orderers[%d]: %w", i, err))
			}
		}
		for i, e := range cfg.Gateway.Endorsers {
			if err := e.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("gateway.endorsers[%d]: %w", i, err))
			}
		}
		if f := cfg.Gateway.Filters; f != nil {
			if f.MaxFilters != nil && *f.MaxFilters < 0 {
				errs = append(errs, errors.New("gateway.filters.max-filters must be >= 0"))
			}
			if f.MaxSubscriptionsPerConnection != nil && *f.MaxSubscriptionsPerConnection < 0 {
				errs = append(errs, errors.New("gateway.filters.max-subscriptions-per-connection must be >= 0"))
			}
			if f.MaxSubscriptionsGlobal != nil && *f.MaxSubscriptionsGlobal < 0 {
				errs = append(errs, errors.New("gateway.filters.max-subscriptions-global must be >= 0"))
			}
		}
	}

	if cfg.Endorser != nil {
		errs = append(errs, cfg.Endorser.Validate())
		// A standalone endorser (no gateway) is unreachable, and therefore
		// pointless, without a gRPC server. A gateway's own embedded endorser
		// has no such requirement — serving it is optional, config-gated on
		// whether another org needs to reach it.
		if cfg.Gateway == nil && cfg.Endorser.Server == nil {
			errs = append(errs, errors.New("endorser.server is required when running as a standalone endorser (no gateway configured)"))
		}
	}

	return errors.Join(errs...)
}
