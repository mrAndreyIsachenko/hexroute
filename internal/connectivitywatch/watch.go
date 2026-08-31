// Package connectivitywatch reports what changed about the read model since
// anyone last looked.
//
// It exists because watching this host meant asking it the same questions by
// hand, over and over, and noticing the answer had moved. A watcher that
// printed the current state every time would put that noticing back on the
// reader; this one prints transitions and is silent when there are none, for
// the same reason the daemon stopped logging a line per cycle.
//
// Silence is the one thing it must never get wrong. A store it could not read
// is a transition, not a quiet run: the failures worth catching on this host
// all looked like nothing happening.
package connectivitywatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	// StateSchema names the file this watcher keeps between runs.
	StateSchema = "hexroute.connectivity-watch.v1"
	// MaxTransitions bounds one report. A run that produced more than this
	// has stopped describing a change and started describing a rebuild.
	MaxTransitions = 32
)

var ErrUnreadable = errors.New("connectivity watch state is unreadable")

// Facts are what one look at the host establishes.
//
// Every field is something an operator asked about by hand this week. They are
// deliberately flat and comparable: a transition is a field that moved, and
// nothing here needs interpreting to say whether it did.
type Facts struct {
	// Readable is false when the store could not be read at all. It is a fact
	// like any other, and moving into it is a transition worth waking for.
	Readable bool   `json:"readable"`
	Failure  string `json:"failure,omitempty"`

	Resume       string `json:"resume,omitempty"`
	ResumeReason string `json:"resume_reason,omitempty"`
	LineageBroke bool   `json:"lineage_broke,omitempty"`

	Aggregate       string `json:"aggregate,omitempty"`
	Authorization   string `json:"authorization,omitempty"`
	OpenGaps        uint16 `json:"open_gaps"`
	SourceConflicts uint16 `json:"source_conflicts"`
	StaleComponents uint16 `json:"stale_components"`

	// Qualification is absent when no chain was given to watch.
	Qualification *QualificationFacts `json:"qualification,omitempty"`
}

// QualificationFacts are the soak's own answers.
type QualificationFacts struct {
	Diverged       uint32 `json:"diverged"`
	Unbound        uint32 `json:"unbound"`
	GuessedHealthy bool   `json:"guessed_healthy"`
	GatePassing    bool   `json:"gate_passing"`
	Blocking       string `json:"blocking,omitempty"`
	// EligibleSeconds moves on every run by design, so it is reported rather
	// than compared: a watcher that called the clock a transition would never
	// be quiet.
	EligibleSeconds uint64 `json:"eligible_seconds"`
}

// Transition is one thing that moved.
type Transition struct {
	What string `json:"what"`
	From string `json:"from"`
	To   string `json:"to"`
	// Regression marks a move worth acting on rather than merely noting. It is
	// stated per transition rather than derived from severity, because which
	// direction is bad is a property of the field and not of the reader.
	Regression bool `json:"regression"`
}

// State is what the watcher remembers between runs.
type State struct {
	Schema string `json:"schema"`
	Facts  Facts  `json:"facts"`
}

