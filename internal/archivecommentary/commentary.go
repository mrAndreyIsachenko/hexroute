// Package archivecommentary attaches a local model's notes to a report that
// was already finished without it.
//
// The whole design is one refusal. The model may add a sentence to a finding
// the deterministic ranking already produced, and may do nothing else: it does
// not select which findings appear, does not order them, does not remove one,
// and cannot add a finding of its own. Every ordered field is settled before
// this package is called and is not writable from here.
//
// The reason is not caution about models in general. It is that a report is
// only worth having if this week's compares with last week's, and a ranking
// that a model could influence is a ranking that changes when the model
// changes — which is a difference in the model reported as a difference in
// the host.
//
// Nothing here executes anything. The caller runs whatever produces the
// answer; this builds the question and decides what may be done with the
// reply.
package archivecommentary

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
)

const (
	// MaxCommentaryBytes bounds one note. A model given room to write an
	// essay writes one, and a report is read by someone in a hurry.
	MaxCommentaryBytes = 400
	// MaxAnswerBytes bounds the whole reply. Past this it is not an answer.
	MaxAnswerBytes = 64 * 1024
)

// ErrUnusable means the answer could not be used at all.
var ErrUnusable = errors.New("model answer is unusable")

// Note is one thing the model wants to say about one finding.
type Note struct {
	// Finding is the index the report gave, one-based, as it was offered.
	Finding int    `json:"finding"`
	Comment string `json:"comment"`
}

// Prompt builds the question.
//
// The findings are numbered and the model is asked to answer by number, so a
// reply can be checked against what was offered. Asking it to repeat the
// finding back would let a paraphrase pass for a match.
func Prompt(report eventarchive.Report) string {
	var builder strings.Builder
	builder.WriteString(
		"You are annotating a report that is already finished.\n" +
			"You may add one short note to any of the numbered findings below.\n" +
			"You may not add findings, reorder them, or say anything about " +
			"findings that are not listed.\n" +
			"Answer with a JSON array of {\"finding\": <number>, " +
			"\"comment\": \"<one sentence>\"} and nothing else.\n" +
			"An empty array is a good answer when you have nothing to add.\n\n")
	fmt.Fprintf(&builder, "Window: %d records, covered %s to %s.\n",
		report.Records, report.Covered.Oldest.Format("2006-01-02T15:04:05Z"),
		report.Covered.Newest.Format("2006-01-02T15:04:05Z"))
	if report.Shortened {
		builder.WriteString(
			"The archive answered about less than was asked for.\n")
	}
	builder.WriteString("\nFindings, rarest first:\n")
	for index, finding := range report.Rare {
		fmt.Fprintf(&builder, "%d. schema=%s component=%s reason=%s count=%d\n",
			index+1, finding.Schema, finding.Component,
			finding.Reason, finding.Count)
	}
	return builder.String()
}

// Result is what a pass managed, whatever it managed.
type Result struct {
	Attached uint16
	Rejected uint16
}

// Attach returns the report with commentary added, and what happened.
//
// The returned report is a copy. Nothing but the Commentary field of an
// existing finding is ever different, which is the property the caller can
// check rather than trust: the digest is recomputed and must not have moved.
func Attach(
	report eventarchive.Report, model string, answer string,
) (eventarchive.Report, Result, error) {
	annotated := report
	annotated.Rare = make([]eventarchive.RareFinding, len(report.Rare))
	copy(annotated.Rare, report.Rare)

	notes, err := parse(answer)
	if err != nil {
		annotated.Pass = &eventarchive.CommentaryPass{
			Model: model, Outcome: eventarchive.CommentaryUnusable,
		}
		return annotated, Result{}, err
	}

	var result Result
	seen := make(map[int]struct{}, len(notes))
	for _, note := range notes {
		comment := strings.TrimSpace(note.Comment)
		// A note about a finding this report does not contain is refused
		// whatever it says. The model may be describing something real; it is
		// not describing this week, and a report that carried it would be
		// asserting something its own evidence does not support.
		if note.Finding < 1 || note.Finding > len(annotated.Rare) ||
			comment == "" {
			result.Rejected++
			continue
		}
		if _, twice := seen[note.Finding]; twice {
			result.Rejected++
			continue
		}
		if len(comment) > MaxCommentaryBytes {
			comment = comment[:MaxCommentaryBytes]
		}
		seen[note.Finding] = struct{}{}
		annotated.Rare[note.Finding-1].Commentary = comment
		result.Attached++
	}

	outcome := eventarchive.CommentaryAttached
	switch {
	case result.Attached == 0 && result.Rejected > 0:
		outcome = eventarchive.CommentaryUnusable
	case result.Attached == 0:
		outcome = eventarchive.CommentaryNothingToSay
	}
	annotated.Pass = &eventarchive.CommentaryPass{
		Model: model, Outcome: outcome,
		Attached: result.Attached, Rejected: result.Rejected,
	}
	return annotated, result, nil
}

// Absent records that no model ran, and why.
//
// A report with no commentary and no record of a pass cannot be told from one
// where the model was never asked, and those are different weeks.
func Absent(
	report eventarchive.Report, model string,
	outcome eventarchive.CommentaryOutcome,
) eventarchive.Report {
	annotated := report
	annotated.Pass = &eventarchive.CommentaryPass{Model: model, Outcome: outcome}
	return annotated
}

// parse reads the answer, tolerating a model that wrapped its JSON in prose
// and refusing one that produced no JSON at all.
func parse(answer string) ([]Note, error) {
	if len(answer) > MaxAnswerBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrUnusable, len(answer))
	}
	start := strings.Index(answer, "[")
	end := strings.LastIndex(answer, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("%w: no array in the answer", ErrUnusable)
	}
	var notes []Note
	if err := json.Unmarshal([]byte(answer[start:end+1]), &notes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	return notes, nil
}
