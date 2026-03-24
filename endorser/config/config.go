/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

// Endorser contains configuration for a single endorser peer.
type Endorser struct {
	Name      string
	PeerAddr  string
	PeerTLS   string
	MspDir    string
	MspID     string
	DbConnStr string
}

// Network contains network details shared across components
// and network participants.
type Network struct {
	// Channel is the Fabric channel.
	Channel string

	// Namespace is the namespace for all token transactions.
	Namespace string

	// NsVersion is the version of the namespace, usually 1.0.
	NsVersion string

	// ChainID is the ethereum-style chain ID for this network.
	ChainID int64
}
