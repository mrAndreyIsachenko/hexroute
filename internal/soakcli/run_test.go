package soakcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/soakcompare"
	"github.com/mrAndreyIsachenko/hexroute/internal/soakledger"
)

var t0 = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

func archived(t *testing.T, sequence uint64, at time.Time, decision event.TunnelDecision) eventarchive.Record {
	t.Helper()
	encoded, err := event.Encode(event.SchemaTunnelDecision, decision)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return eventarchive.Record{Sequence: sequence, Metadata: metadata.Metadata{WallClock: at}, Event: encoded}
}

func TestOnlyRebuildDecisionsAreCollected(t *testing.T) {
	entries, err := RebuildEntries([]eventarchive.Record{
		archived(t, 1, t0, event.TunnelDecision{Action: "none"}),
		archived(t, 2, t0.Add(time.Minute), event.TunnelDecision{
			Action: "rebuild_tunnel", Causes: []string{"wake_gap"},
			Grounds: &event.TunnelGrounds{ProcessObserved: true, ProcessRunning: true, TickGapMS: tickGap(660000)},
		}),
		archived(t, 3, t0.Add(2*time.Minute), event.TunnelDecision{Action: "reapply_routes", Causes: []string{"routes_drifted"}}),
	})
	if err != nil {
		t.Fatalf("RebuildEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Sequence != 2 || entries[0].Causes[0] != "wake_gap" {
		t.Fatalf("entries = %+v", entries)
	}
	// The gap the decision was made on comes with it: a silence inside that gap
	// is one the runtime accounted for, and the judgement needs to see it.
	if entries[0].TickGap != 11*time.Minute {
		t.Fatalf("the collected decision lost its tick gap: %+v", entries[0])
	}
}

func writeTwilight(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twilight-events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The owning runtime's rebuilds are its transitions for the three judged
// reasons, and nothing else in its log.
func TestTwilightRebuildsAreTheThreeReasons(t *testing.T) {
	path := writeTwilight(t,
		`{"timestamp":"2026-09-15T00:10:00Z","from":"HEALTHY","to":"SINGBOX_EXITED","reason":"process_missing","pid":1}`,
		`{"timestamp":"2026-09-15T00:20:00Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`,
		`{"timestamp":"2026-09-15T00:30:00Z","from":"HEALTHY","to":"STARTING","reason":"carrier_changed","pid":1}`,
		`{"timestamp":"2026-09-15T00:40:00Z","from":"HEALTHY","to":"DEGRADED","reason":"pritunl_public_probe_failed","pid":1}`,
		`{"timestamp":"2026-09-15T00:50:00Z","event":"reserve_probe","target":"x","reason":"","pid":1,"verdict":"ok"}`,
		`{"timestamp":"2026-09-15T00:55:00Z","from":"OUTER_DOWN","to":"STARTING","reason":"outer_path_restored","pid":1}`,
	)
	rebuilds, err := TwilightRebuilds(path)
	if err != nil {
		t.Fatalf("TwilightRebuilds: %v", err)
	}
	if len(rebuilds) != 3 {
		t.Fatalf("rebuilds = %+v, want the three judged reasons only", rebuilds)
	}
	want := []string{soakcompare.ProcessGone, soakcompare.WakeGap, soakcompare.CarrierChanged}
	for index, rebuild := range rebuilds {
		if rebuild.Cause != want[index] {
			t.Fatalf("rebuild %d = %+v, want %s", index, rebuild, want[index])
		}
	}
}

func TestAnEmptyTwilightLogIsNotAJudgementAboutNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TwilightRebuilds(path); err == nil {
		t.Fatal("an empty log was read as a soak with no rebuilds")
	}
}

// A soak with a hole is not judged at all.
func TestJudgeRefusesASoakWithAHole(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Collect(nil, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(48 * time.Hour), Records: 5, CollectedAt: t0.Add(48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-15T00:10:00Z","from":"HEALTHY","to":"DEGRADED","reason":"x","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(7 * 24 * time.Hour).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if code != 1 || !strings.Contains(stdout.String(), "NOT JUDGEABLE") {
		t.Fatalf("judge = %d, %q %q", code, stdout.String(), stderr.String())
	}
}

func TestNoteRefusesAnUnjudgedCause(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{"--ledger", filepath.Join(t.TempDir(), "soak"), "--cause", "payload_failed", "note"}, stdout, stderr, nil)
	if code != 2 {
		t.Fatalf("note of an unjudged cause = %d: %s", code, stderr.String())
	}
}

// A collection with nothing to read yet records nothing.
//
// Recorded, an empty window would make every later judgement refuse the soak for
// a collection that read nothing, when the only thing wrong was running it too
// soon after the last.
func TestCollectingTooSoonRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledgerDir := filepath.Join(dir, "soak")
	now := func() time.Time { return t0.Add(time.Minute) }
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{"--archive", archiveDir, "--ledger", ledgerDir, "--from", t0.Format(time.RFC3339), "collect"},
		stdout, stderr, now)
	if code != 0 {
		t.Fatalf("collect = %d: %s", code, stderr.String())
	}
	ledger, err := soakledger.Open(ledgerDir)
	if err != nil {
		t.Fatal(err)
	}
	if windows, err := ledger.Coverage(); err != nil || len(windows) != 0 {
		t.Fatalf("a collection with nothing to read recorded %v, %v", windows, err)
	}
}

