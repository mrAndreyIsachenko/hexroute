package connectivityhost

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const testSession = "6f1a2b3c-4d5e-4f60-8a1b-2c3d4e5f6071"

// soak builds a reader with one checkpoint in its lineage and a qualification
// observer whose clocks the test drives.
type soak struct {
	t      *testing.T
	reader *Reader
	chain  string
	wall   time.Time
	// continuous counts through sleep; awake stops for it. Advancing them by
	// different amounts is how a sleep is stated.
	continuous time.Duration
	awake      time.Duration
	boot       string
	tick       int64
}

func newSoak(t *testing.T) *soak {
	t.Helper()
	root := t.TempDir()
	reader, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Something has to be checkpointed before a record has evidence to name.
	if _, _, err := reader.Observe(
		reachedEvidence(), connectivityreduce.PolicyDescriptor{}, 1000); err != nil {
		t.Fatalf("observe: %v", err)
	}
	chain := filepath.Join(root, "qualification")
	if err := reader.AttachQualifier(chain, testSession); err != nil {
		t.Fatalf("attach: %v", err)
	}
	run := &soak{
		t: t, reader: reader, chain: chain,
		wall:       time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		continuous: 4 * time.Hour,
		awake:      3 * time.Hour,
		boot:       "boot-0000000000000000",
	}
	reader.clocks = func() (reading, error) {
		return reading{Wall: run.wall, Continuous: run.continuous, Awake: run.awake}, nil
	}
	// The reader has already seen one cycle on the real clocks, so its
	// comparison starts from whatever those said. Clearing it makes the first
	// driven cycle the first comparison.
	reader.now = reading{}
	// The evaluation tick has to come from the clock the collectors stamp
	// their facts with. A number picked out of the air works only on a
	// machine whose uptime is already past it: on a host that booted a minute
	// ago every fact would arrive with a deadline below the tick judging it,
	// and the whole picture would be stale before the test did anything.
	base, err := continuousTick()
	if err != nil {
		t.Skipf("no continuous clock: %v", err)
	}
	run.tick = int64(base)
	run.cycle()
	return run
}

// cycle is what the daemon does once a round: fold the evidence, then describe
// what the fold lived through. Driving them separately would let a test see a
// snapshot the observer never saw.
func (run *soak) cycle() {
	run.t.Helper()
	// One tick a cycle. The fake sleep moves the clocks the observer reads,
	// not the one freshness is measured on, so a component that goes stale
	// here went stale because the wake said so and not because a deadline
	// quietly expired underneath.
	run.tick++
	if _, _, err := run.reader.Observe(reachedEvidence(),
		connectivityreduce.PolicyDescriptor{}, control.Tick(run.tick)); err != nil {
		run.t.Fatalf("observe: %v", err)
	}
	run.sample()
}

// advance moves all three clocks forward by the same amount: time the host was
// up and being watched.
func (run *soak) advance(span time.Duration) {
	run.wall = run.wall.Add(span)
	run.continuous += span
	run.awake += span
}

// sleep moves the clocks the way a system sleep does: the continuous clock
// counts through it and the awake clock does not.
func (run *soak) sleep(span time.Duration) {
	run.wall = run.wall.Add(span)
	run.continuous += span
}

// observeNothing runs a cycle that reached nothing, so no owner restates
// anything and whatever the wake invalidated stays invalidated.
func (run *soak) observeNothing() {
	run.t.Helper()
	run.tick++
	if _, _, err := run.reader.Observe(
		Evidence{}, connectivityreduce.PolicyDescriptor{},
		control.Tick(run.tick)); err != nil {
		run.t.Fatalf("observe: %v", err)
	}
}

func (run *soak) sample() {
	run.t.Helper()
	if err := run.reader.qualifier.Sample(
		run.boot, run.reader.runtime.Snapshot(),
		run.reader.now, run.reader.slept); err != nil {
		run.t.Fatalf("sample: %v", err)
	}
}

func (run *soak) records() []connectivityqualification.EvidenceRecord {
	run.t.Helper()
	records, err := connectivityqualification.ReadRecords(run.chain)
	if err != nil {
		run.t.Fatalf("read chain: %v", err)
	}
	return records
}

