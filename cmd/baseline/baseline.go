/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
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
// or go test's hierarchical subtest name) — never reformatted. File is the
// source file the runner attributes the test to, if it reports one; used only
// to group the report by suite, never persisted to a baseline Entry.
type Result struct {
	ID      string
	Status  Status
	Message string
	File    string
}

// Entry is one checked-in baseline record: a test expected to fail today.
// Cause and Note are optional human annotations, filled in opportunistically —
// never required for an entry to be valid. Flaky marks a test whose pass/fail
// outcome is not deterministic (e.g. a race condition, not a clean incompatibility);
// see Diff and Quarantined for how that changes gating.
type Entry struct {
	ID    string `json:"id"`
	Cause string `json:"cause,omitempty"`
	Note  string `json:"note,omitempty"`
	Flaky bool   `json:"flaky,omitempty"`
}

// ExpectedFailure pairs a currently-failing result with the baseline entry that
// expects it, so the report can show the entry's cause tag alongside the failure.
type ExpectedFailure struct {
	Result Result
	Entry  Entry
}

// QuarantinedResult pairs a Flaky baseline entry with whatever it actually did
// this run. Result is the zero value (Status "") if the test didn't run at all —
// which, for a flaky entry, is itself an unreliable signal, not evidence the
// test was removed.
type QuarantinedResult struct {
	Result Result
	Entry  Entry
}

// DiffResult is the outcome of comparing current results against a baseline.
type DiffResult struct {
	Regressions []Result            // failing, not in the baseline
	Stale       []Entry             // in the baseline (and not Flaky), but not failing (or missing) now
	Expected    []ExpectedFailure   // failing, in the baseline, not Flaky — the normal case
	Quarantined []QuarantinedResult // Flaky baseline entries, whatever they did this run — never gates
}

// Regressed reports whether the diff should fail CI: any regression, or any stale
// entry (a listed test that no longer fails is exactly as much "baseline doesn't
// match reality" as a new failure). Quarantined (Flaky) entries never contribute
// here, by design.
func (d DiffResult) Regressed() bool {
	return len(d.Regressions) > 0 || len(d.Stale) > 0
}

// mochaTest mirrors the fields we need from Mocha's built-in --reporter json output.
// Every test (pass, fail, or pending) appears in mochaReport.Tests with the same
// shape; err is an empty object for a pass, or has Message set for a failure.
type mochaTest struct {
	FullTitle string   `json:"fullTitle"`
	File      string   `json:"file"`
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
		r := Result{ID: t.FullTitle, File: t.File}
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
		results = append(results, Result{ID: f.FullTitle, Status: StatusFail, Message: f.Err.Message, File: f.File})
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

// encodeJSON renders v as indented JSON with encoding/json's HTML escaping turned off.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil { // Encode already ends the output with a newline
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderBaseline is the canonical on-disk form of a baseline: sorted by ID for
// stable, reviewable diffs, and encoded the one way this tool encodes.
func renderBaseline(entries []Entry) ([]byte, error) {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return encodeJSON(sorted)
}

// SaveBaseline writes a baseline file in canonical form (see renderBaseline).
func SaveBaseline(path string, entries []Entry) error {
	data, err := renderBaseline(entries)
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}

// Diff compares current results against a baseline: failing-and-listed is
// expected (the normal case), failing-and-unlisted is a regression, and
// listed-but-not-failing (including a listed ID that no longer appears in
// results at all — renamed or removed upstream) is stale and should be
// removed from the baseline. A Flaky entry is carved out of all three of
// those into Quarantined instead, regardless of what it did this run — its
// outcome isn't reliable enough to gate on, by definition.
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
		case listed && entry.Flaky:
			out.Quarantined = append(out.Quarantined, QuarantinedResult{Result: r, Entry: entry})
		case r.Status == StatusFail && listed:
			out.Expected = append(out.Expected, ExpectedFailure{Result: r, Entry: entry})
		case r.Status == StatusFail && !listed:
			out.Regressions = append(out.Regressions, r)
		case r.Status != StatusFail && listed:
			out.Stale = append(out.Stale, entry)
		}
	}

	// Baseline entries whose ID never appeared in this run at all (upstream
	// renamed/removed the test) are safe to remove, same as a passing test —
	// unless Flaky, in which case "didn't run" is just as unreliable a signal
	// as any other outcome, so it goes to Quarantined instead.
	for _, e := range baseline {
		if seen[e.ID] {
			continue
		}
		if e.Flaky {
			out.Quarantined = append(out.Quarantined, QuarantinedResult{Entry: e})
			continue
		}
		out.Stale = append(out.Stale, e)
	}

	return out
}

