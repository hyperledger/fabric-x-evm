/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// parsers maps a --format value to the adapter that turns a runner's raw output
// into []Result. Adding a runner later is "add one entry here."
var parsers = map[string]func([]byte) ([]Result, error){
	"mocha-json": ParseMochaJSON,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: baseline <check|update|tag> [flags]")
		os.Exit(2)
	}

	var code int
	switch os.Args[1] {
	case "check":
		code = runCheck(os.Args[2:])
	case "update":
		code = runUpdate(os.Args[2:])
	case "tag":
		code = runTag(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q (want check, update, or tag)\n", os.Args[1])
		code = 2
	}
	os.Exit(code)
}

type commonFlags struct {
	suite        string
	format       string
	baselinePath string
	resultsGlob  string
}

func parseCommonFlags(name string, args []string) (*commonFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	f := &commonFlags{}
	fs.StringVar(&f.suite, "suite", "", "suite name, for the report header")
	fs.StringVar(&f.format, "format", "mocha-json", "raw results format (mocha-json)")
	fs.StringVar(&f.baselinePath, "baseline", "", "path to the baseline JSON file")
	fs.StringVar(&f.resultsGlob, "results", "", "glob of raw results files to parse")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if f.baselinePath == "" {
		return nil, fmt.Errorf("--baseline is required")
	}
	if f.resultsGlob == "" {
		return nil, fmt.Errorf("--results is required")
	}
	return f, nil
}

// loadResults resolves the --results glob and parses every match with the adapter
// selected by --format, concatenating their results. A test ID repeated across (or
// within) matched files is folded into one entry rather than double-counted — OZ's
// suite genuinely does this (a shared-behavior helper invoked twice in one file
// produces two identical fullTitles) — but only when every occurrence agrees on
// status and message; a real disagreement (e.g. an overlapping --results glob mixing
// two different runs of the same test) is ambiguous and gets rejected instead.
func loadResults(format, resultsGlob string) ([]Result, error) {
	parse, ok := parsers[format]
	if !ok {
		return nil, fmt.Errorf("unknown --format %q", format)
	}

	paths, err := filepath.Glob(resultsGlob)
	if err != nil {
		return nil, fmt.Errorf("invalid --results glob: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("--results %q matched no files", resultsGlob)
	}
	sort.Strings(paths)

	var all []Result
	seen := make(map[string]Result)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read results %s: %w", p, err)
		}
		results, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse results %s: %w", p, err)
		}
		for _, r := range results {
			if prev, dup := seen[r.ID]; dup {
				if prev.Status == r.Status && prev.Message == r.Message {
					continue
				}
				return nil, fmt.Errorf("test ID %q reported inconsistently (%s vs %s) in %s", r.ID, prev.Status, r.Status, p)
			}
			seen[r.ID] = r
			all = append(all, r)
		}
	}
	return all, nil
}

func writeStepSummary(report []byte) error {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open GITHUB_STEP_SUMMARY: %w", err)
	}
	defer f.Close()
	_, err = f.Write(report)
	return err
}

func runCheck(args []string) int {
	f, err := parseCommonFlags("check", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	results, err := loadResults(f.format, f.resultsGlob)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	baseline, err := LoadBaseline(f.baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	diff := Diff(results, baseline)

	var buf bytes.Buffer
	WriteReport(&buf, f.suite, results, diff)
	fmt.Print(buf.String())
	if err := writeStepSummary(buf.Bytes()); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	if diff.Regressed() {
		return 1
	}
	return 0
}

func runUpdate(args []string) int {
	f, err := parseCommonFlags("update", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	results, err := loadResults(f.format, f.resultsGlob)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	baseline, err := LoadBaseline(f.baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	diff := Diff(results, baseline)
	updated := Reconcile(baseline, diff)
	if err := SaveBaseline(f.baselinePath, updated); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	fmt.Printf("%s: %d entries removed, %d entries added, %d total now\n",
		f.baselinePath, len(diff.Stale), len(diff.Regressions), len(updated))
	return 0
}

// tagMatching sets Cause (and Note, if given) on every baseline entry that is
// still failing (present in messageByID) and whose ID or current message
// contains match. Returns how many entries changed. A human decision, made in
// bulk instead of one JSON edit at a time.
//
// An entry that already has a Cause is left untouched unless force is true —
// a broad ID match (e.g. a whole describe block) can otherwise silently
// clobber a more specific tag an earlier pass already got right.
func tagMatching(baseline []Entry, messageByID map[string]string, match, cause, note string, force bool) int {
	n := 0
	for i, e := range baseline {
		if e.Cause != "" && !force {
			continue
		}
		msg, stillFailing := messageByID[e.ID]
		if !stillFailing {
			continue
		}
		if !strings.Contains(e.ID, match) && !strings.Contains(msg, match) {
			continue
		}
		baseline[i].Cause = cause
		if note != "" {
			baseline[i].Note = note
		}
		n++
	}
	return n
}

func runTag(args []string) int {
	fs := flag.NewFlagSet("tag", flag.ContinueOnError)
	baselinePath := fs.String("baseline", "", "path to the baseline JSON file")
	resultsGlob := fs.String("results", "", "glob of raw results files to parse")
	format := fs.String("format", "mocha-json", "raw results format (mocha-json)")
	match := fs.String("match", "", "substring to match against each entry's ID or current failure message")
	cause := fs.String("cause", "", "cause to assign to every matching entry")
	note := fs.String("note", "", "optional note to assign alongside cause")
	force := fs.Bool("force", false, "overwrite entries that already have a cause (default: leave them untouched)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *baselinePath == "" || *resultsGlob == "" || *match == "" || *cause == "" {
		fmt.Fprintln(os.Stderr, "usage: baseline tag --baseline <path> --results <glob> --match <substring> --cause <name> [--note <text>] [--force]")
		return 2
	}

	results, err := loadResults(*format, *resultsGlob)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	baseline, err := LoadBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	diff := Diff(results, baseline)
	messageByID := make(map[string]string, len(diff.Expected))
	for _, exp := range diff.Expected {
		messageByID[exp.Entry.ID] = exp.Result.Message
	}

	n := tagMatching(baseline, messageByID, *match, *cause, *note, *force)
	if err := SaveBaseline(*baselinePath, baseline); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("%s: tagged %d entries matching %q with cause %q\n", *baselinePath, n, *match, *cause)
	return 0
}
