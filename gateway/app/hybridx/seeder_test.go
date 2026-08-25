/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package hybridx

// seedHeight sets fakeDelivery's store height and returns the first block number
// delivery would stream from that height — mirrors throwawaySeeder's expected
// computation so tests can seed claimed/dispatched via seedClaimAt without a
// real peer connection.
func (f *fakeDelivery) seedHeight(height uint64) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.height = height
	if height > 1 {
		return height // delivery resumes at height; peer delivers block height first
	}
	return 0 // delivery resumes at 0; peer delivers block 0 first
}