func (run *soak) progress() connectivityqualification.Progress {
	run.t.Helper()
	records := run.records()
	if len(records) == 0 {
		run.t.Fatal("the chain is empty")
	}
	binding := records[0].Binding
	binding.SessionID = metadata.UUID(testSession)
	progress, err := connectivityqualification.Inspect(run.chain, binding)
	if err != nil {
		run.t.Fatalf("inspect: %v", err)
	}
	return progress
}

func kinds(records []connectivityqualification.EvidenceRecord) []connectivityqualification.Kind {
	out := make([]connectivityqualification.Kind, 0, len(records))
	for _, record := range records {
		out = append(out, record.Kind)
	}
	return out
}

// The first sample has nothing to compare against. Claiming a window from it
// would be claiming the span between the daemon starting and some earlier
// moment nobody observed.
func TestTheFirstSampleClaimsNothing(t *testing.T) {
	run := newSoak(t)
	if records := run.records(); len(records) != 0 {
		t.Fatalf("the first sample wrote %v", kinds(records))
	}
}

func TestObservedTimeBecomesAnEligibleWindow(t *testing.T) {
	run := newSoak(t)
	run.advance(5 * time.Minute)
	run.cycle()

	progress := run.progress()
	if progress.EligibleSeconds != 300 {
		t.Fatalf("eligible seconds %d, want 300", progress.EligibleSeconds)
	}
	if progress.Diverged != 0 {
		t.Fatalf("%d divergences on a clean five minutes", progress.Diverged)
	}
}

// A sleep is not a failure to observe: the host was not there to observe. What
// the gate wants to know is that the model came back from it, and it can only
// come back because the cycle that noticed the sleep also restated every
// component in full.
func TestASleepIsSurvivedByRestatingTheWholePicture(t *testing.T) {
	run := newSoak(t)
	run.sleep(2 * time.Hour)
	run.advance(1 * time.Minute)
	run.cycle()

	progress := run.progress()
	if progress.SleepWakeCycles != 1 {
		t.Fatalf("%d sleep/wake cycles, want 1: %v", progress.SleepWakeCycles,
			kinds(run.records()))
	}
	if progress.Diverged != 0 {
		t.Fatalf("a recovered wake was recorded as a divergence: %+v", progress)
	}
	// Two hours asleep is not two hours of eligible time.
	if progress.EligibleSeconds != 60 {
		t.Fatalf("%d eligible seconds, want 60; the sleep was counted as "+
			"observing time", progress.EligibleSeconds)
	}
}

// A cycle that never reached its observations has nothing to restate with, so
// the wake it raised stands. Recording that as a survived sleep would be
// recording a recovery that did not happen.
func TestAWakeTheModelDoesNotRecoverFromIsRecordedAsADivergence(t *testing.T) {
	run := newSoak(t)
	run.sleep(2 * time.Hour)
	run.advance(time.Minute)
	// Nothing was observed, so nothing restates and the wake stands.
	run.observeNothing()
	run.sample()
	for _, record := range run.records() {
		if record.Kind == connectivityqualification.KindSleepWake {
			t.Fatal("the wake was decided before the model had a chance to recover")
		}
	}

	// It stays held until the settle window closes, and is then recorded for
	// what it was.
	run.advance(qualifyWakeSettle + time.Minute)
	run.observeNothing()
	run.sample()

	progress := run.progress()
	if progress.SleepWakeCycles != 1 {
		t.Fatalf("%d sleep/wake cycles, want 1: %v", progress.SleepWakeCycles,
			kinds(run.records()))
	}
	if progress.Diverged == 0 {
		t.Fatal("a wake the model never recovered from counted as a clean one")
	}
	if progress.Complete {
		t.Fatal("the gate completed with an unrecovered wake")
	}
}

// The host was up and nothing was watching it. That time passed, but a gate
// that counted it would reach 72 hours on the strength of the hours it failed.
func TestUnobservedAwakeTimeDoesNotCountTowardTheGate(t *testing.T) {
	run := newSoak(t)
	run.advance(qualifyMaximumGap + time.Minute)
	run.cycle()

	progress := run.progress()
	if progress.EligibleSeconds != 0 {
		t.Fatalf("%d eligible seconds from a stretch nobody observed",
			progress.EligibleSeconds)
	}
	if progress.Diverged != 1 {
		t.Fatalf("%d divergences, want 1", progress.Diverged)
	}
	if progress.Complete {
		t.Fatal("the gate completed on unobserved time")
	}
}

