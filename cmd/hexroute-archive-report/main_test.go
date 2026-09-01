package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const reviewNodeID = metadata.UUID("55555555-5555-4555-8555-555555555555")

var reviewNow = time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return reviewNow }

// steppingClock keeps archived records inside the reviewed window and in a
// stable order, so a report over one archive is the same report twice.
type steppingClock struct {
	base time.Time
	tick int64
}

func (clock *steppingClock) WallNow() time.Time {
	clock.tick++
	return clock.base.Add(time.Duration(clock.tick) * time.Second)
}

func (clock *steppingClock) MonotonicNow() time.Duration {
	return time.Duration(clock.tick) * time.Second
}

func archiveWith(t *testing.T, records int) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "archive")
	archive, err := eventarchive.Open(root, eventarchive.Options{
		NodeID: reviewNodeID,
		Clock:  &steppingClock{base: reviewNow.Add(-24 * time.Hour)},
	})
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	for index := 0; index < records; index++ {
		encoded, err := event.Encode(event.SchemaObservation, event.Observation{
			Component:           control.ComponentNetwork,
			Health:              control.HealthReady,
			Reason:              control.ReasonProbeSucceeded,
			ConsecutiveFailures: uint32(index % 5),
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := archive.Append(encoded); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	return root
}

func review(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runAt(args, &stdout, &stderr, fixedNow)
	return code, stdout.String(), stderr.String()
}

// 4.1 — a dated report for the requested window, and nothing else on disk.
func TestAReviewWritesOneDatedReport(t *testing.T) {
	root := archiveWith(t, 5)
	out := t.TempDir()

	code, stdout, stderr := review(t, "--archive", root, "--out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	expected := filepath.Join(out, "2026-09-02-archive-report.json")
	if !strings.Contains(stdout, expected) {
		t.Fatalf("stdout did not name the report it wrote: %s", stdout)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026-09-02-archive-report.json" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("a review left %v, want one dated report", names)
	}

	raw, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report eventarchive.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Records != 5 {
		t.Fatalf("report covers %d records, archived 5", report.Records)
	}
	digest, err := report.Digested()
	if err != nil {
		t.Fatalf("digested: %v", err)
	}
	if digest != report.Digest {
		t.Fatal("the written report does not match its own digest")
	}
}

// 4.1 — the same archive reviewed twice writes the same bytes, which is what
// makes this week's report comparable with last week's.
func TestTwoReviewsOfOneArchiveAgree(t *testing.T) {
	root := archiveWith(t, 5)
	first, second := t.TempDir(), t.TempDir()

	if code, _, stderr := review(t, "--archive", root, "--out", first); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if code, _, stderr := review(t, "--archive", root, "--out", second); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	name := "2026-09-02-archive-report.json"
	one, err := os.ReadFile(filepath.Join(first, name))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	other, err := os.ReadFile(filepath.Join(second, name))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(one, other) {
		t.Fatal("two reviews of one archive wrote different bytes")
	}
}

// 4.1 — an empty window produces a report that says it is empty, rather than a
// report that reads as a quiet week.
func TestAnEmptyWindowIsReportedAsEmpty(t *testing.T) {
	root := archiveWith(t, 0)
	out := t.TempDir()

	code, stdout, stderr := review(t, "--archive", root, "--out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "held nothing") {
		t.Fatalf("an empty review printed %q", stdout)
	}
	raw, err := os.ReadFile(filepath.Join(out, "2026-09-02-archive-report.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report eventarchive.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !report.Covered.Empty || report.Records != 0 {
		t.Fatalf("an empty window reported %+v", report.Covered)
	}
	if !report.Shortened {
		t.Fatal("a window the archive could not answer for was not marked shortened")
	}
}

// 4.3 — the review does not change what it reviews.
func TestAReviewLeavesTheArchiveUnchanged(t *testing.T) {
	root := archiveWith(t, 4)
	before := archiveState(t, root)

	if code, _, stderr := review(t, "--archive", root, "--out", t.TempDir()); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if after := archiveState(t, root); after != before {
		t.Fatal("a review changed the archive it read")
	}
}

// 4.3 — and a failed review changes nothing either.
func TestAFailedReviewLeavesTheArchiveUnchanged(t *testing.T) {
	root := archiveWith(t, 4)
	before := archiveState(t, root)

	// An output directory that cannot be created fails the review after the
	// archive has already been read.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	code, _, _ := review(t, "--archive", root, "--out", filepath.Join(blocked, "out"))
	if code == 0 {
		t.Fatal("a review that could not write its report reported success")
	}
	if after := archiveState(t, root); after != before {
		t.Fatal("a failed review changed the archive")
	}
}

func archiveState(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	var state strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		state.WriteString(entry.Name())
		state.WriteString(":")
		state.WriteString(strconv.FormatInt(info.Size(), 10))
		state.WriteString(";")
	}
	return state.String()
}

// 4.3 — the review cannot reach a network, run a command or touch credentials.
// Asserting the imports catches the path nobody thought to test.
func TestTheReviewHasNoNetworkOrPrivilege(t *testing.T) {
	banned := []string{
		"net",
		"net/http",
		"os/exec",
		"os/user",
		"/internal/credentials",
		"/internal/policystore",
		"/internal/ipc",
		"/internal/telemetry",
		"/internal/rootdaemon",
		"/internal/userdaemon",
	}
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("import path: %v", err)
			}
			for _, forbidden := range banned {
				if path == forbidden || strings.HasSuffix(path, forbidden) {
					t.Fatalf("%s imports %s; a weekly review runs unattended "+
						"and must not be able to reach anything",
						entry.Name(), path)
				}
			}
		}
	}
}

// 4.1 — arguments it cannot act on are refused rather than guessed at.
func TestIncompleteArgumentsAreRefused(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--archive", "/tmp/x"},
		{"--out", "/tmp/y"},
		{"--archive", "/tmp/x", "--out", "/tmp/y", "--window", "0"},
		{"--archive", "/tmp/x", "--out", "/tmp/y", "extra"},
	} {
		if code, _, _ := review(t, args...); code != 2 {
			t.Fatalf("%v exited %d, want 2", args, code)
		}
	}
}
