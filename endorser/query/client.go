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
	// BeginView opens a view: a snapshot of committed state that all reads in
	// it see.
	BeginView(ctx context.Context) (viewID string, err error)
	// GetRows returns the committed rows for keys in namespace ns, as seen by
	// the view, or the latest state if viewID is empty. Keys with no committed
	// value are left out.
	GetRows(ctx context.Context, viewID, ns string, keys [][]byte) ([]Row, error)
	// EndView closes a view.
	EndView(ctx context.Context, viewID string) error
	Close() error
}
