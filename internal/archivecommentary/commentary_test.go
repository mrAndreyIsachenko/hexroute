package archivecommentary

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
)

func report() eventarchive.Report {
	moment := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	built := eventarchive.Report{
		Schema: eventarchive.ReportSchema, Version: eventarchive.ReportSchemaVersion,
		Covered: eventarchive.Window{
			Records: 3, First: 1, Last: 3, Oldest: moment, Newest: moment,
		},
		Records: 3,
		BySchema: []eventarchive.SchemaCount{
			{Schema: "component.observation", Count: 2},
		},
		ByComponent: []eventarchive.ComponentCount{
			{Component: "network", Count: 2},
		},
		Transitions: []eventarchive.TransitionRun{},
		Rare: []eventarchive.RareFinding{
			{Schema: "runtime.diagnostic", Component: "runtime",
				Reason: "upload_deferred", Count: 1, FirstSequence: 2},
			{Schema: "component.observation", Component: "network",
				Reason: "probe_succeeded", Count: 2, FirstSequence: 1},
		},
	}
	digest, err := built.Digested()
	if err != nil {
		panic(err)
	}
	built.Digest = digest
	return built
}

// 3.3 — the model may move one field and nothing else. Everything the ranking
// decided is identical, and the digest, taken over everything but the
// commentary, has not moved.
func TestOnlyTheCommentaryFieldDiffers(t *testing.T) {
	before := report()
	after, result, err := Attach(before, "test-model",
		`[{"finding": 1, "comment": "this one has not been seen before"}]`)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if result.Attached != 1 || result.Rejected != 0 {
		t.Fatalf("attached %d rejected %d, want 1 and 0",
			result.Attached, result.Rejected)
	}
	if after.Rare[0].Commentary == "" {
		t.Fatal("nothing was attached to the finding the model named")
	}

	stripped := after
	stripped.Rare = make([]eventarchive.RareFinding, len(after.Rare))
	copy(stripped.Rare, after.Rare)
	for index := range stripped.Rare {
		stripped.Rare[index].Commentary = ""
	}
	stripped.Pass = nil
	if !reflect.DeepEqual(before, stripped) {
		t.Fatalf("the model changed more than commentary:\n before %+v\n after %+v",
			before, stripped)
	}

	digest, err := after.Digested()
	if err != nil {
		t.Fatalf("digested: %v", err)
	}
	if digest != before.Digest {
		t.Fatal("attaching commentary moved the digest, so a week with a " +
			"model reads as a different week from one without")
	}
}

// 3.2 — an answer that is not an answer is discarded, and the report survives
// it whole.
func TestUnparsableAnswersAreDiscarded(t *testing.T) {
	for _, answer := range []string{
		"",
		"I think the network looks fine this week.",
		"[{finding: 1}]",
		`{"finding": 1, "comment": "not an array"}`,
		"[" + strings.Repeat("x", MaxAnswerBytes) + "]",
	} {
		before := report()
		after, result, err := Attach(before, "test-model", answer)
		if !errors.Is(err, ErrUnusable) {
			t.Fatalf("answer %q gave %v, want %v", truncate(answer), err, ErrUnusable)
		}
		if result.Attached != 0 {
			t.Fatalf("answer %q attached %d notes", truncate(answer), result.Attached)
		}
		if after.Pass == nil || after.Pass.Outcome != eventarchive.CommentaryUnusable {
			t.Fatalf("answer %q did not record itself as unusable", truncate(answer))
		}
		for _, finding := range after.Rare {
			if finding.Commentary != "" {
				t.Fatalf("answer %q left commentary behind", truncate(answer))
			}
		}
		digest, err := after.Digested()
		if err != nil {
			t.Fatalf("digested: %v", err)
		}
		if digest != before.Digest {
			t.Fatalf("answer %q moved the digest", truncate(answer))
		}
	}
}

func truncate(value string) string {
	if len(value) > 40 {
		return value[:40] + "..."
	}
	return value
}