// Eligible time, sleep and the gap between samples are all read off these two
// clocks. If they disagree, every measurement around them is unusable, and
// recording a duration derived from them would be recording a number the
// readings just disproved.
func TestClocksThatCannotBothBeRightAreRecordedAsSuch(t *testing.T) {
	run := newSoak(t)
	// The awake clock cannot lead the one that counts through sleep.
	run.wall = run.wall.Add(time.Minute)
	run.continuous += time.Minute
	run.awake += 10 * time.Minute
	run.cycle()

	records := run.records()
	if len(records) != 1 || records[0].Kind != connectivityqualification.KindClockAnomaly {
		t.Fatalf("chain holds %v, want one clock anomaly", kinds(records))
	}
	if records[0].Result != connectivityqualification.ResultDiverged {
		t.Fatal("a clock anomaly was not recorded as a divergence")
	}
	if progress := run.progress(); progress.Diverged != 1 || progress.EligibleSeconds != 0 {
		t.Fatalf("progress %+v; the anomaly did not block", progress)
	}
}

// Every deadline the prior boot issued was measured against a clock that no
// longer exists, so nothing before the reboot compares with anything after it.
func TestARebootIsRecordedAndTheSpanAcrossItIsNotClaimed(t *testing.T) {
	run := newSoak(t)
	run.advance(3 * time.Minute)
	run.boot = "boot-1111111111111111"
	// A reboot restarts both monotonic clocks, and the wall clock does not.
	run.continuous = time.Minute
	run.awake = time.Minute
	run.cycle()

	records := run.records()
	if len(records) != 1 || records[0].Kind != connectivityqualification.KindReboot {
		t.Fatalf("chain holds %v, want one reboot", kinds(records))
	}
	if progress := run.progress(); progress.Reboots != 1 || progress.EligibleSeconds != 0 {
		t.Fatalf("progress %+v; a span was claimed across the reboot", progress)
	}

	// And the next window is measured from the new boot's clocks, not against
	// readings that belonged to the old one.
	run.advance(2 * time.Minute)
	run.cycle()
	if progress := run.progress(); progress.EligibleSeconds != 120 {
		t.Fatalf("%d eligible seconds after the reboot, want 120",
			progress.EligibleSeconds)
	}
}

// Two runs in one chain add up to a number about neither, and the refusal has
// to come at startup: a soak that ran for hours before anyone discovered it
// was appending to somebody else's evidence is worse than one that never
// started.
func TestAChainFromAnotherSessionIsRefusedAtStartup(t *testing.T) {
	run := newSoak(t)
	run.advance(time.Minute)
	run.cycle()

	if err := run.reader.AttachQualifier(run.chain,
		"11111111-2222-4333-8444-555555555555"); err == nil {
		t.Fatal("a second session attached to an existing chain")
	}
}

// The observer describes the soak. A reader without one has to behave exactly
// as it did before it existed.
func TestNoQualifierIsSilentAndHarmless(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := reader.AttachQualifier("", ""); err != nil {
		t.Fatalf("attach nothing: %v", err)
	}
	if reader.qualifier != nil {
		t.Fatal("a qualifier was built without a chain root")
	}
	if err := reader.qualifier.Sample(
		"boot-0000000000000000", nil, reading{}, 0); err != nil {
		t.Fatalf("sampling without a qualifier: %v", err)
	}
}

// The read model's own claim, without any qualification in the picture: sleep
// is not evidence of health, and a host that slept must not come back with
// components still reading fresh on deadlines set before it went down.
func TestASleptHostDoesNotComeBackLookingFresh(t *testing.T) {
	run := newSoak(t)
	run.sleep(2 * time.Hour)
	run.advance(time.Minute)
	// The cycle observes nothing, so no owner restates anything.
	run.observeNothing()
	// The aggregate would already be unwell from the empty cycle, so it
	// proves nothing here. What the sleep has to change is the components
	// that were fresh a moment ago on deadlines set before the host went
	// down.
	snapshot := run.reader.runtime.Snapshot()
	if snapshot.Summary.Stale == 0 {
		t.Fatalf("nothing went stale across a two-hour sleep: %+v", snapshot.Summary)
	}
}
