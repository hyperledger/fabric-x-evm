/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeMochaFixture(t *testing.T, dir, name, fullTitle, errMessage string) string {
	t.Helper()
	err := ""
	if errMessage != "" {
		err = `"message": ` + `"` + errMessage + `"`
	}
	data := `{
  "stats": {"tests": 1, "passes": 0, "pending": 0, "failures": 0},
  "tests": [{"fullTitle": "` + fullTitle + `", "err": {` + err + `}}],
  "pending": [],
  "failures": [],
  "passes": []
}`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadResults_AgreeingDuplicateIsFolded(t *testing.T) {
	dir := t.TempDir()
	writeMochaFixture(t, dir, "a.json", "shared test", "")
	writeMochaFixture(t, dir, "b.json", "shared test", "")

	results, err := loadResults("mocha-json", filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("loadResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the repeated ID to fold into one result, got %+v", results)
	}
}

func TestLoadResults_ConflictingDuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeMochaFixture(t, dir, "a.json", "shared test", "")
	writeMochaFixture(t, dir, "b.json", "shared test", "boom")

	_, err := loadResults("mocha-json", filepath.Join(dir, "*.json"))
	if err == nil {
		t.Fatal("expected an error for a test ID reported with conflicting outcomes")
	}
}

func TestTagMatching(t *testing.T) {
	baseline := []Entry{
		{ID: "Packing packs"},
		{ID: "Packing extracts"},
		{ID: "unrelated test", Cause: "already-tagged"},
		{ID: "gone upstream"}, // not in messageByID: upstream removed/renamed it
	}
	messageByID := map[string]string{
		"Packing packs":    "could not decode result data (value=\"0x\", ...)",
		"Packing extracts": "could not decode result data (value=\"0x\", ...)",
		"unrelated test":   "something else entirely",
	}

	n := tagMatching(baseline, messageByID, "Packing", "max-code-size", "", false)

	if n != 2 {
		t.Fatalf("expected 2 tagged, got %d", n)
	}
	want := []Entry{
		{ID: "Packing packs", Cause: "max-code-size"},
		{ID: "Packing extracts", Cause: "max-code-size"},
		{ID: "unrelated test", Cause: "already-tagged"},
		{ID: "gone upstream"},
	}
	if !reflect.DeepEqual(baseline, want) {
		t.Fatalf("got %+v, want %+v", baseline, want)
	}
}

func TestTagMatching_MatchesOnMessageToo(t *testing.T) {
	baseline := []Entry{{ID: "some test"}}
	messageByID := map[string]string{"some test": "max code size exceeded: code size 25144 limit 24576"}

	n := tagMatching(baseline, messageByID, "max code size exceeded", "max-code-size", "confirmed via logs", false)

	if n != 1 || baseline[0].Cause != "max-code-size" || baseline[0].Note != "confirmed via logs" {
		t.Fatalf("got %+v", baseline)
	}
}

func TestTagMatching_SkipsAlreadyTaggedUnlessForced(t *testing.T) {
	// A broad ID match (e.g. a whole describe block) shouldn't clobber a more
	// specific cause an earlier, more targeted pass already got right.
	baseline := []Entry{{ID: "AccessControlDefaultAdminRules ERC165 uses less than 30k gas", Cause: "gas-stub"}}
	messageByID := map[string]string{
		"AccessControlDefaultAdminRules ERC165 uses less than 30k gas": "expected 10000000 to be at most 30000.",
	}

	n := tagMatching(baseline, messageByID, "AccessControlDefaultAdminRules", "fixed-block-context", "", false)
	if n != 0 || baseline[0].Cause != "gas-stub" {
		t.Fatalf("expected the existing tag to survive untouched, got %+v", baseline)
	}

	n = tagMatching(baseline, messageByID, "AccessControlDefaultAdminRules", "fixed-block-context", "", true)
	if n != 1 || baseline[0].Cause != "fixed-block-context" {
		t.Fatalf("expected --force to overwrite, got %+v", baseline)
	}
}