// causeSignature is a high-confidence, mechanical rule for deriving a cause from
// a failure message: the message names its own cause (a missing RPC method) —
// never a guess at what a generic assertion diff is actually about. Order
// matters: first match wins.
var causeSignatures = []struct {
	pattern *regexp.Regexp
	cause   func(match []string) string
}{
	{
		pattern: regexp.MustCompile(`^the method (\S+) does not exist/is not available$`),
		cause:   func(m []string) string { return m[1] },
	},
	{
		pattern: regexp.MustCompile(`^insufficient funds for gas \* price \+ value`),
		cause:   func(m []string) string { return "insufficient-funds" },
	},
}

// inferCause returns the cause tag for a message matching a known signature
// above, or "" if none match — left for a human to tag opportunistically.
func inferCause(message string) string {
	for _, sig := range causeSignatures {
		if m := sig.pattern.FindStringSubmatch(message); m != nil {
			return sig.cause(m)
		}
	}
	return ""
}

// Reconcile computes the updated baseline for `update`: drop stale entries, add
// an entry for every regression, and backfill `cause` (via inferCause) for any
// entry — new or existing — that doesn't already have one. An existing cause,
// however it was set, is never overwritten.
func Reconcile(baseline []Entry, diff DiffResult) []Entry {
	stale := make(map[string]bool, len(diff.Stale))
	for _, e := range diff.Stale {
		stale[e.ID] = true
	}
	messageByID := make(map[string]string, len(diff.Expected))
	for _, exp := range diff.Expected {
		messageByID[exp.Entry.ID] = exp.Result.Message
	}

	out := make([]Entry, 0, len(baseline)+len(diff.Regressions))
	for _, e := range baseline {
		if stale[e.ID] {
			continue
		}
		if e.Cause == "" {
			e.Cause = inferCause(messageByID[e.ID])
		}
		out = append(out, e)
	}
	for _, r := range diff.Regressions {
		out = append(out, Entry{ID: r.ID, Cause: inferCause(r.Message)})
	}
	return out
}

// passRate is the percentage of executed (non-skipped) results that passed.
// Skipped results are excluded from the denominator — they're neither a pass
// nor a fail, so including them would dilute the number in either direction.
func passRate(pass, fail int) float64 {
	if pass+fail == 0 {
		return 0
	}
	return float64(pass) / float64(pass+fail) * 100
}

// suiteOf derives a coarse grouping label from a test's source file path — the
// first path segment under "test/", e.g. ".../test/token/ERC20/ERC20.test.js"
// -> "token". Falls back to the full file (or "" if the runner didn't report
// one, e.g. go test has no per-test file) when the path doesn't have that shape.
func suiteOf(file string) string {
	const marker = "/test/"
	idx := strings.LastIndex(file, marker)
	if idx == -1 {
		return file
	}
	rest := file[idx+len(marker):]
	if slash := strings.Index(rest, "/"); slash != -1 {
		return rest[:slash]
	}
	return rest
}

type suiteStats struct {
	name             string
	pass, fail, skip int
}

// bySuite buckets results by suiteOf(r.File), sorted alphabetically by name —
// a stable order to scan or diff against a previous run, independent of any
// run's actual pass rates.
func bySuite(results []Result) []suiteStats {
	stats := make(map[string]*suiteStats)
	var order []string
	for _, r := range results {
		name := suiteOf(r.File)
		s, ok := stats[name]
		if !ok {
			s = &suiteStats{name: name}
			stats[name] = s
			order = append(order, name)
		}
		switch r.Status {
		case StatusPass:
			s.pass++
		case StatusFail:
			s.fail++
		case StatusSkip:
			s.skip++
		}
	}

	out := make([]suiteStats, 0, len(order))
	for _, name := range order {
		out = append(out, *stats[name])
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].name < out[j].name
	})
	return out
}