// A silence longer than a few cycles is recorded, because it is a hole.
func TestALongSilenceIsRecordedAsAHole(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledgerDir := filepath.Join(dir, "soak")
	now := func() time.Time { return t0.Add(time.Hour) }
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"--archive", archiveDir, "--ledger", ledgerDir, "--from", t0.Format(time.RFC3339), "collect"},
		stdout, stderr, now); code != 0 {
		t.Fatalf("collect = %d: %s", code, stderr.String())
	}
	ledger, _ := soakledger.Open(ledgerDir)
	windows, err := ledger.Coverage()
	if err != nil || len(windows) != 1 || windows[0].Records != 0 {
		t.Fatalf("an hour of silence was not recorded as a hole: %v, %v", windows, err)
	}
	if err := soakledger.Continuous(windows, nil, t0, t0.Add(time.Hour)); err == nil {
		t.Fatal("an hour of silence was judged continuous")
	}
}

// The longest silence inside a collection is what it records, wherever it falls.
func TestACollectionRecordsItsLongestSilence(t *testing.T) {
	entries := 3
	routes := uint16(2)
	at := func(sequence uint64, offset time.Duration) eventarchive.Record {
		return archived(t, sequence, t0.Add(offset), event.TunnelDecision{Action: "none",
			Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
				CarrierEntries: &entries, RoutesPlanned: &routes}})
	}
	reading := eventarchive.Reading{Records: []eventarchive.Record{
		at(1, time.Minute), at(2, 2*time.Minute), at(3, 4*time.Hour), at(4, 4*time.Hour+time.Minute),
	}}
	reading.Covered = eventarchive.Window{Records: 4, Oldest: t0.Add(time.Minute), Newest: t0.Add(4*time.Hour + time.Minute)}
	coverage, err := CoverageOf(t0, t0.Add(4*time.Hour+2*time.Minute), reading)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.LongestSilence == nil || *coverage.LongestSilence != 4*time.Hour-2*time.Minute {
		t.Fatalf("LongestSilence = %v, want 3h58m", coverage.LongestSilence)
	}
	if err := soakledger.Continuous([]soakledger.Coverage{coverage}, nil, t0, t0.Add(4*time.Hour+2*time.Minute)); err == nil {
		t.Fatal("four hours in which the runtime wrote nothing were judged continuous")
	}
	if len(coverage.Silences) != 1 || !coverage.Silences[0].From.Equal(t0.Add(2*time.Minute)) || !coverage.Silences[0].To.Equal(t0.Add(4*time.Hour)) {
		t.Fatalf("Silences = %v, want one from 2m to 4h", coverage.Silences)
	}
	wake := []soakledger.Cycle{{At: t0.Add(4*time.Hour + 30*time.Second), Wake: true}}
	if err := soakledger.Continuous([]soakledger.Coverage{coverage}, wake, t0, t0.Add(4*time.Hour+2*time.Minute)); err != nil {
		t.Fatalf("four hours ended by a wake were refused: %v", err)
	}
}

