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
		archived(t, 2, t0.Add(time.Minute), event.TunnelDecision{Action: "rebuild_tunnel", Causes: []string{"wake_gap"}}),
		archived(t, 3, t0.Add(2*time.Minute), event.TunnelDecision{Action: "reapply_routes", Causes: []string{"routes_drifted"}}),
	})
	if err != nil {
		t.Fatalf("RebuildEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Sequence != 2 || entries[0].Causes[0] != "wake_gap" {
		t.Fatalf("entries = %+v", entries)
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
	if err := soakledger.Continuous(windows, t0, t0.Add(time.Hour)); err == nil {
		t.Fatal("an hour of silence was judged continuous")
	}
}
