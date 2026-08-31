package connectivitywatch

import (
	"os"
	"path/filepath"
	"testing"
)

func healthy() Facts {
	return Facts{
		Readable: true, Resume: "latest", Aggregate: "ready",
		Authorization: "authorized",
		Qualification: &QualificationFacts{GatePassing: false, Blocking: "not enough eligible time"},
	}
}

// A watcher run every few minutes is only worth reading if it is silent when
// nothing moved. The daemon stopped logging a line per cycle for the same
// reason, and a watcher that reprinted the state would put the noticing back
// on whoever reads it.
func TestNothingMovedSaysNothing(t *testing.T) {
	if moves := Compare(healthy(), healthy(), false); len(moves) != 0 {
		t.Fatalf("a quiet run reported %d transitions: %+v", len(moves), moves)
	}
}

// The first look has nothing to compare against. Announcing everything would
// teach whoever reads this to skip the first run, which is where a fresh
// deployment's trouble shows.
func TestTheFirstLookAnnouncesNothing(t *testing.T) {
	if moves := Compare(Facts{}, healthy(), true); len(moves) != 0 {
		t.Fatalf("the first look reported %+v", moves)
	}
}

// This is the case the watcher exists for. Every wedge on this host looked
// like nothing happening: the daemon was gone, the store said nothing, and
// only a person going to look found out.
func TestAStoreThatStoppedBeingReadableIsARegression(t *testing.T) {
	gone := Facts{Readable: false, Failure: "store unavailable"}
	moves := Compare(healthy(), gone, false)
	if len(moves) == 0 {
		t.Fatal("losing the store reported nothing")
	}
	if !Regressed(moves) {
		t.Fatalf("losing the store was not a regression: %+v", moves)
	}
	// And it says which way it went, not merely that something changed.
	if moves[0].To == "" || moves[0].From == moves[0].To {
		t.Fatalf("the transition does not name a direction: %+v", moves[0])
	}
}

// Recovery is a transition too, and must not be reported as trouble — a
// watcher that cried on the way back up would be muted before it mattered.
func TestComingBackIsReportedWithoutBeingARegression(t *testing.T) {
	gone := Facts{Readable: false, Failure: "store unavailable"}
	moves := Compare(gone, healthy(), false)
	if len(moves) == 0 {
		t.Fatal("coming back reported nothing")
	}
	if Regressed(moves) {
		t.Fatalf("coming back was called a regression: %+v", moves)
	}
}

func TestWhatCountsAsGettingWorse(t *testing.T) {
	worse := []struct {
		name   string
		change func(*Facts)
	}{
		{"the lineage stops being provable", func(f *Facts) { f.Resume = "unrecoverable" }},
		{"the aggregate leaves ready", func(f *Facts) { f.Aggregate = "degraded" }},
		{"authorization is withdrawn", func(f *Facts) { f.Authorization = "unauthorized" }},
		{"a hole opens", func(f *Facts) { f.OpenGaps = 1 }},
		{"a source conflicts", func(f *Facts) { f.SourceConflicts = 1 }},
		{"the soak diverges", func(f *Facts) { f.Qualification.Diverged = 1 }},
		{"a result stands on nothing", func(f *Facts) { f.Qualification.Unbound = 1 }},
		{"a fault produced a healthy-looking state",
			func(f *Facts) { f.Qualification.GuessedHealthy = true }},
	}
	for _, testCase := range worse {
		t.Run(testCase.name, func(t *testing.T) {
			current := healthy()
			current.Qualification = &QualificationFacts{}
			previous := healthy()
			previous.Qualification = &QualificationFacts{}
			testCase.change(&current)
			moves := Compare(previous, current, false)
			if !Regressed(moves) {
				t.Fatalf("not reported as getting worse: %+v", moves)
			}
		})
	}
}

// Eligible time moves on every single run. Calling that a transition would
// make the watcher shout once a cycle and be muted the same day.
func TestTheClockMovingIsNotATransition(t *testing.T) {
	previous, current := healthy(), healthy()
	previous.Qualification.EligibleSeconds = 100
	current.Qualification.EligibleSeconds = 700
	if moves := Compare(previous, current, false); len(moves) != 0 {
		t.Fatalf("the clock alone reported %+v", moves)
	}
}

// A memory nobody can read is not a first run. Reading it as one would report
// nothing and call this a quiet host — which is the shape of every failure
// this watcher is for.
func TestAnUnreadableMemoryIsRefusedRatherThanTreatedAsFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(path, []byte(`{"schema":"something else"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, first, err := Load(path); err == nil || first {
		t.Fatalf("an unreadable memory was accepted (first=%v, err=%v)", first, err)
	}
}

func TestAMemoryThatIsNotThereIsAFirstLook(t *testing.T) {
	_, first, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || !first {
		t.Fatalf("an absent memory was not a first look (first=%v, err=%v)", first, err)
	}
}

func TestWhatWasSavedIsWhatComesBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.json")
	if err := Save(path, healthy()); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, first, err := Load(path)
	if err != nil || first {
		t.Fatalf("reload: first=%v err=%v", first, err)
	}
	if moves := Compare(loaded, healthy(), false); len(moves) != 0 {
		t.Fatalf("a saved look did not come back the same: %+v", moves)
	}
}

// A field with no value at all prints blank, and a blank right-hand side reads
// as a truncated line rather than as an answer. When the lineage carries no
// snapshot there is no aggregate to report, and saying so is the report.
func TestAnAbsentValueIsNamedRatherThanBlank(t *testing.T) {
	previous := healthy()
	current := Facts{Readable: true, Resume: "unrecoverable"}
	for _, move := range Compare(previous, current, false) {
		if move.From == "" || move.To == "" {
			t.Fatalf("a transition printed a blank side: %+v", move)
		}
	}
}