// --from wins over where the ledger reached, so a soak can be collected again.
func TestCollectingAgainFromTheStartIsAllowed(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledgerDir := filepath.Join(dir, "soak")
	ledger, err := soakledger.Open(ledgerDir)
	if err != nil {
		t.Fatal(err)
	}
	earlier := soakledger.Coverage{Requested: t0, Oldest: t0, Newest: t0.Add(30 * time.Minute), Records: 5, CollectedAt: t0.Add(30 * time.Minute)}
	if err := ledger.Collect(nil, earlier); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return t0.Add(time.Hour) }
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"--archive", archiveDir, "--ledger", ledgerDir, "--from", t0.Format(time.RFC3339), "collect"},
		stdout, stderr, now); code != 0 {
		t.Fatalf("collect = %d: %s", code, stderr.String())
	}
	windows, err := ledger.Coverage()
	if err != nil || len(windows) != 2 || !windows[1].Requested.Equal(t0) {
		t.Fatalf("collecting again from the start asked from %v, %v", windows, err)
	}
}

// A night's sleep inside the soak is judged, not refused.
//
// The judgement takes the wakes from the decisions it collected. Without them
// every sleep is a hole, and the soak needs three.
func TestJudgeCountsASleepEndedByAWakeAsObserved(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	night := 8 * time.Hour
	asleep, awake := t0.Add(24*time.Hour), t0.Add(24*time.Hour+night)
	wake := []soakledger.Entry{{Sequence: 7, At: awake.Add(time.Minute), Causes: []string{soakcompare.WakeGap}}}
	if err := ledger.Collect(wake, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(week), Records: 5, CollectedAt: t0.Add(week),
		LongestSilence: &night, Silences: []soakledger.Span{{From: asleep, To: awake}},
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-16T08:01:00Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(week).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if strings.Contains(stdout.String(), "NOT JUDGEABLE") || stderr.Len() > 0 {
		t.Fatalf("a sleep ended by a wake made the soak unjudgeable: %q %q", stdout.String(), stderr.String())
	}
}

// Every restart of the owner's tunnel is read, whatever its reason.
func TestTwilightRestartsAreEveryReplacement(t *testing.T) {
	path := writeTwilight(t,
		`{"timestamp":"2026-09-15T00:10:00Z","from":"HEALTHY","to":"SINGBOX_EXITED","reason":"process_missing","pid":1}`,
		`{"timestamp":"2026-09-15T00:10:04Z","from":"SINGBOX_EXITED","to":"STARTING","reason":"singbox_started","pid":1}`,
		`{"timestamp":"2026-09-15T00:20:00Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`,
		`{"timestamp":"2026-09-15T00:40:00Z","from":"HEALTHY","to":"DEGRADED","reason":"pritunl_public_probe_failed","pid":1}`,
		`{"timestamp":"2026-09-15T00:50:00Z","event":"reserve_probe","target":"x","reason":"","pid":1,"verdict":"ok"}`,
		`{"timestamp":"2026-09-15T00:55:00Z","from":"OUTER_DOWN","to":"STARTING","reason":"outer_path_restored","pid":1}`,
	)
	rebuilds, restarts, err := TwilightLog(path)
	if err != nil {
		t.Fatalf("TwilightLog: %v", err)
	}
	if len(rebuilds) != 2 {
		t.Fatalf("rebuilds = %+v, want the process loss and the wake gap", rebuilds)
	}
	if len(restarts) != 4 {
		t.Fatalf("restarts = %v, want the four transitions that replace the process", restarts)
	}
}

// A process-gone decision beside the owner's own restart is explained, not
// reported as a disagreement: the judgement reads the owner's restarts and
// passes them on.
func TestJudgeExplainsAProcessGoneByTheOwnersRestart(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	restart := t0.Add(time.Hour)
	quiet := time.Duration(0)
	decided := []soakledger.Entry{{Sequence: 3, At: restart.Add(20 * time.Second),
		Causes: []string{soakcompare.ProcessGone, soakcompare.CarrierChanged}}}
	if err := ledger.Collect(decided, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(week), Records: 5, CollectedAt: t0.Add(week),
		LongestSilence: &quiet,
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-15T01:00:00Z","from":"HEALTHY","to":"STARTING","reason":"carrier_changed","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(week).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if !strings.Contains(stdout.String(), "explained by the owner's own restart") ||
		strings.Contains(stdout.String(), "decided, not made") {
		t.Fatalf("a process loss beside the owner's restart was judged as: %q %q", stdout.String(), stderr.String())
	}
}

// A wake decided on a gap that covers an earlier silence makes the soak
// judgeable: the runtime accounted for the stretch, late but in one decision.
func TestJudgeCountsASilenceCoveredByALaterWake(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	from, to := t0.Add(time.Hour), t0.Add(time.Hour+46*time.Minute)
	decided := []soakledger.Entry{{Sequence: 9, At: to.Add(42 * time.Minute),
		Causes: []string{soakcompare.WakeGap}, TickGap: 88 * time.Minute}}
	night := to.Sub(from)
	if err := ledger.Collect(decided, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(week), Records: 5, CollectedAt: t0.Add(week),
		LongestSilence: &night, Silences: []soakledger.Span{{From: from, To: to}},
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-15T01:00:00Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(week).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if strings.Contains(stdout.String(), "NOT JUDGEABLE") || stderr.Len() > 0 {
		t.Fatalf("a silence covered by a later wake was judged as: %q %q", stdout.String(), stderr.String())
	}
}

func tickGap(ms int64) *int64 { return &ms }

// A dozing stretch runs from an incomplete decision to the next complete one.
func TestDozingSpansRunFromIncompleteToComplete(t *testing.T) {
	complete := func(at time.Time, sequence uint64) eventarchive.Record {
		entries := 3
		routes := uint16(2)
		return archived(t, sequence, at, event.TunnelDecision{Action: "none",
			Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
				CarrierEntries: &entries, RoutesPlanned: &routes}})
	}
	incomplete := func(at time.Time, sequence uint64) eventarchive.Record {
		return archived(t, sequence, at, event.TunnelDecision{Action: "rebuild_tunnel", Causes: []string{"wake_gap"},
			Grounds: &event.TunnelGrounds{ProcessObserved: true, ProcessRunning: true, TickGapMS: tickGap(600000)}})
	}
	spans, err := DozingSpans([]eventarchive.Record{
		complete(t0, 1),
		incomplete(t0.Add(10*time.Minute), 2),
		incomplete(t0.Add(25*time.Minute), 3),
		incomplete(t0.Add(30*time.Minute), 4),
		complete(t0.Add(40*time.Minute), 5),
		complete(t0.Add(41*time.Minute), 6),
	})
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 1 || !spans[0].From.Equal(t0.Add(10*time.Minute)) || !spans[0].To.Equal(t0.Add(40*time.Minute)) {
		t.Fatalf("spans = %+v, want one from 10m to 40m", spans)
	}
	// A stretch still open at the end of the reading ends at its last incomplete
	// decision: nothing yet says the machine came back.
	spans, err = DozingSpans([]eventarchive.Record{
		complete(t0, 1),
		incomplete(t0.Add(10*time.Minute), 2),
		incomplete(t0.Add(25*time.Minute), 3),
		incomplete(t0.Add(30*time.Minute), 4),
	})
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 1 || !spans[0].To.Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("an open stretch gave %+v", spans)
	}
}

// A sleep the operator took is not a doze: the lid closed on 2026-09-22 left one
// suspended cycle and five minutes, where a night on battery leaves a dozen
// suspended cycles over hours. Excluding the short one excluded the wake gap the
// soak needs, in both runtimes at once.
func TestAShortSuspensionIsASleepAndIsJudged(t *testing.T) {
	entries := 3
	routes := uint16(2)
	suspended := func(at time.Time, sequence uint64, is bool) eventarchive.Record {
		return archived(t, sequence, at, event.TunnelDecision{Action: "none",
			Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
				Suspended: &is, CarrierEntries: &entries, RoutesPlanned: &routes}})
	}
	spans, err := DozingSpans([]eventarchive.Record{
		suspended(t0, 1, false),
		suspended(t0.Add(2*time.Minute), 2, true),
		suspended(t0.Add(8*time.Minute), 3, false),
	})
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("a lid closed for six minutes gave %+v, want no dozing stretch", spans)
	}
	// Several such sleeps over a week are still sleeps: each stretch is counted
	// on its own.
	var week []eventarchive.Record
	for i := uint64(0); i < 3; i++ {
		at := t0.Add(time.Duration(i) * 24 * time.Hour)
		week = append(week, suspended(at, 3*i+1, false), suspended(at.Add(2*time.Minute), 3*i+2, true),
			suspended(at.Add(8*time.Minute), 3*i+3, false))
	}
	spans, err = DozingSpans(week)
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("three sleeps in a week gave %+v, want none", spans)
	}
	// A reading that ends inside such a sleep is a sleep too.
	spans, err = DozingSpans([]eventarchive.Record{suspended(t0, 1, false), suspended(t0.Add(2*time.Minute), 2, true)})
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("a reading ending in a sleep gave %+v, want none", spans)
	}
	// The same stretch with a night's worth of suspended cycles in it is one.
	records := []eventarchive.Record{suspended(t0, 1, false)}
	for i := uint64(1); i <= DozingCycles; i++ {
		records = append(records, suspended(t0.Add(time.Duration(i)*15*time.Minute), i+1, true))
	}
	records = append(records, suspended(t0.Add(time.Duration(DozingCycles+1)*15*time.Minute), uint64(DozingCycles)+2, false))
	spans, err = DozingSpans(records)
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 1 || !spans[0].From.Equal(t0.Add(15*time.Minute)) {
		t.Fatalf("a night gave %+v, want one dozing stretch", spans)
	}
}

