/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

// Limits caps how many filters and newHeads subscriptions may exist at once,
// and how large each filter's pending buffer may grow.
//
// Zero MaxFilters / MaxSubscriptions* means that limit is actually zero (nothing
// allowed), not "use the default". Callers that want defaults should pass
// DefaultLimits or use NewFilterAPI. Partial test overrides should start from
// DefaultLimits and change only the fields they care about.
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

	// MaxFilterBuffer caps pending hashes (block filters) or logs (log filters)
	// buffered for GetFilterChanges. When full, oldest entries are dropped.
	MaxFilterBuffer int
}

// DefaultLimits are conservative production defaults. Exact values matter less
// than the caps existing and being tunable via config.
var DefaultLimits = Limits{
	MaxFilters:              1000,
	MaxSubscriptionsPerConn: 1,
	MaxSubscriptionsGlobal:  1000,
	MaxFilterBuffer:         1024,
}

// mergePartialLimits fills zero MaxFilterBuffer from DefaultLimits so existing
// tests that only override count caps keep a sane buffer size. Count caps that
// are zero stay zero (unlike the old withDefaults behavior).
func mergePartialLimits(l Limits) Limits {
	out := l
	if out.MaxFilterBuffer <= 0 {
		out.MaxFilterBuffer = DefaultLimits.MaxFilterBuffer
	}
	return out
}
