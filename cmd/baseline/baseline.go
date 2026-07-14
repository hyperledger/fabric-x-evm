/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// Status is the outcome of a single test, normalized across runner formats.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Result is one test's outcome, normalized from whatever runner produced it.
// ID is the runner's own full test name, used verbatim (Mocha's fullTitle,
// or go test's hierarchical subtest name) — never reformatted.
type Result struct {
	ID      string
	Status  Status
	Message string
}

// Entry is one checked-in baseline record: a test expected to fail today.
// Cause and Note are optional human annotations, filled in opportunistically —
// never required for an entry to be valid (see BASELINE_PLAN.md's "Two workflows").
type Entry struct {
	ID    string `json:"id"`
	Cause string `json:"cause,omitempty"`
	Note  string `json:"note,omitempty"`
}

// ExpectedFailure pairs a currently-failing result with the baseline entry that
// expects it, so the report can show the entry's cause tag alongside the failure.
type ExpectedFailure struct {
	Result Result
	Entry  Entry
}

// DiffResult is the outcome of comparing current results against a baseline.
type DiffResult struct {
	Regressions []Result          // failing, not in the baseline
	Stale       []Entry           // in the baseline, but not failing (or missing) now
	Expected    []ExpectedFailure // failing, in the baseline — the normal case
}

// Regressed reports whether the diff should fail CI: any regression, or any stale
// entry (a listed test that no longer fails is exactly as much "baseline doesn't
// match reality" as a new failure — see BASELINE_PLAN.md's diff semantics).
func (d DiffResult) Regressed() bool {
	return len(d.Regressions) > 0 || len(d.Stale) > 0
}

// mochaTest mirrors the fields we need from Mocha's built-in --reporter json output.
// Every test (pass, fail, or pending) appears in mochaReport.Tests with the same
// shape; err is an empty object for a pass, or has Message set for a failure.
type mochaTest struct {
	FullTitle string   `json:"fullTitle"`
	Err       mochaErr `json:"err"`
}

type mochaErr struct {
	Message string `json:"message"`
}

// mochaReport is the top-level shape of Mocha's --reporter json output.
// Tests is the authoritative list of every test that ran; Pending duplicates the
// subset that were skipped (Mocha gives them empty err too, so Pending is the only
// way to tell a skip apart from a pass). Failures duplicates every failing test too,
// but is also the *only* place a failing before/beforeEach hook shows up — a hook
// isn't itself a test, so it never appears in Tests at all.
type mochaReport struct {
	Tests    []mochaTest `json:"tests"`
	Pending  []mochaTest `json:"pending"`
	Failures []mochaTest `json:"failures"`
}

// ParseMochaJSON converts Mocha's built-in --reporter json output into []Result.
func ParseMochaJSON(data []byte) ([]Result, error) {
	var report mochaReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse mocha json: %w", err)
	}

	pending := make(map[string]bool, len(report.Pending))
	for _, t := range report.Pending {
		pending[t.FullTitle] = true
	}

	results := make([]Result, 0, len(report.Tests))
	seen := make(map[string]bool, len(report.Tests))
	for _, t := range report.Tests {
		seen[t.FullTitle] = true
		r := Result{ID: t.FullTitle}
		switch {
		case pending[t.FullTitle]:
			r.Status = StatusSkip
		case t.Err.Message != "":
			r.Status = StatusFail
			r.Message = t.Err.Message
		default:
			r.Status = StatusPass
		}
		results = append(results, r)
	}

	// A hook failure (e.g. a beforeEach that throws) has its own descriptive
	// fullTitle (Mocha's own ID for it) and is otherwise indistinguishable from a
	// regular failure — surface it the same way, or it silently vanishes.
	for _, f := range report.Failures {
		if seen[f.FullTitle] {
			continue
		}
		results = append(results, Result{ID: f.FullTitle, Status: StatusFail, Message: f.Err.Message})
	}
	return results, nil
}

// LoadBaseline reads a baseline file. A missing file is an empty baseline, not an
// error, so `update` can create one from scratch (initial seeding).
func LoadBaseline(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read baseline: %w", err)
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return entries, nil
}

// SaveBaseline writes a baseline file, sorted by ID for stable, reviewable diffs.
func SaveBaseline(path string, entries []Entry) error {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}