// A collection records where the runtime dozed, beside the silences it measured.
func TestACollectionRecordsWhereTheRuntimeDozed(t *testing.T) {
	entries := 3
	routes := uint16(2)
	complete := archived(t, 1, t0, event.TunnelDecision{Action: "none",
		Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
			CarrierEntries: &entries, RoutesPlanned: &routes}})
	dozing := func(sequence uint64, at time.Time) eventarchive.Record {
		return archived(t, sequence, at, event.TunnelDecision{
			Action: "rebuild_tunnel", Causes: []string{"wake_gap"},
			Grounds: &event.TunnelGrounds{ProcessObserved: true, ProcessRunning: true, TickGapMS: tickGap(600000)}})
	}
	back := archived(t, 5, t0.Add(40*time.Minute), event.TunnelDecision{Action: "none",
		Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
			CarrierEntries: &entries, RoutesPlanned: &routes}})
	reading := eventarchive.Reading{Records: []eventarchive.Record{complete,
		dozing(2, t0.Add(10*time.Minute)), dozing(3, t0.Add(20*time.Minute)), dozing(4, t0.Add(30*time.Minute)), back}}
	reading.Covered = eventarchive.Window{Records: 5, Oldest: t0, Newest: t0.Add(40 * time.Minute)}
	coverage, err := CoverageOf(t0, t0.Add(41*time.Minute), reading)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.Dozing) != 1 || !coverage.Dozing[0].From.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("Dozing = %+v, want one stretch from 10m", coverage.Dozing)
	}
}

