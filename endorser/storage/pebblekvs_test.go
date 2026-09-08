/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import "fmt"

// update is a test-only entry point into commitBlock for a raw batch of
// writes, not carrying any guarantee of its own: production always goes
// through Handle, which calls commitBlock directly.
func (p *PebbleKVS) update(updates []KeyValueVersion) error {
	if len(updates) == 0 {
		return nil
	}

	// Sanity check to prevent incorrectly written tests.
	blockNum := updates[0].BlockNum
	for i := range updates {
		if updates[i].BlockNum != blockNum {
			return fmt.Errorf(
				"pebble kvs: update batch spans multiple blocks (%d at index 0, %d at index %d); "+
					"writes must be grouped by block before committing",
				blockNum, updates[i].BlockNum, i)
		}
	}

	return p.commitBlock(blockNum, updates)
}
