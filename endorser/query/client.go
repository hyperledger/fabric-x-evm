/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package query lets an endorser read world state from the committer's query
// service instead of keeping its own copy.
package query

import "context"

// Row is one committed key, with its value and MVCC version.
type Row struct {
	Key     []byte
	Value   []byte
	Version uint64
}

// Client reads committed state.
type Client interface {
	// GetRows returns the current committed rows for keys in namespace ns.
	// Keys with no committed value are left out.
	GetRows(ctx context.Context, ns string, keys [][]byte) ([]Row, error)
	Close() error
}
