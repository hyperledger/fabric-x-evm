/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"os"
	"path/filepath"
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