// Compare reports what moved between two looks.
//
// The first look has nothing to compare against and reports nothing, which is
// the honest answer: a watcher that announced everything on its first run
// would teach whoever reads it to ignore the first run.
func Compare(previous, current Facts, first bool) []Transition {
	if first {
		return nil
	}
	moves := make([]Transition, 0, 8)
	add := func(what, from, to string, regression bool) {
		if from == to {
			return
		}
		// An empty side means the field had no value at all — the lineage
		// carried no snapshot to read one from. Printed blank it reads as a
		// truncated line rather than as an answer, so it is named.
		from, to = named(from), named(to)
		moves = append(moves, Transition{
			What: what, From: from, To: to, Regression: regression,
		})
	}

	// Losing the ability to read the store is the transition this watcher
	// exists for. Every wedge on this host looked like nothing happening.
	if previous.Readable != current.Readable {
		add("store", readable(previous), readable(current), !current.Readable)
	}
	if !current.Readable || !previous.Readable {
		return bounded(moves)
	}

	add("resume", previous.Resume, current.Resume, worseResume(previous.Resume, current.Resume))
	add("resume_reason", previous.ResumeReason, current.ResumeReason, false)
	add("lineage_broke", flag(previous.LineageBroke), flag(current.LineageBroke),
		current.LineageBroke && !previous.LineageBroke)
	add("aggregate", previous.Aggregate, current.Aggregate,
		previous.Aggregate == "ready" && current.Aggregate != "ready")
	add("authorization", previous.Authorization, current.Authorization,
		previous.Authorization == "authorized" && current.Authorization != "authorized")
	add("open_gaps", count(previous.OpenGaps), count(current.OpenGaps),
		current.OpenGaps > previous.OpenGaps)
	add("source_conflicts", count(previous.SourceConflicts), count(current.SourceConflicts),
		current.SourceConflicts > previous.SourceConflicts)
	add("stale_components", count(previous.StaleComponents), count(current.StaleComponents),
		current.StaleComponents > previous.StaleComponents)

	if previous.Qualification != nil && current.Qualification != nil {
		before, now := *previous.Qualification, *current.Qualification
		add("diverged", count32(before.Diverged), count32(now.Diverged),
			now.Diverged > before.Diverged)
		add("unbound", count32(before.Unbound), count32(now.Unbound),
			now.Unbound > before.Unbound)
		add("guessed_healthy", flag(before.GuessedHealthy), flag(now.GuessedHealthy),
			now.GuessedHealthy && !before.GuessedHealthy)
		add("gate", gate(before.GatePassing), gate(now.GatePassing),
			before.GatePassing && !now.GatePassing)
		// The blocking phrase moving is progress as often as it is trouble,
		// so it is noted and never called a regression on its own.
		add("blocking", before.Blocking, now.Blocking, false)
	}
	return bounded(moves)
}

// Regressed reports whether any transition is one to act on.
func Regressed(moves []Transition) bool {
	for _, move := range moves {
		if move.Regression {
			return true
		}
	}
	return false
}

// worseResume says whether a lineage verdict moved in the wrong direction.
func worseResume(previous, current string) bool {
	rank := map[string]int{
		"latest": 0, "recovered_ancestor": 1, "genesis": 1, "unrecoverable": 2,
	}
	return rank[current] > rank[previous]
}

func bounded(moves []Transition) []Transition {
	if len(moves) <= MaxTransitions {
		return moves
	}
	sort.SliceStable(moves, func(i, j int) bool {
		return moves[i].Regression && !moves[j].Regression
	})
	return moves[:MaxTransitions]
}

func readable(facts Facts) string {
	if facts.Readable {
		return "readable"
	}
	if facts.Failure != "" {
		return "unreadable: " + facts.Failure
	}
	return "unreadable"
}

// named renders an absent value as absent rather than as nothing.
func named(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func flag(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func gate(passing bool) string {
	if passing {
		return "passing"
	}
	return "refused"
}

func count(value uint16) string   { return fmt.Sprintf("%d", value) }
func count32(value uint32) string { return fmt.Sprintf("%d", value) }

// Load reads what the last run established.
func Load(path string) (Facts, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Facts{}, true, nil
	}
	if err != nil {
		return Facts{}, false, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var state State
	if decoder.Decode(&state) != nil || state.Schema != StateSchema {
		// Refusing is the safe answer. Treating an unreadable memory as a
		// first run would report nothing and call that a quiet host.
		return Facts{}, false, fmt.Errorf("%w: %s", ErrUnreadable, path)
	}
	return state.Facts, false, nil
}

// Save records what this run established, for the next one to compare against.
func Save(path string, facts Facts) error {
	encoded, err := json.Marshal(State{Schema: StateSchema, Facts: facts})
	if err != nil {
		return err
	}
	temporary := path + ".partial"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