// Diff compares current results against a baseline. See BASELINE_PLAN.md's "Diff
// semantics" for the 4-way matrix this implements.
func Diff(results []Result, baseline []Entry) DiffResult {
	byID := make(map[string]Entry, len(baseline))
	for _, e := range baseline {
		byID[e.ID] = e
	}
	seen := make(map[string]bool, len(results))

	var out DiffResult
	for _, r := range results {
		seen[r.ID] = true
		entry, listed := byID[r.ID]

		switch {
		case r.Status == StatusFail && listed:
			out.Expected = append(out.Expected, ExpectedFailure{Result: r, Entry: entry})
		case r.Status == StatusFail && !listed:
			out.Regressions = append(out.Regressions, r)
		case r.Status != StatusFail && listed:
			out.Stale = append(out.Stale, entry)
		}
	}

	// Baseline entries whose ID never appeared in this run at all (upstream
	// renamed/removed the test) are safe to remove, same as a passing test.
	for _, e := range baseline {
		if !seen[e.ID] {
			out.Stale = append(out.Stale, e)
		}
	}

	return out
}

// Reconcile computes the updated baseline for `update`: drop stale entries, add a
// blank-cause entry for every regression. Existing entries (including their cause/
// note tags) are left untouched.
func Reconcile(baseline []Entry, diff DiffResult) []Entry {
	stale := make(map[string]bool, len(diff.Stale))
	for _, e := range diff.Stale {
		stale[e.ID] = true
	}

	out := make([]Entry, 0, len(baseline)+len(diff.Regressions))
	for _, e := range baseline {
		if !stale[e.ID] {
			out = append(out, e)
		}
	}
	for _, r := range diff.Regressions {
		out = append(out, Entry{ID: r.ID})
	}
	return out
}

// WriteReport prints a human-readable summary: headline counts, regressions, stale
// entries, and a cause histogram of expected failures (grouped by the entry's Cause
// tag, falling back to the raw failure message when Cause is blank).
func WriteReport(w io.Writer, suite string, results []Result, diff DiffResult) {
	var pass, fail, skip int
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			pass++
		case StatusFail:
			fail++
		case StatusSkip:
			skip++
		}
	}

	fmt.Fprintf(w, "# Baseline check: %s\n\n", suite)
	fmt.Fprintf(w, "%d passed, %d failed, %d skipped (%d total)\n\n", pass, fail, skip, len(results))

	if len(diff.Regressions) > 0 {
		fmt.Fprintf(w, "## Regressions (%d)\n\n", len(diff.Regressions))
		for _, r := range diff.Regressions {
			fmt.Fprintf(w, "- `%s`: %s\n", r.ID, r.Message)
		}
		fmt.Fprintln(w)
	}

	if len(diff.Stale) > 0 {
		fmt.Fprintf(w, "## Stale baseline entries (%d) — remove these\n\n", len(diff.Stale))
		for _, e := range diff.Stale {
			fmt.Fprintf(w, "- `%s`\n", e.ID)
		}
		fmt.Fprintln(w)
	}

	if len(diff.Expected) > 0 {
		fmt.Fprintf(w, "## Expected failures by cause (%d)\n\n", len(diff.Expected))
		for _, group := range groupExpected(diff.Expected) {
			fmt.Fprintf(w, "- %s: %d\n", group.key, len(group.items))
		}
		fmt.Fprintln(w)
	}
}

type expectedGroup struct {
	key   string
	items []ExpectedFailure
}

// groupExpected buckets expected failures by cause (falling back to the raw
// message when untagged), sorted by group size descending — the ROI ranking.
func groupExpected(expected []ExpectedFailure) []expectedGroup {
	groups := make(map[string][]ExpectedFailure)
	for _, e := range expected {
		key := e.Entry.Cause
		if key == "" {
			key = e.Result.Message
		}
		groups[key] = append(groups[key], e)
	}

	out := make([]expectedGroup, 0, len(groups))
	for key, items := range groups {
		out = append(out, expectedGroup{key: key, items: items})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].items) != len(out[j].items) {
			return len(out[i].items) > len(out[j].items)
		}
		return out[i].key < out[j].key
	})
	return out
}
