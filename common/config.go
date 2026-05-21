/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"

	"github.com/hyperledger/fabric-x-sdk/network"
)

// Network contains network details shared across components
// and network participants.
type Network struct {
	// Protocol selects the network protocol: "fabric" or "fabric-x". Defaults to "fabric-x".
	Protocol string `mapstructure:"protocol"`

	// Channel is the Fabric channel.
	Channel string `mapstructure:"channel" yaml:"channel"`

	// Namespace is the namespace for all token transactions.
	Namespace string `mapstructure:"namespace" yaml:"namespace"`

	// NsVersion is the version of the namespace, usually 1.0.
	NsVersion string `mapstructure:"ns-version" yaml:"ns-version"`

	// ChainID is the ethereum-style chain ID for this network.
	ChainID int64 `mapstructure:"chain-id" yaml:"chain-id"`
}

// IdentityConfig defines the component's MSP.
type IdentityConfig struct {
	// MspID indicates to which MSP this client belongs to.
	MspID  string `mapstructure:"msp-id" yaml:"msp-id"`
	MSPDir string `mapstructure:"msp-dir" yaml:"msp-dir"`
}

// Validate checks that MspID is set and MSPDir exists as a directory.
func (c IdentityConfig) Validate() error {
	var errs []error
	if c.MspID == "" {
		errs = append(errs, errors.New("msp-id is required"))
	}
	if c.MSPDir == "" {
		errs = append(errs, errors.New("msp-dir is required"))
	} else if info, err := os.Stat(c.MSPDir); err != nil {
		errs = append(errs, fmt.Errorf("msp-dir: %w", err))
	} else if !info.IsDir() {
		errs = append(errs, fmt.Errorf("msp-dir: %q is not a directory", c.MSPDir))
	}
	return errors.Join(errs...)
}

// ClientConfig contains a single endpoint, TLS config, and retry profile.
type ClientConfig struct {
	Endpoint *Endpoint `mapstructure:"endpoint"  yaml:"endpoint"`
	TLS      TLSConfig `mapstructure:"tls"       yaml:"tls"`
}

// Endpoint describes a remote endpoint.
type Endpoint struct {
	Host string `mapstructure:"host" json:"host,omitempty" yaml:"host,omitempty"`
	Port int    `mapstructure:"port" json:"port,omitempty" yaml:"port,omitempty"`
}

// Address returns a string representation of the endpoint's address.
func (e *Endpoint) Address() string {
	// JoinHostPort defaults to ipv6 for localhost,
	// which is not always wanted.
	if e.Host == "localhost" {
		return fmt.Sprintf("%s:%d", e.Host, e.Port)
	}
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// ToPeerConf converts a ClientConfig to the SDK's PeerConf.
func (c ClientConfig) ToPeerConf() network.PeerConf {
	return network.PeerConf{
		Address: c.Endpoint.Address(),
		TLS: network.TLSConfig{
			Mode:        c.TLS.Mode,
			CertPath:    c.TLS.CertPath,
			KeyPath:     c.TLS.KeyPath,
			CACertPaths: c.TLS.CACertPaths,
			ServerName:  c.TLS.ServerName,
		},
	}
}

// ToOrdererConf converts a ClientConfig to the SDK's OrdererConf.
func (c ClientConfig) ToOrdererConf() network.OrdererConf {
	return network.OrdererConf{
		Address: c.Endpoint.Address(),
		TLS: network.TLSConfig{
			Mode:        c.TLS.Mode,
			CertPath:    c.TLS.CertPath,
			KeyPath:     c.TLS.KeyPath,
			CACertPaths: c.TLS.CACertPaths,
			ServerName:  c.TLS.ServerName,
		},
	}
}

// TLSConfig holds the TLS options and certificate paths
// used for secure communication between servers and clients.
// Credentials are built based on the configuration mode.
// For example, If only server-side TLS is required, the certificate pool (certPool) is not built (for a server),
// since the relevant certificates paths are defined in the YAML according to the selected mode.
type TLSConfig struct {
	Mode string `mapstructure:"mode" yaml:"mode"`
	// CertPath is the path to the certificate file (public key).
	CertPath string `mapstructure:"cert-path" yaml:"cert-path"`
	// KeyPath is the path to the key file (private key).
	KeyPath     string   `mapstructure:"key-path"      yaml:"key-path"`
	CACertPaths []string `mapstructure:"ca-cert-paths" yaml:"ca-cert-paths"`
	// ServerName is the server name for TLS certificate validation (SNI).
	ServerName string `mapstructure:"server-name" yaml:"server-name"`
}

// Validate checks that each configured cert/key path exists on disk.
func (c TLSConfig) Validate() error {
	var errs []error
	checkFile := func(label, path string) {
		if path == "" {
			return
		}
		if _, err := os.Stat(path); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
		}
	}
	for _, path := range c.CACertPaths {
		checkFile("tls.ca-cert-paths", path)
	}
	checkFile("tls.cert-path", c.CertPath)
	checkFile("tls.key-path", c.KeyPath)
	return errors.Join(errs...)
}

// Validate checks that the endpoint is set and that TLS files exist.
func (c ClientConfig) Validate() error {
	var errs []error
	if c.Endpoint == nil {
		errs = append(errs, errors.New("endpoint is required"))
	}
	errs = append(errs, c.TLS.Validate())
	return errors.Join(errs...)
}

// ValidateListenAddress checks that addr is a valid host:port listen address.
func ValidateListenAddress(addr string) error {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > math.MaxUint16 {
		return fmt.Errorf("invalid listen address %q: port must be 0–65535", addr)
	}
	return nil
}
