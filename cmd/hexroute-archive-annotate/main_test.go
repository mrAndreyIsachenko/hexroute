package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func written(t *testing.T) string {
	t.Helper()
	moment := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	report := eventarchive.Report{
		Schema: eventarchive.ReportSchema, Version: eventarchive.ReportSchemaVersion,
		Covered: eventarchive.Window{
			Records: 2, First: 1, Last: 2, Oldest: moment, Newest: moment,
		},
		Records:     2,
		BySchema:    []eventarchive.SchemaCount{{Schema: "runtime.diagnostic", Count: 1}},
		ByComponent: []eventarchive.ComponentCount{{Component: "runtime", Count: 1}},
		Transitions: []eventarchive.TransitionRun{},
		Rare: []eventarchive.RareFinding{
			{Schema: "runtime.diagnostic", Component: "runtime",
				Reason: "upload_deferred", Count: 1, FirstSequence: 2},
		},
	}
	digest, err := report.Digested()
	if err != nil {
		t.Fatalf("digested: %v", err)
	}
	report.Digest = digest

	_, encoded, err := policy.CanonicalSHA256(report)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "2026-09-02-archive-report.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func annotate(t *testing.T, path string, ask asker, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	full := append([]string{"--report", path}, args...)
	code := runWith(full, &stdout, &stderr, ask)
	return code, stdout.String(), stderr.String()
}

func readBack(t *testing.T, path string) eventarchive.Report {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var report eventarchive.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return report
}

// 3.4 — the three ways a model fails, each left as a complete report that says
// which way it failed.
func TestAFailedModelLeavesACompleteReportThatSaysSo(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		ask     asker
		outcome eventarchive.CommentaryOutcome
	}{
		{
			name: "absent",
			ask: func(context.Context, string, string) (string, error) {
				return "", errModelUnavailable
			},
			outcome: eventarchive.CommentaryUnavailable,
		},
		{
			name: "timed out",
			ask: func(ctx context.Context, _, _ string) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
			outcome: eventarchive.CommentaryTimedOut,
		},
		{
			name: "nonsense",
			ask: func(context.Context, string, string) (string, error) {
				return "I have thought about it and have no comment.", nil
			},
			outcome: eventarchive.CommentaryUnusable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := written(t)
			before := readBack(t, path)

			code, _, stderr := annotate(t, path, testCase.ask, "--timeout", "1s")
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			after := readBack(t, path)
			if after.Pass == nil || after.Pass.Outcome != testCase.outcome {
				t.Fatalf("outcome %+v, want %q", after.Pass, testCase.outcome)
			}
			if after.Records != before.Records ||
				len(after.Rare) != len(before.Rare) {
				t.Fatal("a failed pass changed the report")
			}
			if after.Digest != before.Digest {
				t.Fatal("a failed pass moved the digest")
			}
			for _, finding := range after.Rare {
				if finding.Commentary != "" {
					t.Fatal("a failed pass left commentary behind")
				}
			}
		})
	}
}

// 3.1 — a usable answer adds a sentence and nothing else.
func TestAUsableAnswerOnlyAddsCommentary(t *testing.T) {
	path := written(t)
	before := readBack(t, path)

	code, stdout, stderr := annotate(t, path,
		func(context.Context, string, string) (string, error) {
			return `[{"finding": 1, "comment": "an upload has been waiting"}]`, nil
		})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	after := readBack(t, path)
	if after.Rare[0].Commentary == "" {
		t.Fatal("nothing was attached")
	}
	if after.Digest != before.Digest {
		t.Fatal("attaching commentary moved the digest")
	}
	if after.Pass.Attached != 1 {
		t.Fatalf("recorded %d attached, want 1", after.Pass.Attached)
	}
	if !strings.Contains(stdout, "attached") {
		t.Fatalf("stdout did not say what happened: %s", stdout)
	}
}

// A report that does not agree with its own digest is not one to annotate.
// Adding notes would produce something that looks checked.
func TestAReportThatFailsItsOwnDigestIsRefused(t *testing.T) {
	path := written(t)
	report := readBack(t, path)
	report.Records = 99
	_, encoded, err := policy.CanonicalSHA256(report)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	code, _, stderr := annotate(t, path,
		func(context.Context, string, string) (string, error) {
			t.Fatal("the model was asked about a report that does not check out")
			return "", nil
		})
	if code == 0 {
		t.Fatal("a report failing its own digest was annotated")
	}
	if !strings.Contains(stderr, "digest") {
		t.Fatalf("the refusal did not say why: %s", stderr)
	}
}

// 3.1 — off by default means nothing runs it. The weekly review installs and
// schedules the report command; the annotator is a thing someone chooses.
func TestNothingSchedulesTheAnnotator(t *testing.T) {
	for _, path := range []string{
		"../../scripts/macos/archive-review-run.sh",
		"../../scripts/macos/archive-review-launchd.sh",
		"../../deploy/macos/com.hexroute.observe.archive-review.plist",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(raw), "archive-annotate") {
			t.Fatalf("%s runs the annotator; the model pass is off by default "+
				"and a schedule is not a choice anyone made this week", path)
		}
	}
}

// Arguments it cannot act on are refused rather than guessed at.
func TestIncompleteArgumentsAreRefused(t *testing.T) {
	refusing := func(context.Context, string, string) (string, error) {
		return "", errors.New("should not be asked")
	}
	for _, args := range [][]string{
		{},
		{"--model", "x"},
		{"--report", "/tmp/x", "--timeout", "0"},
		{"--report", "/tmp/x", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runWith(args, &stdout, &stderr, refusing); code != 2 {
			t.Fatalf("%v exited %d, want 2", args, code)
		}
	}
}