// A rebuild carries when the owning runtime last wrote anything before it.
func TestTwilightRebuildsCarryTheirPreviousActivity(t *testing.T) {
	path := writeTwilight(t,
		`{"timestamp":"2026-09-15T00:05:00Z","event":"reserve_probe","target":"x","reason":"","pid":1,"verdict":"ok"}`,
		`{"timestamp":"2026-09-15T00:20:00Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`,
	)
	rebuilds, _, err := TwilightLog(path)
	if err != nil {
		t.Fatalf("TwilightLog: %v", err)
	}
	if len(rebuilds) != 1 {
		t.Fatalf("rebuilds = %+v", rebuilds)
	}
	// The line before it is a probe, not a transition: it still says the
	// runtime was there.
	if !rebuilds[0].Previous.Equal(time.Date(2026, 9, 15, 0, 5, 0, 0, time.UTC)) {
		t.Fatalf("previous activity = %s, want 00:05:00Z", rebuilds[0].Previous)
	}
}

// A decision whose gap began while the machine dozed is not judged, so the
// judgement has to carry that moment from the ledger into the comparison.
func TestJudgeCarriesTheCycleADecisionNames(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	dozeFrom, dozeTo := t0.Add(time.Hour), t0.Add(3*time.Hour)
	decided := []soakledger.Entry{{Sequence: 11, At: dozeTo.Add(time.Minute),
		Causes: []string{soakcompare.WakeGap}, TickGap: 90 * time.Minute}}
	quiet := time.Duration(0)
	if err := ledger.Collect(decided, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(week), Records: 5, CollectedAt: t0.Add(week),
		LongestSilence: &quiet, Dozing: []soakledger.Span{{From: dozeFrom, To: dozeTo}},
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-15T00:01:00Z","from":"HEALTHY","to":"DEGRADED","reason":"x","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(week).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if strings.Contains(stdout.String(), "decided, not made") || stderr.Len() > 0 {
		t.Fatalf("a decision whose gap began while dozing was judged: %q %q", stdout.String(), stderr.String())
	}
}

// A cycle that did not finish is not a dozing machine.
//
// A tunnel that went down leaves a cycle unfinished as surely as a doze does:
// on 2026-09-21 an induced process loss left one, and the judgement excluded
// the very event it was there to compare. The machine says which it was.
func TestAnUnfinishedCycleIsNotDozingWhenTheMachineSaysSo(t *testing.T) {
	entries := 3
	routes := uint16(2)
	awake := false
	dozing := true
	complete := func(at time.Time, sequence uint64) eventarchive.Record {
		return archived(t, sequence, at, event.TunnelDecision{Action: "none",
			Grounds: &event.TunnelGrounds{Complete: true, ProcessObserved: true, ProcessRunning: true,
				CarrierEntries: &entries, RoutesPlanned: &routes, Suspended: &awake}})
	}
	// The tunnel went down: the cycle did not finish, and the machine was awake.
	lost := archived(t, 2, t0.Add(10*time.Minute), event.TunnelDecision{
		Action: "rebuild_tunnel", Causes: []string{"process_gone"},
		Grounds: &event.TunnelGrounds{ProcessObserved: true, Suspended: &awake, TickGapMS: tickGap(60000)}})
	spans, err := DozingSpans([]eventarchive.Record{complete(t0, 1), lost, complete(t0.Add(11*time.Minute), 3)})
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("a lost tunnel was read as a dozing machine: %+v", spans)
	}
	// The same shape with the machine dozing is a stretch, once it has dozed
	// often enough to be one.
	night := func(ground *bool) []eventarchive.Record {
		records := []eventarchive.Record{complete(t0, 1)}
		for i := uint64(1); i <= DozingCycles; i++ {
			records = append(records, archived(t, i+1, t0.Add(time.Duration(i)*15*time.Minute),
				event.TunnelDecision{Action: "rebuild_tunnel", Causes: []string{"wake_gap"},
					Grounds: &event.TunnelGrounds{ProcessObserved: true, Suspended: ground, TickGapMS: tickGap(900000)}}))
		}
		return append(records, complete(t0.Add(time.Duration(DozingCycles+1)*15*time.Minute), uint64(DozingCycles)+2))
	}
	spans, err = DozingSpans(night(&dozing))
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("a dozing machine gave %+v", spans)
	}
	// A record written before the machine said either way is read as before.
	spans, err = DozingSpans(night(nil))
	if err != nil {
		t.Fatalf("DozingSpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("an older record gave %+v", spans)
	}
}

