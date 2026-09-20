package soakledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if err := Continuous(windows, nil, t0, t0.Add(7*24*time.Hour)); err != nil {
		t.Fatalf("Continuous: %v", err)
	}
}

// A stretch nobody collected is a hole, not a quiet stretch.
func TestAHoleBetweenCollectionsIsRefused(t *testing.T) {
	windows := []Coverage{
		window(0, time.Minute, 48*time.Hour),
		window(50*time.Hour, 50*time.Hour+time.Minute, 7*24*time.Hour),
	}
	if err := Continuous(windows, nil, t0, t0.Add(7*24*time.Hour)); err == nil {
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
	if err := Continuous([]Coverage{window(0, MaxLead, week)}, nil, t0, t0.Add(week)); err != nil {
		t.Fatalf("a lead of exactly MaxLead was refused: %v", err)
	}
	if err := Continuous([]Coverage{window(0, MaxLead+time.Minute, week)}, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("a lead a minute past MaxLead was accepted")
	}
	if err := Continuous([]Coverage{window(0, 6*time.Hour, week)}, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("a collection whose first six hours were already evicted was accepted")
	}
}

func TestCollectionThatStopsShortOfTheEndIsRefused(t *testing.T) {
	windows := []Coverage{window(0, time.Minute, 6*24*time.Hour)}
	if err := Continuous(windows, nil, t0, t0.Add(7*24*time.Hour)); err == nil {
		t.Fatal("collection a day short of the end was accepted")
	}
}

func TestCollectionThatStartsAfterTheSoakIsRefused(t *testing.T) {
	windows := []Coverage{window(time.Hour, time.Hour+time.Minute, 7*24*time.Hour)}
	if err := Continuous(windows, nil, t0, t0.Add(7*24*time.Hour)); err == nil {
		t.Fatal("collection starting an hour late was accepted")
	}
}

func TestNothingCollectedIsRefused(t *testing.T) {
	if err := Continuous(nil, nil, t0, t0.Add(time.Hour)); err == nil {
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
	if err := Continuous([]Coverage{quiet}, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("three hours of silence inside a window were accepted")
	}
	brief := window(0, time.Minute, week)
	brief.LongestSilence = silence(MaxLead)
	if err := Continuous([]Coverage{brief}, nil, t0, t0.Add(week)); err != nil {
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
	if err := Continuous([]Coverage{legacy}, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("a week whose silences nobody measured was judged continuous")
	}
	again := window(0, time.Minute, week)
	if err := Continuous([]Coverage{legacy, again}, nil, t0, t0.Add(week)); err != nil {
		t.Fatalf("collecting again from the start was refused: %v", err)
	}
	late := window(time.Hour, time.Hour+time.Minute, week)
	if err := Continuous([]Coverage{legacy, late}, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("an unmeasured first hour was counted as covered")
	}
}

// A sleep is a silence the runtime ended by deciding a wake.
func TestASilenceEndedByAWakeIsObserved(t *testing.T) {
	week := 7 * 24 * time.Hour
	start, end := t0.Add(24*time.Hour), t0.Add(32*time.Hour)
	asleep := window(0, time.Minute, week)
	asleep.LongestSilence = silence(end.Sub(start))
	asleep.Silences = []Span{{From: start, To: end}}
	windows := []Coverage{asleep}
	if err := Continuous(windows, []Wake{{At: end.Add(time.Minute)}}, t0, t0.Add(week)); err != nil {
		t.Fatalf("a night's sleep ended by a wake was refused: %v", err)
	}
	if err := Continuous(windows, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("eight hours with no record and no wake were accepted")
	}
	if err := Continuous(windows, []Wake{{At: end.Add(MaxLead + time.Second)}}, t0, t0.Add(week)); err == nil {
		t.Fatal("a wake decided more than three cycles after the silence excused it")
	}
	if err := Continuous(windows, []Wake{{At: end.Add(-time.Second)}}, t0, t0.Add(week)); err == nil {
		t.Fatal("a wake decided before the silence ended excused it")
	}
}

// A sleep between two collections is observed the same way.
func TestASleepBeforeACollectionsFirstRecordIsObserved(t *testing.T) {
	week := 7 * 24 * time.Hour
	first := window(0, time.Minute, 24*time.Hour)
	second := window(24*time.Hour, 32*time.Hour, week)
	second.LongestSilence = silence(8 * time.Hour)
	second.Silences = []Span{{From: t0.Add(24 * time.Hour), To: t0.Add(32 * time.Hour)}}
	windows := []Coverage{first, second}
	if err := Continuous(windows, []Wake{{At: t0.Add(32*time.Hour + time.Minute)}}, t0, t0.Add(week)); err != nil {
		t.Fatalf("a sleep before a collection's first record, ended by a wake, was refused: %v", err)
	}
	if err := Continuous(windows, nil, t0, t0.Add(week)); err == nil {
		t.Fatal("eight hours before a collection's first record, with no wake, were accepted")
	}
}

// A long silence that was not located cannot be excused by anything.
func TestAnUnlocatedLongSilenceIsRefused(t *testing.T) {
	week := 7 * 24 * time.Hour
	quiet := window(0, time.Minute, week)
	quiet.LongestSilence = silence(8 * time.Hour)
	var everywhere []Wake
	for at := t0; at.Before(t0.Add(week)); at = at.Add(time.Minute) {
		everywhere = append(everywhere, Wake{At: at, TickGap: time.Minute})
	}
	if err := Continuous([]Coverage{quiet}, everywhere, t0, t0.Add(week)); err == nil {
		t.Fatal("a silence nobody located was excused")
	}
}

func TestSilencesAreLocatedFromTheStart(t *testing.T) {
	moments := []time.Time{t0.Add(6 * time.Hour), t0.Add(time.Minute), t0.Add(6*time.Hour + time.Minute), t0.Add(2 * time.Minute)}
	got := Silences(t0, moments)
	if len(got) != 1 || !got[0].From.Equal(t0.Add(2*time.Minute)) || !got[0].To.Equal(t0.Add(6*time.Hour)) {
		t.Fatalf("Silences = %v, want one from 2m to 6h", got)
	}
	late := Silences(t0, []time.Time{t0.Add(MaxLead + time.Second)})
	if len(late) != 1 || !late[0].From.Equal(t0) {
		t.Fatalf("a first record past MaxLead gave %v, want a silence from the start", late)
	}
	if Silences(t0, []time.Time{t0.Add(MaxLead)}) != nil {
		t.Fatal("a silence of exactly MaxLead was located")
	}
}

// A wake decided later, on a gap that covers the silence, accounts for it.
//
// A machine on battery wakes for seconds and sleeps again, so the cycle that
// finishes and decides can be several sleeps after the silence it names.
// Measured 2026-09-20: a silence of 46 minutes, the wake decided 42 minutes
// after it ended on a gap that spanned both.
func TestAWakeDecidedOnAGapThatCoversTheSilenceAccountsForIt(t *testing.T) {
	week := 7 * 24 * time.Hour
	from, to := t0.Add(time.Hour), t0.Add(time.Hour+46*time.Minute)
	asleep := window(0, time.Minute, week)
	asleep.LongestSilence = silence(to.Sub(from))
	asleep.Silences = []Span{{From: from, To: to}}
	windows := []Coverage{asleep}
	covering := Wake{At: to.Add(42 * time.Minute), TickGap: 88 * time.Minute}
	if err := Continuous(windows, []Wake{covering}, t0, t0.Add(week)); err != nil {
		t.Fatalf("a wake whose gap covers the silence was refused: %v", err)
	}
	short := Wake{At: to.Add(42 * time.Minute), TickGap: 10 * time.Minute}
	if err := Continuous(windows, []Wake{short}, t0, t0.Add(week)); err == nil {
		t.Fatal("a wake whose gap starts after the silence excused it")
	}
	before := Wake{At: from.Add(-time.Minute), TickGap: 4 * time.Hour}
	if err := Continuous(windows, []Wake{before}, t0, t0.Add(week)); err == nil {
		t.Fatal("a wake decided before the silence ended excused it")
	}
}

// A collection made before the gap was kept is repaired by collecting again.
//
// Collections dedupe by sequence, so the decisions a soak collected before its
// command carried the tick gap would have stayed gapless for the rest of the
// soak, and every silence they accounted for would have read as a hole.
func TestCollectingAgainCarriesAGapTheFirstCollectionLacked(t *testing.T) {
	ledger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gapless := Entry{Sequence: 4, At: t0.Add(time.Hour), Causes: []string{"wake_gap"}}
	coverage := window(0, time.Minute, 2*time.Hour)
	if err := ledger.Collect([]Entry{gapless}, coverage); err != nil {
		t.Fatal(err)
	}
	withGap := gapless
	withGap.TickGap = 88 * time.Minute
	if err := ledger.Collect([]Entry{withGap}, coverage); err != nil {
		t.Fatal(err)
	}
	entries, err := ledger.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TickGap != 88*time.Minute {
		t.Fatalf("decisions = %+v, want one carrying the gap", entries)
	}
	// And collecting the same thing again writes nothing: a ledger that grew on
	// every collection would be one decision written a hundred times.
	before := ledgerLines(t, ledger)
	if err := ledger.Collect([]Entry{withGap}, coverage); err != nil {
		t.Fatal(err)
	}
	if after := ledgerLines(t, ledger); after != before {
		t.Fatalf("collecting the same decision again wrote %d lines, was %d", after, before)
	}
}

func ledgerLines(t *testing.T, ledger *Ledger) int {
	t.Helper()
	raw, err := os.ReadFile(ledger.path("decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

// A decision written again without its gap does not erase the gap.
//
// An older command collecting after a newer one writes the same decision with
// no gap, and the judgement would stop seeing the silences it accounted for.
func TestALaterLineWithoutTheGapDoesNotEraseIt(t *testing.T) {
	ledger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rich := Entry{Sequence: 4, At: t0.Add(time.Hour), Causes: []string{"wake_gap"}, TickGap: 88 * time.Minute}
	if err := ledger.Collect([]Entry{rich}, window(0, time.Minute, 2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	gapless, err := json.Marshal(Entry{Sequence: 4, At: rich.At, Causes: rich.Causes})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(ledger.path("decisions.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(gapless, '\n')); err != nil {
		t.Fatal(err)
	}
	file.Close()
	entries, err := ledger.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TickGap != 88*time.Minute {
		t.Fatalf("decisions = %+v, want the gap kept", entries)
	}
}