// 3.2 — a note about a finding this report does not contain is refused
// whatever it says. The model may be describing something real; it is not
// describing this week.
func TestCommentaryAboutAFindingThatIsNotThereIsRefused(t *testing.T) {
	before := report()
	after, result, err := Attach(before, "test-model", `[
	  {"finding": 99, "comment": "about something else entirely"},
	  {"finding": 0, "comment": "about nothing"},
	  {"finding": -1, "comment": "about less than nothing"},
	  {"finding": 1, "comment": ""}
	]`)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if result.Attached != 0 || result.Rejected != 4 {
		t.Fatalf("attached %d rejected %d, want 0 and 4",
			result.Attached, result.Rejected)
	}
	if after.Pass.Outcome != eventarchive.CommentaryUnusable {
		t.Fatalf("outcome %q, want unusable", after.Pass.Outcome)
	}
	for _, finding := range after.Rare {
		if finding.Commentary != "" {
			t.Fatal("a refused note reached a finding")
		}
	}
}

// The same finding twice is one note, not two. A model that repeats itself
// must not be able to overwrite what it already said.
func TestASecondNoteOnOneFindingIsRefused(t *testing.T) {
	after, result, err := Attach(report(), "test-model", `[
	  {"finding": 1, "comment": "first"},
	  {"finding": 1, "comment": "second"}
	]`)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if result.Attached != 1 || result.Rejected != 1 {
		t.Fatalf("attached %d rejected %d, want 1 and 1",
			result.Attached, result.Rejected)
	}
	if after.Rare[0].Commentary != "first" {
		t.Fatalf("commentary is %q, want the first note",
			after.Rare[0].Commentary)
	}
}

// A note longer than the bound is cut rather than refused. The model saying
// too much is not the same failure as it saying the wrong thing.
func TestAnOverlongNoteIsBounded(t *testing.T) {
	long := strings.Repeat("word ", 400)
	after, _, err := Attach(report(), "test-model",
		`[{"finding": 1, "comment": "`+long+`"}]`)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(after.Rare[0].Commentary) != MaxCommentaryBytes {
		t.Fatalf("commentary is %d bytes, want it bounded to %d",
			len(after.Rare[0].Commentary), MaxCommentaryBytes)
	}
}

// 3.4 — the report is complete and valid when no model ran, and says which
// way it failed. A report with no commentary and no record cannot be told
// from one where the model was never asked.
func TestAnAbsentModelIsRecordedRatherThanImplied(t *testing.T) {
	for _, outcome := range []eventarchive.CommentaryOutcome{
		eventarchive.CommentaryUnavailable,
		eventarchive.CommentaryTimedOut,
	} {
		before := report()
		after := Absent(before, "test-model", outcome)
		if after.Pass == nil || after.Pass.Outcome != outcome {
			t.Fatalf("outcome %q was not recorded", outcome)
		}
		if after.Records != before.Records ||
			!reflect.DeepEqual(after.Rare, before.Rare) {
			t.Fatal("a failed pass changed the report")
		}
		digest, err := after.Digested()
		if err != nil {
			t.Fatalf("digested: %v", err)
		}
		if digest != before.Digest {
			t.Fatalf("recording %q moved the digest", outcome)
		}
	}
}

// An empty array is a good answer. It has to be told from a failure, or a
// quiet week reads as a broken model.
func TestNothingToSayIsNotAFailure(t *testing.T) {
	after, result, err := Attach(report(), "test-model", "[]")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if result.Attached != 0 || result.Rejected != 0 {
		t.Fatalf("attached %d rejected %d, want none of either",
			result.Attached, result.Rejected)
	}
	if after.Pass.Outcome != eventarchive.CommentaryNothingToSay {
		t.Fatalf("outcome %q, want nothing_to_say", after.Pass.Outcome)
	}
}

// The question names findings by number so the answer can be checked against
// what was offered. Asking the model to repeat the finding back would let a
// paraphrase pass for a match.
func TestThePromptNumbersEveryFinding(t *testing.T) {
	built := report()
	prompt := Prompt(built)
	for index := range built.Rare {
		if !strings.Contains(prompt, strconv.Itoa(index+1)+". schema=") {
			t.Fatalf("finding %d is not numbered in the prompt", index+1)
		}
	}
	if !strings.Contains(prompt, "may not add findings") {
		t.Fatal("the prompt does not say what the model may not do")
	}
}

// This package decides what may be done with an answer. Running whatever
// produced it belongs to the caller, and keeping that boundary is what makes
// "the model cannot select or order" a property of the code.
func TestCommentaryRunsNothingAndReachesNothing(t *testing.T) {
	banned := []string{"os/exec", "net", "net/http", "os/user"}
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
				if path == forbidden {
					t.Fatalf("%s imports %s", entry.Name(), path)
				}
			}
		}
	}
}
