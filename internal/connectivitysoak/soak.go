// Package connectivitysoak injects fault traces into an isolated read model
// and reports what they made visible.
//
// It exists because a catalogue nobody runs qualifies nothing. Thirteen faults
// were written down with an expectation each, and until something injected
// them the gate rested on the claim that they would have behaved as described.
//
// Everything happens under a scratch root the caller owns. The live store is
// never opened: several of these faults are deliberate corruption of a
// checkpoint lineage, and corrupting the running host's lineage to prove it
// refuses corruption would be the same mistake with better paperwork.
//
// The runner decides nothing about what surviving a fault means. Every claim
// is read from the trace, which states it before the run; the runner reports
// which claim failed and, separately, whether the model came back looking
// untroubled when the trace said it must not.
package connectivitysoak

import (
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
)

var (
	// ErrScratch reports that the isolated store could not be prepared.
	ErrScratch = errors.New("connectivity soak scratch root unavailable")
	// ErrInject reports that the fault could not be injected at all, which is
	// not the same as a fault the model failed to describe.
	ErrInject = errors.New("connectivity fault could not be injected")
)

// Observation is what a trace actually produced.
//
// The acceptance counts cover the injected steps alone. The baseline that had
// to be folded first is setup, not result, and counting it would make every
// number a statement about the fixtures.
type Observation struct {
	Accepted   uint16 `json:"accepted"`
	Duplicates uint16 `json:"duplicates"`
	Conflicts  uint16 `json:"conflicts"`
	Stale      uint16 `json:"stale"`
	Rejected   uint16 `json:"rejected"`

	OpenGaps         uint16 `json:"open_gaps"`
	AwaitingBaseline uint16 `json:"awaiting_baseline"`
	SourceConflicts  uint16 `json:"source_conflicts"`
	StaleComponents  uint16 `json:"stale_components"`

	Aggregate           connectivityreduce.AggregateState      `json:"aggregate"`
	Authorization       connectivityreduce.Authorization       `json:"authorization"`
	AuthorizationReason connectivityreduce.AuthorizationReason `json:"authorization_reason"`

	Resume       connectivitycheckpoint.ResumeStatus `json:"resume,omitempty"`
	ResumeReason connectivitycheckpoint.ResumeReason `json:"resume_reason,omitempty"`
	ResumeDepth  uint16                              `json:"resume_depth,omitempty"`

	CheckpointID    string `json:"checkpoint_id"`
	SnapshotSHA256  string `json:"snapshot_sha256"`
	DiffSHA256      string `json:"diff_sha256"`
	ProposalsSHA256 string `json:"proposals_sha256"`
	BootID          string `json:"boot_id"`
}

// Outcome is one trace injected and judged.
type Outcome struct {
	Fault       connectivitytrace.Fault `json:"fault"`
	TraceSHA256 string                  `json:"trace_sha256"`
	// Visible is the trace's own phrase for what should have shown, kept so a
	// recorded result reads as something rather than as a row of counters.
	Visible     string      `json:"visible"`
	Observation Observation `json:"observation"`

	// Matched is whether every claim the trace made held.
	Matched bool `json:"matched"`
	// Mismatch names the first claim that did not, in a bounded phrase. A
	// failure that cannot be named is a failure nobody can act on.
	Mismatch string `json:"mismatch,omitempty"`
	// GuessedHealthy is the separate and worse result: the trace said this
	// fault must leave a mark, and the model came back untroubled. It is
	// derived independently of Matched, because an assertion that was wrong
	// must not be able to conceal it.
	GuessedHealthy bool `json:"guessed_healthy"`
}

