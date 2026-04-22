/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"fmt"
	"net"
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
	Mode string `mapstructure:"mode"`
	// CertPath is the path to the certificate file (public key).
	CertPath string `mapstructure:"cert-path"`
	// KeyPath is the path to the key file (private key).
	KeyPath     string   `mapstructure:"key-path"`
	CACertPaths []string `mapstructure:"ca-cert-paths"`
	// ServerName is the server name for TLS certificate validation (SNI).
	ServerName string `mapstructure:"server-name"`
}
