// Package restartguard holds the restart property.
//
// Four of the defects the shadow soak found were the same shape: a component
// that starts clean, runs correctly, and misbehaves only when a second process
// opens what the first one left behind. Not one was reachable from a test that
// starts the component once, and every one of them cost a live restart on the
// operator's own machine to find.
//
// The property is deliberately "restart twice", not "restart". The first
// restart walks empty-to-populated, which is the path the existing tests
// already walk. The second walks populated-to-populated over a write that a
// restarted process made — and that is where the lineage wedge lived: the
// restarted reducer wrote a checkpoint with no parent, and the store then
// refused every write that followed it, forever, while reporting itself
// healthy from outside.
//
// tests/restart_property_test.sh keeps this list honest: a connectivity
// package that writes durable state and is named nowhere here fails that
// census, so the next such component cannot arrive uncovered.
package restartguard

import "testing"

// component is one thing whose state has to outlive the process that wrote it.
type component struct {
	name string
	// open stands for a process start over a root that may already hold state.
	open func(tb testing.TB, root string) session
	// keepsOnlyLatest marks a component that remembers one thing rather than
	// accumulating a log. Its continuity is that the last write survives, not
	// that every write does.
	keepsOnlyLatest bool
}

// session is one process's use of a component.
type session struct {
	// write records one thing and returns what identifies it.
	write func(tb testing.TB) string
	// seen is everything this session can see, oldest first. A session that
	// reopens as if the root were empty returns nothing here.
	seen func(tb testing.TB) []string
	// position is where the session believes it stands. It is nil when the
	// component keeps no position and continuity is only about content.
	position func(tb testing.TB) uint64
}

// components are the connectivity read model's durable pieces.
//
// connectivitycheckpoint is absent on purpose: it already carries its own
// restart tests, which no table-driven version of this would improve on.
// tests/restart_property_test.sh records that exemption and names the tests
// that discharge it, so the claim can be checked rather than believed.
var components = []component{
	{name: "connectivityjournal", open: openJournal},
	{name: "connectivityhost.Recorder", open: openComparisonRecorder},
	{name: "connectivityqualification.Recorder", open: openEvidenceChain},
	{name: "connectivitywatch", open: openWatchMemory, keepsOnlyLatest: true},
}

func TestDurableStateSurvivesTwoRestarts(t *testing.T) {
	for _, subject := range components {
		t.Run(subject.name, func(t *testing.T) {
			assertRestart(t, subject)
		})
	}
}

func assertRestart(t *testing.T, subject component) {
	t.Helper()
	root := t.TempDir()

	first := subject.open(t, root)
	one := first.write(t)

	// Restart 1 — empty to populated.
	second := subject.open(t, root)
	requireSees(t, subject, "after one restart", second, one)
	requireCoherent(t, subject, "after one restart", second)
	afterOne := positionOf(t, second)
	two := second.write(t)
	if two == one {
		t.Fatalf("%s: a restarted process reused the identity %q, "+
			"so it numbered from its own beginning rather than from the root",
			subject.name, one)
	}

	// Restart 2 — populated to populated, over a write a restarted process
	// made. This is the step the lineage wedge failed.
	third := subject.open(t, root)
	requireSees(t, subject, "after two restarts", third, one, two)
	requireCoherent(t, subject, "after two restarts", third)
	if afterTwo := positionOf(t, third); afterTwo < afterOne {
		t.Fatalf("%s: position rewound across the second restart: %d then %d",
			subject.name, afterOne, afterTwo)
	}
	three := third.write(t)
	if three == one || three == two {
		t.Fatalf("%s: the write after the second restart reused the identity %q",
			subject.name, three)
	}

	// A fourth process reads what all three left.
	requireSees(t, subject, "after three restarts", subject.open(t, root), one, two, three)
}

func requireSees(t *testing.T, subject component, when string, current session, want ...string) {
	t.Helper()
	got := current.seen(t)
	if subject.keepsOnlyLatest {
		want = want[len(want)-1:]
	}
	if len(got) != len(want) {
		t.Fatalf("%s %s: sees %v, wrote %v", subject.name, when, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s %s: sees %v, wrote %v", subject.name, when, got, want)
		}
	}
}

// requireCoherent catches a component whose own count contradicts its own
// file: the shape of a reopened handle that reports zero over a root that
// plainly holds records.
func requireCoherent(t *testing.T, subject component, when string, current session) {
	t.Helper()
	if current.position == nil || subject.keepsOnlyLatest {
		return
	}
	held := uint64(len(current.seen(t)))
	if at := current.position(t); at < held {
		t.Fatalf("%s %s: reports position %d over %d records it can see, "+
			"so it reopened as if the root were empty",
			subject.name, when, at, held)
	}
}

func positionOf(t *testing.T, current session) uint64 {
	t.Helper()
	if current.position == nil {
		return 0
	}
	return current.position(t)
}
