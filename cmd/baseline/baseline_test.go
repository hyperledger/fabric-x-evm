/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustParseMocha(t *testing.T, path string) []Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	results, err := ParseMochaJSON(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return results
}

func TestParseMochaJSON_AllPass(t *testing.T) {
	results := mustParseMocha(t, "testdata/allpass.json")
	want := []Result{
		{ID: "Foo bar test A", Status: StatusPass},
		{ID: "Foo bar test B", Status: StatusPass},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseMochaJSON_AllFail(t *testing.T) {
	results := mustParseMocha(t, "testdata/allfail.json")
	want := []Result{
		{ID: "Foo bar test A", Status: StatusFail, Message: "boom A"},
		{ID: "Foo bar test B", Status: StatusFail, Message: "boom B"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseMochaJSON_Mixed(t *testing.T) {
	results := mustParseMocha(t, "testdata/mixed.json")
	want := []Result{
		{ID: "Foo bar test A", Status: StatusPass},
		{ID: "Foo bar test B", Status: StatusFail, Message: "boom B"},
		{ID: "Foo bar test C", Status: StatusSkip},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseMochaJSON_HookFailure(t *testing.T) {
	// A failing before/beforeEach hook never appears in `tests` — only in
	// `failures`, with its own descriptive fullTitle — but must still surface.
	results := mustParseMocha(t, "testdata/hookfail.json")
	want := []Result{
		{ID: "Foo bar test A", Status: StatusPass},
		{ID: `Foo "before each" hook for "bar test B"`, Status: StatusFail, Message: "boom hook"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func mustParseGoTest(t *testing.T, path string) []Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	results, err := ParseGoTestJSON(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return results
}

func TestParseGoTestJSON_AllPass(t *testing.T) {
	results := mustParseGoTest(t, "testdata/gotest-allpass.json")
	want := []Result{
		{ID: "TestFoo", Status: StatusPass},
		{ID: "TestBar", Status: StatusPass},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseGoTestJSON_AllFail(t *testing.T) {
	results := mustParseGoTest(t, "testdata/gotest-allfail.json")
	want := []Result{
		{ID: "TestFoo", Status: StatusFail, Message: "foo_test.go:10: boom A"},
		{ID: "TestBar", Status: StatusFail, Message: "bar_test.go:20: boom B"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseGoTestJSON_Mixed(t *testing.T) {
	results := mustParseGoTest(t, "testdata/gotest-mixed.json")
	want := []Result{
		{ID: "TestA", Status: StatusPass},
		{ID: "TestB", Status: StatusFail, Message: "b_test.go:5: boom B"},
		{ID: "TestC", Status: StatusSkip},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseGoTestJSON_NestedSubtestsOnlyLeavesReported(t *testing.T) {
	// go test emits its own pass/fail event for every level of a t.Run tree,
	// including the parent, which merely aggregates its children -- only the
	// leaves are meaningful baseline entries.
	results := mustParseGoTest(t, "testdata/gotest-nested.json")
	want := []Result{
		{ID: "TestParent/child_pass", Status: StatusPass},
		{ID: "TestParent/child_fail", Status: StatusFail, Message: "nested_test.go:42: root mismatch"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

func TestParseGoTestJSON_IncompleteTestTreatedAsFailure(t *testing.T) {
	// No pass/fail/skip event ever arrived for this test (e.g. the binary
	// panicked mid-run) -- must surface as a failure, not vanish silently.
	results := mustParseGoTest(t, "testdata/gotest-incomplete.json")
	want := []Result{
		{ID: "TestCrashes", Status: StatusFail, Message: "test did not complete (no pass/fail/skip event — binary may have crashed)"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

// TestParseGoTestJSON_RealCapture replays an actual `go test -json` stream
// (captured from a package with the same TestParent/child_pass, child_fail,
// TestSkipped, TestCrashy shapes as the hand-written fixtures above, panic
// trace trimmed of machine-specific paths) — confirms the hand-written
// fixtures aren't just testing our own assumptions about test2json's shape.
func TestParseGoTestJSON_RealCapture(t *testing.T) {
	results := mustParseGoTest(t, "testdata/gotest-real-capture.json")

	byID := make(map[string]Result, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 leaf results (TestParent itself excluded), got %+v", results)
	}

	if r := byID["TestParent/child_pass"]; r.Status != StatusPass {
		t.Errorf("child_pass = %+v", r)
	}
	if r := byID["TestParent/child_fail"]; r.Status != StatusFail || r.Message != "sample_test.go:11: root mismatch: got 0xabc want 0xdef" {
		t.Errorf("child_fail = %+v", r)
	}
	if r := byID["TestSkipped"]; r.Status != StatusSkip {
		t.Errorf("TestSkipped = %+v", r)
	}
	// TestCrashy's own "--- FAIL: TestCrashy" banner is printed *before* the
	// panic trace output arrives, unlike a normal t.Errorf failure -- exercises
	// that cleanGoTestOutput strips the banner wherever it falls, not just at
	// the end.
	if r := byID["TestCrashy"]; r.Status != StatusFail ||
		!strings.Contains(r.Message, "panic: boom") ||
		strings.Contains(r.Message, "--- FAIL") {
		t.Errorf("TestCrashy = %+v", r)
	}
}

func TestSuiteOf(t *testing.T) {
	cases := []struct{ file, want string }{
		{"/repo/testdata/openzeppelin-contracts/test/token/ERC20/ERC20.test.js", "token"},
		{"/repo/testdata/openzeppelin-contracts/test/finance/VestingWallet.test.js", "finance"},
		{"", ""},
		{"no-test-dir-here.js", "no-test-dir-here.js"},
	}
	for _, c := range cases {
		if got := suiteOf(c.file); got != c.want {
			t.Errorf("suiteOf(%q) = %q, want %q", c.file, got, c.want)
		}
	}
}

func TestBySuite(t *testing.T) {
	// "zzz" is the worse suite (0%) but must still sort after "aaa" (100%) —
	// proves the order is alphabetical, not pass-rate.
	results := []Result{
		{ID: "a", Status: StatusPass, File: "/x/test/aaa/A.test.js"},
		{ID: "b", Status: StatusFail, File: "/x/test/zzz/B.test.js"},
	}

	stats := bySuite(results)

	want := []suiteStats{
		{name: "aaa", pass: 1, fail: 0},
		{name: "zzz", pass: 0, fail: 1},
	}
	if !reflect.DeepEqual(stats, want) {
		t.Fatalf("got %+v, want %+v", stats, want)
	}
}

func TestBaseline_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_failures.json")

	// A missing file is an empty baseline, not an error.
	entries, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty baseline, got %+v", entries)
	}

	want := []Entry{
		{ID: "Foo bar test B", Cause: "signTypedData_v4"},
		{ID: "Foo bar test A"},
	}
	if err := SaveBaseline(path, want); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	// SaveBaseline sorts by ID.
	wantSorted := []Entry{
		{ID: "Foo bar test A"},
		{ID: "Foo bar test B", Cause: "signTypedData_v4"},
	}
	if !reflect.DeepEqual(got, wantSorted) {
		t.Fatalf("got %+v, want %+v", got, wantSorted)
	}
}

// TestSaveBaseline_WritesCharactersVerbatim pins the encoding: encoding/json
// escapes <, > and & by default, which rewrites the many OZ test names holding
// an arrow ("... => bytes24") into an unreadable "=\\u003e" and re-churns the
// diff on every rewrite. Nothing here should come back escaped.
func TestSaveBaseline_WritesCharactersVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_failures.json")

	entries := []Entry{{
		ID:    "Packing pack pack bytes22 + bytes2 => bytes24",
		Cause: "max-code-size",
		Note:  "a & b, x < y, y > x — an em dash too",
	}}
	if err := SaveBaseline(path, entries); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(data, []byte(`\u`)) {
		t.Fatalf("baseline contains escaped characters:\n%s", data)
	}
	for _, want := range []string{"=> bytes24", "a & b", "x < y", "y > x", "—"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("%q missing from written baseline:\n%s", want, data)
		}
	}

	// Verbatim on disk still has to mean unchanged on the way back in.
	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Fatalf("got %+v, want %+v", got, entries)
	}
}

func TestDiff(t *testing.T) {
	results := []Result{
		{ID: "regression", Status: StatusFail, Message: "new break"},
		{ID: "expected-fail", Status: StatusFail, Message: "known issue"},
		{ID: "now-passing", Status: StatusPass},
		{ID: "unlisted-pass", Status: StatusPass},
	}
	baseline := []Entry{
		{ID: "expected-fail", Cause: "some-cause"},
		{ID: "now-passing"},
		{ID: "disappeared"}, // renamed/removed upstream, absent from results entirely
	}

	diff := Diff(results, baseline)

	if len(diff.Regressions) != 1 || diff.Regressions[0].ID != "regression" {
		t.Fatalf("regressions = %+v", diff.Regressions)
	}
	if len(diff.Expected) != 1 || diff.Expected[0].Result.ID != "expected-fail" || diff.Expected[0].Entry.Cause != "some-cause" {
		t.Fatalf("expected = %+v", diff.Expected)
	}
	staleIDs := map[string]bool{}
	for _, e := range diff.Stale {
		staleIDs[e.ID] = true
	}
	if len(diff.Stale) != 2 || !staleIDs["now-passing"] || !staleIDs["disappeared"] {
		t.Fatalf("stale = %+v", diff.Stale)
	}
	if !diff.Regressed() {
		t.Fatal("expected Regressed() to be true (has both a regression and stale entries)")
	}
}

func TestDiff_NothingWrong(t *testing.T) {
	results := []Result{
		{ID: "expected-fail", Status: StatusFail, Message: "known issue"},
		{ID: "unlisted-pass", Status: StatusPass},
	}
	baseline := []Entry{
		{ID: "expected-fail"},
	}

	diff := Diff(results, baseline)
	if diff.Regressed() {
		t.Fatalf("expected clean diff, got regressions=%+v stale=%+v", diff.Regressions, diff.Stale)
	}
}

// TestDiff_StaleAloneDoesNotRegress locks in the policy that Stale, unlike a
// Regression, never gates CI on its own: "no longer failing" is ambiguous (a
// real fix, or just a test that didn't run this time) in a way a new failure
// isn't, so Regressed() must stay false with nothing but stale entries.
func TestDiff_StaleAloneDoesNotRegress(t *testing.T) {
	results := []Result{
		{ID: "now-passing", Status: StatusPass},
	}
	baseline := []Entry{
		{ID: "now-passing"},
		{ID: "disappeared"}, // renamed/removed upstream, absent from results entirely
	}

	diff := Diff(results, baseline)
	if len(diff.Stale) != 2 {
		t.Fatalf("expected both entries stale, got %+v", diff.Stale)
	}
	if diff.Regressed() {
		t.Fatalf("expected Regressed() false with only stale entries, got stale=%+v", diff.Stale)
	}
}

// TestDiff_Quarantined verifies a Flaky entry never lands in Regressions,
// Stale, or Expected, and never trips Regressed(), regardless of whether it
// failed, passed, or didn't run at all this time.
func TestDiff_Quarantined(t *testing.T) {
	results := []Result{
		{ID: "flaky-failing", Status: StatusFail, Message: "arithmetic underflow or overflow"},
		{ID: "flaky-passing", Status: StatusPass},
		// flaky-absent deliberately has no Result at all this run.
		{ID: "real-regression", Status: StatusFail, Message: "genuinely new"},
	}
	baseline := []Entry{
		{ID: "flaky-failing", Flaky: true, Note: "races on concurrent tx"},
		{ID: "flaky-passing", Flaky: true, Note: "races on concurrent tx"},
		{ID: "flaky-absent", Flaky: true, Note: "races on concurrent tx"},
	}

	diff := Diff(results, baseline)

	if len(diff.Regressions) != 1 || diff.Regressions[0].ID != "real-regression" {
		t.Fatalf("regressions = %+v", diff.Regressions)
	}
	if len(diff.Stale) != 0 {
		t.Fatalf("expected no stale entries (flaky ones must not count), got %+v", diff.Stale)
	}
	if len(diff.Expected) != 0 {
		t.Fatalf("expected flaky-failing to land in Quarantined, not Expected, got %+v", diff.Expected)
	}
	byID := make(map[string]QuarantinedResult, len(diff.Quarantined))
	for _, q := range diff.Quarantined {
		byID[q.Entry.ID] = q
	}
	if len(diff.Quarantined) != 3 {
		t.Fatalf("quarantined = %+v", diff.Quarantined)
	}
	if byID["flaky-failing"].Result.Status != StatusFail {
		t.Errorf("flaky-failing: got status %q", byID["flaky-failing"].Result.Status)
	}
	if byID["flaky-passing"].Result.Status != StatusPass {
		t.Errorf("flaky-passing: got status %q", byID["flaky-passing"].Result.Status)
	}
	if byID["flaky-absent"].Result.Status != "" {
		t.Errorf("flaky-absent: expected zero-value Result (didn't run), got %+v", byID["flaky-absent"].Result)
	}

	// A real regression is still real: Regressed() is driven by it alone, not
	// by any of the three flaky outcomes above.
	if !diff.Regressed() {
		t.Fatal("expected Regressed() true from the one real regression")
	}
}

// TestDiff_QuarantinedOnlyMeansClean confirms that when every anomaly is
// Flaky and there's no independent real regression, Regressed() is false —
// this is the entire point of the mechanism.
func TestDiff_QuarantinedOnlyMeansClean(t *testing.T) {
	results := []Result{
		{ID: "flaky-failing", Status: StatusFail, Message: "arithmetic underflow or overflow"},
	}
	baseline := []Entry{
		{ID: "flaky-failing", Flaky: true},
		{ID: "flaky-absent", Flaky: true},
	}

	diff := Diff(results, baseline)
	if diff.Regressed() {
		t.Fatalf("expected Regressed() false, got regressions=%+v stale=%+v", diff.Regressions, diff.Stale)
	}
}

func TestBuildSummary(t *testing.T) {
	results := []Result{
		{ID: "regression", Status: StatusFail, Message: "new break", File: "test/token/ERC20.test.js"},
		{ID: "expected-fail", Status: StatusFail, Message: "known issue", File: "test/token/ERC20.test.js"},
		{ID: "now-passing", Status: StatusPass, File: "test/access/AccessControl.test.js"},
		{ID: "unlisted-pass", Status: StatusPass, File: "test/access/AccessControl.test.js"},
	}
	baseline := []Entry{
		{ID: "expected-fail", Cause: "some-cause"},
		{ID: "now-passing"},
	}

	diff := Diff(results, baseline)
	summary := BuildSummary("oz-hardhat", results, diff)

	if summary.Suite != "oz-hardhat" || summary.Pass != 2 || summary.Fail != 2 || summary.Skip != 0 || summary.Total != 4 {
		t.Fatalf("counts = %+v", summary)
	}
	if summary.SummaryLine != "2 passed, 2 failed, 0 skipped (4 total, 50.0% passing)" {
		t.Fatalf("summaryLine = %q", summary.SummaryLine)
	}
	if len(summary.Regressions) != 1 || summary.Regressions[0] != (Regression{ID: "regression", Message: "new break"}) {
		t.Fatalf("regressions = %+v", summary.Regressions)
	}
	if len(summary.Stale) != 1 || summary.Stale[0] != "now-passing" {
		t.Fatalf("stale = %+v", summary.Stale)
	}
	if len(summary.Causes) != 1 || summary.Causes[0] != (CauseCount{Cause: "some-cause", Count: 1}) {
		t.Fatalf("causes = %+v", summary.Causes)
	}
	if len(summary.BySuite) != 2 {
		t.Fatalf("bySuite = %+v", summary.BySuite)
	}
}

func TestBuildSummary_Quarantined(t *testing.T) {
	results := []Result{
		{ID: "flaky-failing", Status: StatusFail, Message: "arithmetic underflow or overflow"},
	}
	baseline := []Entry{
		{ID: "flaky-failing", Flaky: true, Note: "races on concurrent tx"},
	}

	diff := Diff(results, baseline)
	summary := BuildSummary("oz-hardhat", results, diff)

	if len(summary.Stale) != 0 {
		t.Fatalf("expected no stale entries, got %+v", summary.Stale)
	}
	want := Quarantined{ID: "flaky-failing", Status: StatusFail, Note: "races on concurrent tx"}
	if len(summary.Quarantined) != 1 || summary.Quarantined[0] != want {
		t.Fatalf("quarantined = %+v, want [%+v]", summary.Quarantined, want)
	}
}

func TestBuildSummary_EmptySlicesMarshalAsEmptyArraysNotNull(t *testing.T) {
	results := []Result{{ID: "unlisted-pass", Status: StatusPass}}
	summary := BuildSummary("oz-hardhat", results, Diff(results, nil))

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Every slice field must serialize as "[]", never "null" — a consumer reading
	// this JSON should never need to guard against a missing/null key. bySuite
	// isn't asserted here: a single result always yields exactly one suiteStats
	// bucket (grouped under suiteOf("") == ""), never zero — see TestSuiteOf.
	for _, field := range []string{`"regressions":[]`, `"stale":[]`, `"quarantined":[]`, `"causes":[]`} {
		if !bytes.Contains(data, []byte(field)) {
			t.Fatalf("expected %s in JSON, got %s", field, data)
		}
	}
}

func TestInferCause(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"the method hardhat_mine does not exist/is not available", "hardhat_mine"},
		{"the method eth_signTypedData_v4 does not exist/is not available", "eth_signTypedData_v4"},
		{"insufficient funds for gas * price + value: address 0xabc have 0 want 100", "insufficient-funds"},
		{"execution reverted", ""},
		{"expected 3 to equal 4", ""},
	}
	for _, c := range cases {
		if got := inferCause(c.message); got != c.want {
			t.Errorf("inferCause(%q) = %q, want %q", c.message, got, c.want)
		}
	}
}

func TestReconcile_AutoTagsKnownCauses(t *testing.T) {
	baseline := []Entry{
		{ID: "still failing, untagged"},
	}
	diff := DiffResult{
		Regressions: []Result{
			{ID: "new hardhat_mine failure", Status: StatusFail, Message: "the method hardhat_mine does not exist/is not available"},
		},
		Expected: []ExpectedFailure{
			{
				Result: Result{ID: "still failing, untagged", Status: StatusFail, Message: "the method eth_signTypedData_v4 does not exist/is not available"},
				Entry:  baseline[0],
			},
		},
	}

	got := Reconcile(baseline, diff)

	want := []Entry{
		{ID: "still failing, untagged", Cause: "eth_signTypedData_v4"},
		{ID: "new hardhat_mine failure", Cause: "hardhat_mine"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReconcile(t *testing.T) {
	baseline := []Entry{
		{ID: "expected-fail", Cause: "some-cause"},
		{ID: "now-passing"},
	}
	diff := DiffResult{
		Regressions: []Result{{ID: "regression", Status: StatusFail, Message: "new break"}},
		Stale:       []Entry{{ID: "now-passing"}},
		Expected:    []ExpectedFailure{{Result: Result{ID: "expected-fail"}, Entry: baseline[0]}},
	}

	got := Reconcile(baseline, diff)

	want := []Entry{
		{ID: "expected-fail", Cause: "some-cause"}, // untouched
		{ID: "regression"},                         // newly added, blank cause
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestReconcile_LeavesFlakyEntriesUntouched: a Flaky entry lands in
// Quarantined, not Stale, regardless of outcome — so update must never drop
// it, whether it passed, failed, or didn't run this time. Graduating a Flaky
// entry back to normal is a deliberate human edit, not something update does.
func TestReconcile_LeavesFlakyEntriesUntouched(t *testing.T) {
	baseline := []Entry{
		{ID: "flaky-passing", Flaky: true, Note: "races on concurrent tx"},
	}
	results := []Result{
		{ID: "flaky-passing", Status: StatusPass},
	}

	got := Reconcile(baseline, Diff(results, baseline))

	if !reflect.DeepEqual(got, baseline) {
		t.Fatalf("got %+v, want unchanged %+v", got, baseline)
	}
}