// The judgement reads the ledger's latest description of a stretch. A collection
// made under a wider dozing rule described the lid closed on 2026-09-22 as a
// doze; collecting again from the soak's start did not remove that description,
// and the wake both runtimes named stayed excluded.
func TestJudgeTakesTheLaterReadingOfAStretch(t *testing.T) {
	dir := t.TempDir()
	ledger, err := soakledger.Open(filepath.Join(dir, "soak"))
	if err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	quiet := time.Duration(0)
	wake := t0.Add(time.Hour)
	decided := []soakledger.Entry{{Sequence: 3, At: wake, Causes: []string{soakcompare.WakeGap}, TickGap: 6 * time.Minute}}
	stale := soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(2 * time.Hour), Records: 5,
		CollectedAt: t0.Add(2 * time.Hour), LongestSilence: &quiet,
		Dozing: []soakledger.Span{{From: wake.Add(-10 * time.Minute), To: wake.Add(time.Minute)}},
	}
	if err := ledger.Collect(decided, stale); err != nil {
		t.Fatal(err)
	}
	// The same stretch read again, by a rule that does not call it a doze.
	if err := ledger.Collect(nil, soakledger.Coverage{
		Requested: t0, Oldest: t0.Add(time.Minute), Newest: t0.Add(week), Records: 5,
		CollectedAt: t0.Add(week), LongestSilence: &quiet,
	}); err != nil {
		t.Fatal(err)
	}
	twilight := writeTwilight(t, `{"timestamp":"2026-09-15T01:00:30Z","from":"HEALTHY","to":"STARTING","reason":"wake_gap_detected","pid":1}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{"--ledger", filepath.Join(dir, "soak"), "--twilight", twilight,
		"--from", t0.Format(time.RFC3339), "--until", t0.Add(week).Format(time.RFC3339), "judge"},
		stdout, stderr, nil)
	if !strings.Contains(stdout.String(), "wake_gap         agreed 1") {
		t.Fatalf("a stretch read again as awake was judged as: %q %q", stdout.String(), stderr.String())
	}
}