// WriteReport prints a human-readable summary: headline counts and pass rate, a
// per-suite breakdown (to see where compatibility is improving), regressions,
// stale entries, quarantined (Flaky) entries, and a cause histogram of expected
// failures (grouped by the entry's Cause tag, falling back to the raw failure
// message when blank).
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
	fmt.Fprintf(w, "%d passed, %d failed, %d skipped (%d total, %.1f%% passing)\n\n",
		pass, fail, skip, len(results), passRate(pass, fail))

	if suites := bySuite(results); len(suites) > 1 {
		fmt.Fprintf(w, "## By suite\n\n")
		for _, s := range suites {
			fmt.Fprintf(w, "- %s: %d/%d passing (%.0f%%)\n", s.name, s.pass, s.pass+s.fail, passRate(s.pass, s.fail))
		}
		fmt.Fprintln(w)
	}

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

	if len(diff.Quarantined) > 0 {
		fmt.Fprintf(w, "## Quarantined (flaky) (%d) — for visibility only, never gates\n\n", len(diff.Quarantined))
		for _, q := range diff.Quarantined {
			status := "did not run"
			if q.Result.Status != "" {
				status = string(q.Result.Status)
			}
			note := q.Entry.Note
			if note == "" {
				note = "(no note)"
			}
			fmt.Fprintf(w, "- `%s`: %s — %s\n", q.Entry.ID, status, note)
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

// Summary is check's machine-readable counterpart to WriteReport's prose: the
// same underlying results/diff, structured for a caller that wants to build its
// own presentation (e.g. a CI bot posting a PR comment) instead of re-parsing
// rendered text. Every slice field is always present (never omitted/null), even
// when empty, so a consumer never needs to guard against a missing key.
type Summary struct {
	Suite       string         `json:"suite"`
	Pass        int            `json:"pass"`
	Fail        int            `json:"fail"`
	Skip        int            `json:"skip"`
	Total       int            `json:"total"`
	PassRate    float64        `json:"passRate"`
	SummaryLine string         `json:"summaryLine"` // exactly WriteReport's stats line, so the two never drift apart
	BySuite     []SuiteSummary `json:"bySuite"`
	Regressions []Regression   `json:"regressions"`
	Stale       []string       `json:"stale"`
	Quarantined []Quarantined  `json:"quarantined"`
	Causes      []CauseCount   `json:"causes"`
}

type SuiteSummary struct {
	Name     string  `json:"name"`
	Pass     int     `json:"pass"`
	Total    int     `json:"total"`
	PassRate float64 `json:"passRate"`
}

type Regression struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type CauseCount struct {
	Cause string `json:"cause"`
	Count int    `json:"count"`
}

// Quarantined is a Flaky entry's outcome this run, for the JSON summary.
// Status is "" if the test didn't run at all this suite.
type Quarantined struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
	Note   string `json:"note,omitempty"`
}

// BuildSummary computes Summary from the same inputs WriteReport renders from,
// reusing its helpers (passRate, bySuite, groupExpected) so the two can never
// disagree on the underlying numbers, only on presentation.
func BuildSummary(suite string, results []Result, diff DiffResult) Summary {
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
	rate := passRate(pass, fail)

	bySuiteList := []SuiteSummary{}
	for _, s := range bySuite(results) {
		bySuiteList = append(bySuiteList, SuiteSummary{
			Name:     s.name,
			Pass:     s.pass,
			Total:    s.pass + s.fail,
			PassRate: passRate(s.pass, s.fail),
		})
	}

	regressions := []Regression{}
	for _, r := range diff.Regressions {
		regressions = append(regressions, Regression{ID: r.ID, Message: r.Message})
	}

	stale := []string{}
	for _, e := range diff.Stale {
		stale = append(stale, e.ID)
	}

	quarantined := []Quarantined{}
	for _, q := range diff.Quarantined {
		quarantined = append(quarantined, Quarantined{ID: q.Entry.ID, Status: q.Result.Status, Note: q.Entry.Note})
	}

	causes := []CauseCount{}
	for _, group := range groupExpected(diff.Expected) {
		causes = append(causes, CauseCount{Cause: group.key, Count: len(group.items)})
	}

	return Summary{
		Suite:    suite,
		Pass:     pass,
		Fail:     fail,
		Skip:     skip,
		Total:    len(results),
		PassRate: rate,
		SummaryLine: fmt.Sprintf("%d passed, %d failed, %d skipped (%d total, %.1f%% passing)",
			pass, fail, skip, len(results), rate),
		BySuite:     bySuiteList,
		Regressions: regressions,
		Stale:       stale,
		Quarantined: quarantined,
		Causes:      causes,
	}
}
