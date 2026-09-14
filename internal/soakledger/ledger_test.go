package soakledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/soakcompare"
)

var t0 = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

func window(requested, oldest, newest time.Duration) Coverage {
	return Coverage{Requested: t0.Add(requested), Oldest: t0.Add(oldest), Newest: t0.Add(newest), Records: 10, CollectedAt: t0.Add(newest), LongestSilence: silence(time.Minute)}
}

func TestCollectionsThatOverlapAreContinuous(t *testing.T) {
	windows := []Coverage{
		window(0, time.Minute, 48*time.Hour),
		window(48*time.Hour, 48*time.Hour+time.Minute, 7*24*time.Hour),
	}
	if err := Continuous(windows, t0, t0.Add(7*24*time.Hour)); err != nil {
		t.Fatalf("Continuous: %v", err)
	}
}

// A stretch nobody collected is a hole, not a quiet stretch.
func TestAHoleBetweenCollectionsIsRefused(t *testing.T) {
	windows := []Coverage{
		window(0, time.Minute, 48*time.Hour),
		window(50*time.Hour, 50*time.Hour+time.Minute, 7*24*time.Hour),
	}
	if err := Continuous(windows, t0, t0.Add(7*24*time.Hour)); err == nil {
		t.Fatal("a two-hour hole was accepted")
	}
}

// The archive having already lost the start of a request is a hole too.
//
// It evicts by age at seven days regardless of priority, which is why the soak is
// collected as it goes at all. Tested at the bound rather than far past it: a
// lead of six hours is refused by almost any rule, and a rule that allowed hours
// would have passed here.
func TestAnArchiveThatLostTheStartIsRefused(t *testing.T) {
	week := 7 * 24 * time.Hour
	if err := Continuous([]Coverage{window(0, MaxLead, week)}, t0, t0.Add(week)); err != nil {
		t.Fatalf("a lead of exactly MaxLead was refused: %v", err)
	}
	if err := Continuous([]Coverage{window(0, MaxLead+time.Minute, week)}, t0, t0.Add(week)); err == nil {
		t.Fatal("a lead a minute past MaxLead was accepted")
	}
	if err := Continuous([]Coverage{window(0, 6*time.Hour, week)}, t0, t0.Add(week)); err == nil {
		t.Fatal("a collection whose first six hours were already evicted was accepted")
	}
}

func TestCollectionThatStopsShortOfTheEndIsRefused(t *testing.T) {
	windows := []Coverage{window(0, time.Minute, 6*24*time.Hour)}
	if err := Continuous(windows, t0, t0.Add(7*24*time.Hour)); err == nil {
		t.Fatal("collection a day short of the end was accepted")
	}
}

func TestCollectionThatStartsAfterTheSoakIsRefused(t *testing.T) {
	windows := []Coverage{window(time.Hour, time.Hour+time.Minute, 7*24*time.Hour)}
	if err := Continuous(windows, t0, t0.Add(7*24*time.Hour)); err == nil {
		t.Fatal("collection starting an hour late was accepted")
	}
}

func TestNothingCollectedIsRefused(t *testing.T) {
	if err := Continuous(nil, t0, t0.Add(time.Hour)); err == nil {
		t.Fatal("an empty ledger was accepted")
	}
}

// A decision collected twice, as overlapping collections do, is held once.
func TestOverlappingCollectionsDoNotRepeatDecisions(t *testing.T) {
	ledger, err := Open(filepath.Join(t.TempDir(), "soak"))
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{Sequence: 42, At: t0, Causes: []string{soakcompare.WakeGap}}
	if err := ledger.Collect([]Entry{entry}, window(0, time.Minute, time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Collect([]Entry{entry}, window(time.Hour, time.Hour, 2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	decisions, err := ledger.Decisions()
	if err != nil || len(decisions) != 1 {
		t.Fatalf("Decisions = %v, %v; want one", decisions, err)
	}
	next, ok, err := ledger.Next()
	if err != nil || !ok || !next.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("Next = %v, %v, %v", next, ok, err)
	}
}

func TestOnlyJudgedCausesCanBeNoted(t *testing.T) {
	ledger, _ := Open(filepath.Join(t.TempDir(), "soak"))
	if err := ledger.Note(t0, "link_returned"); err == nil {
		t.Fatal("a cause the soak does not judge was noted")
	}
	if err := ledger.Note(t0, soakcompare.ProcessGone); err != nil {
		t.Fatalf("Note: %v", err)
	}
	inductions, err := ledger.Inductions()
	if err != nil || len(inductions) != 1 || inductions[0].Cause != soakcompare.ProcessGone {
		t.Fatalf("Inductions = %v, %v", inductions, err)
	}
}

// A silence inside a window is a hole, even when the windows meet.
//
// Checking only where collections start and meet would miss a runtime that
// stopped for hours between two collections: nothing it would have decided in
// those hours can disagree with anything.
func TestASilenceInsideAWindowIsRefused(t *testing.T) {
	week := 7 * 24 * time.Hour
	quiet := window(0, time.Minute, week)
	quiet.LongestSilence = silence(3 * time.Hour)
	if err := Continuous([]Coverage{quiet}, t0, t0.Add(week)); err == nil {
		t.Fatal("three hours of silence inside a window were accepted")
	}
	brief := window(0, time.Minute, week)
	brief.LongestSilence = silence(MaxLead)
	if err := Continuous([]Coverage{brief}, t0, t0.Add(week)); err != nil {
		t.Fatalf("a silence of MaxLead was refused: %v", err)
	}
}

func TestTheLongestSilenceIsFoundInAnyOrder(t *testing.T) {
	moments := []time.Time{t0.Add(10 * time.Minute), t0, t0.Add(2 * time.Minute), t0.Add(3 * time.Minute)}
	if got := LongestSilence(moments); got != 7*time.Minute {
		t.Fatalf("LongestSilence = %s, want 7m", got)
	}
	if LongestSilence(nil) != 0 || LongestSilence(moments[:1]) != 0 {
		t.Fatal("fewer than two moments have a silence")
	}
}

func silence(d time.Duration) *time.Duration { return &d }

// A window collected before silences were kept proves nothing about them.
func TestAWindowWithoutItsSilencesIsNotEvidence(t *testing.T) {
	week := 7 * 24 * time.Hour
	legacy := window(0, time.Minute, week)
	legacy.LongestSilence = nil
	if err := Continuous([]Coverage{legacy}, t0, t0.Add(week)); err == nil {
		t.Fatal("a week whose silences nobody measured was judged continuous")
	}
	again := window(0, time.Minute, week)
	if err := Continuous([]Coverage{legacy, again}, t0, t0.Add(week)); err != nil {
		t.Fatalf("collecting again from the start was refused: %v", err)
	}
	late := window(time.Hour, time.Hour+time.Minute, week)
	if err := Continuous([]Coverage{legacy, late}, t0, t0.Add(week)); err == nil {
		t.Fatal("an unmeasured first hour was counted as covered")
	}
}
