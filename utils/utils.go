/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package utils

import "math/big"

type BlockInfo struct {
	BlockNumber *big.Int
	BlockTime   uint64
	GasLimit    uint64
}