// Run injects one trace under a scratch root and reports what it produced.
//
// The root must not exist or must be empty: a soak that inherited a lineage
// would be describing that lineage as much as the fault.
func Run(trace connectivitytrace.Trace, root string) (Outcome, error) {
	digest, err := trace.Digest()
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{
		Fault: trace.Fault, TraceSHA256: digest,
		Visible: trace.Expectation.Visible,
	}
	session, err := newSession(trace, root)
	if err != nil {
		return Outcome{}, err
	}
	if err := session.play(); err != nil {
		return Outcome{}, err
	}
	observation, err := session.observe()
	if err != nil {
		return Outcome{}, err
	}
	if trace.Layer == connectivitytrace.LayerStore {
		if err := session.damage(trace.Fault); err != nil {
			return Outcome{}, err
		}
		resume, resumeErr := session.reopen(trace.BootID)
		if resumeErr != nil {
			return Outcome{}, resumeErr
		}
		observation.Resume = resume.Status
		observation.ResumeReason = resume.Reason
		observation.ResumeDepth = uint16(resume.Depth)
	}
	outcome.Observation = observation
	outcome.Mismatch = check(trace.Expectation.Assert, observation)
	outcome.Matched = outcome.Mismatch == ""
	outcome.GuessedHealthy = trace.Expectation.Damaging && untroubled(trace, observation)
	return outcome, nil
}

// untroubled reports that nothing in the read model registered the fault.
//
// It is deliberately blunt. A fault that left an open gap, a conflict, a stale
// component, a withdrawn authorization or a refused lineage was seen, whatever
// else the run got wrong. One that left none of those, on a trace that said it
// must leave a mark, was not survived — it was hidden, and every other number
// this chain holds is then a number about a model that cannot see.
func untroubled(trace connectivitytrace.Trace, observation Observation) bool {
	if trace.Layer == connectivitytrace.LayerStore {
		return observation.Resume == connectivitycheckpoint.ResumeLatest
	}
	return observation.OpenGaps == 0 &&
		observation.AwaitingBaseline == 0 &&
		observation.SourceConflicts == 0 &&
		observation.StaleComponents == 0 &&
		observation.Conflicts == 0 &&
		observation.Stale == 0 &&
		observation.Rejected == 0 &&
		observation.Aggregate == connectivityreduce.AggregateReady &&
		observation.Authorization == connectivityreduce.AuthorizationAuthorized
}

// check returns the first claim the observation failed, or an empty string.
func check(assert connectivitytrace.Assertion, seen Observation) string {
	claims := []struct {
		name     string
		expected *uint16
		actual   uint16
	}{
		{"accepted", assert.Accepted, seen.Accepted},
		{"duplicates", assert.Duplicates, seen.Duplicates},
		{"conflicts", assert.Conflicts, seen.Conflicts},
		{"stale arrivals", assert.Stale, seen.Stale},
		{"rejected", assert.Rejected, seen.Rejected},
		{"open gaps", assert.OpenGaps, seen.OpenGaps},
		{"sources awaiting baseline", assert.AwaitingBaseline, seen.AwaitingBaseline},
		{"sources with conflicts", assert.SourceConflicts, seen.SourceConflicts},
		{"stale components", assert.StaleComponents, seen.StaleComponents},
		{"resume depth", assert.ResumeDepth, seen.ResumeDepth},
	}
	for _, claim := range claims {
		if claim.expected != nil && *claim.expected != claim.actual {
			return fmt.Sprintf("%s: expected %d, saw %d",
				claim.name, *claim.expected, claim.actual)
		}
	}
	words := []struct {
		name     string
		expected string
		actual   string
	}{
		{"aggregate", string(assert.Aggregate), string(seen.Aggregate)},
		{"authorization", string(assert.Authorization), string(seen.Authorization)},
		{"authorization reason", string(assert.AuthorizationReason),
			string(seen.AuthorizationReason)},
		{"resume", string(assert.Resume), string(seen.Resume)},
		{"resume reason", string(assert.ResumeReason), string(seen.ResumeReason)},
	}
	for _, claim := range words {
		if claim.expected != "" && claim.expected != claim.actual {
			return fmt.Sprintf("%s: expected %q, saw %q",
				claim.name, claim.expected, claim.actual)
		}
	}
	return ""
}
