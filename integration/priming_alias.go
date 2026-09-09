/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import "github.com/hyperledger/fabric-x-evm/testutil/priming"

// StatePrimer and its constructor moved to testutil/priming so gateway/testimpl can
// reuse them without importing this package (which would be an import cycle: this
// package imports gateway/app, which imports gateway/testimpl). Aliased here so the
// harness and its tests keep referring to them unqualified.
type (
	StatePrimer = priming.StatePrimer
	AllocEntry  = priming.AllocEntry
)

var (
	NewStatePrimer     = priming.NewStatePrimer
	NewNativeEthClient = priming.NewNativeEthClient
)
