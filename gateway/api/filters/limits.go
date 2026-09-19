/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

// Limits caps how many filters and newHeads subscriptions may exist at once.
// Zero fields mean "use the package default".
type Limits struct {
	// MaxFilters is the global concurrent cap on eth_newBlockFilter +
	// eth_newFilter combined. Filters are connection-independent, so this is
	// not attributable per client without auth.
	MaxFilters int

	// MaxSubscriptionsPerConn caps eth_subscribe("newHeads") on a single WS
	// connection. A normal client only needs one.
	MaxSubscriptionsPerConn int

	// MaxSubscriptionsGlobal caps newHeads subscribers across all connections.
	MaxSubscriptionsGlobal int
}

// DefaultLimits are conservative production defaults. Exact values matter less
// than the caps existing and being tunable via config.
var DefaultLimits = Limits{
	MaxFilters:              1000,
	MaxSubscriptionsPerConn: 1,
	MaxSubscriptionsGlobal:  1000,
}

func (l Limits) withDefaults() Limits {
	out := DefaultLimits
	if l.MaxFilters > 0 {
		out.MaxFilters = l.MaxFilters
	}
	if l.MaxSubscriptionsPerConn > 0 {
		out.MaxSubscriptionsPerConn = l.MaxSubscriptionsPerConn
	}
	if l.MaxSubscriptionsGlobal > 0 {
		out.MaxSubscriptionsGlobal = l.MaxSubscriptionsGlobal
	}
	return out
}
