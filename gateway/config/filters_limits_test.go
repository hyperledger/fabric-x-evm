/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import "testing"

func TestFilters_ResolvedLimits_OmittedVsExplicitZero(t *testing.T) {
	defF, defP, defG := 1000, 1, 1000

	// Nil Filters → defaults
	f, p, g := (*Filters)(nil).ResolvedLimits(defF, defP, defG)
	if f != defF || p != defP || g != defG {
		t.Fatalf("nil Filters: got %d,%d,%d", f, p, g)
	}

	// Omitted fields (nil pointers) → defaults
	empty := &Filters{}
	f, p, g = empty.ResolvedLimits(defF, defP, defG)
	if f != defF || p != defP || g != defG {
		t.Fatalf("omitted fields: got %d,%d,%d", f, p, g)
	}

	// Explicit 0 must stay 0
	zero := 0
	explicit := &Filters{MaxFilters: &zero}
	f, p, g = explicit.ResolvedLimits(defF, defP, defG)
	if f != 0 {
		t.Fatalf("explicit 0 became %d", f)
	}
	if p != defP || g != defG {
		t.Fatalf("other fields: got %d,%d", p, g)
	}
}
