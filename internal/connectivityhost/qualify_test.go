package connectivityhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
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
	storeRoot  string
}

func newSoak(t *testing.T) *soak {
	t.Helper()
	// Every evaluation tick has to come from the clock the collectors stamp
	// their facts with, including the very first one. A number picked out of
	// the air only works on a machine whose uptime is already past it: on a
	// host that booted a minute ago the picture is stale before the test has
	// done anything, and a first reduction above the clock makes every later
	// one look like time running backwards.
	base, err := continuousTick()
	if err != nil {
		t.Skipf("no continuous clock: %v", err)
	}
	root := t.TempDir()
	reader, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	chain := filepath.Join(root, "qualification")
	if err := reader.AttachQualifier(chain, testSession); err != nil {
		t.Fatalf("attach: %v", err)
	}
	run := &soak{
		t: t, reader: reader, chain: chain, storeRoot: root,
		wall:       time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		continuous: 4 * time.Hour,
		awake:      3 * time.Hour,
		boot:       "boot-0000000000000000",
		tick:       int64(base),
	}
	reader.clocks = func() (reading, error) {
		return reading{Wall: run.wall, Continuous: run.continuous, Awake: run.awake}, nil
	}
	// One cycle to start. It checkpoints something for a record to bind to
	// and gives both the reader and the observer a reading to compare the
	// next cycle against, without claiming a span across the moment the
	// daemon came up.
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

// publishUser stands in for the user agent restating what it speaks for. The
// fixture deadlines are long past the tick this harness evaluates at, so these
// components are stale the moment they arrive and stay that way — which is
// exactly how they read on the host.
func (run *soak) publishUser(from uint64) {
	run.t.Helper()
	encoded := make([]json.RawMessage, 0, 2)
	for _, component := range []connectivity.Component{
		connectivity.ComponentUserAccess, connectivity.ComponentSessionExpiry,
	} {
		// One sequence per source, not one per fact: these two components
		// belong to different sources, and each continues its own numbering.
		fact := connectivity.FixtureBaseline(component, from)
		// The deadline is set against the tick this harness evaluates at, not
		// left to the fixture's own. A fixture deadline is a fixed number, so
		// whether it has passed depends on how long the machine has been up —
		// which made this stale on a workstation and fresh on a runner that
		// booted minutes ago.
		fact.MonotonicTick = control.Tick(run.tick - 10)
		fact.FreshnessDeadline = control.Tick(run.tick - 5)
		_, raw, err := policy.CanonicalSHA256(fact)
		if err != nil {
			run.t.Fatal(err)
		}
		encoded = append(encoded, raw)
	}
	if _, err := run.reader.PublishUser(encoded); err != nil {
		run.t.Fatalf("publish user: %v", err)
	}
}

// flip publishes user facts whose lifecycle alternates, so each cycle is an
// effective change and the store actually gains a checkpoint to verify.
func (run *soak) flip(from uint64) {
	run.t.Helper()
	encoded := make([]json.RawMessage, 0, 2)
	for _, component := range []connectivity.Component{
		connectivity.ComponentUserAccess, connectivity.ComponentSessionExpiry,
	} {
		fact := connectivity.FixtureBaseline(component, from)
		fact.MonotonicTick = control.Tick(run.tick)
		fact.FreshnessDeadline = control.Tick(run.tick + 3600)
		if from%2 == 0 {
			fact.Lifecycle = connectivity.LifecycleFailed
			fact.Reason = connectivity.ReasonProbeFailed
		}
		_, raw, err := policy.CanonicalSHA256(fact)
		if err != nil {
			run.t.Fatal(err)
		}
		encoded = append(encoded, raw)
	}
	if _, err := run.reader.PublishUser(encoded); err != nil {
		run.t.Fatalf("publish user: %v", err)
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

// Asking for a soak and getting none, silently, is the failure this refuses.
// The run and the configuration check have to agree about it, or the check
// stops being a check.
func TestASessionWithNoChainIsRefusedRatherThanIgnored(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := reader.AttachQualifier("", testSession); err == nil {
		t.Fatal("a session with nowhere to record was accepted and dropped")
	}
	if err := ValidateQualification("", testSession); err == nil {
		t.Fatal("the check accepts what the run refuses")
	}
}

// A soak outlives the process watching it. The daemon is restarted by an
// install, a crash or a reboot, and the state that says where the last window
// ended is the only thing that keeps the next one from being measured against
// nothing.
func TestTheObserverPicksUpWhereTheLastProcessLeftOff(t *testing.T) {
	run := newSoak(t)
	run.advance(2 * time.Minute)
	run.cycle()
	before := run.progress().EligibleSeconds
	if before != 120 {
		t.Fatalf("%d eligible seconds before the restart, want 120", before)
	}

	// A new process against the same chain, exactly as launchd would.
	restarted, err := OpenQualifier(run.chain, testSession,
		run.reader.store, run.reader.rootJournal, run.reader.userJournal)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if restarted.state.BootID != run.boot {
		t.Fatalf("the restarted observer read boot %q, want %q",
			restarted.state.BootID, run.boot)
	}
	run.reader.qualifier = restarted

	run.advance(3 * time.Minute)
	run.cycle()

	// Three more minutes, measured from where the last process stopped. A
	// restart that seeded afresh would have claimed nothing for them.
	if after := run.progress().EligibleSeconds; after != before+180 {
		t.Fatalf("%d eligible seconds after the restart, want %d",
			after, before+180)
	}
	if diverged := run.progress().Diverged; diverged != 0 {
		t.Fatalf("%d divergences across a clean restart", diverged)
	}
}

// The state beside a chain says where a run got to. If it cannot be read, the
// safe move is to refuse: seeding afresh over an unreadable file would leave
// the next window measured against nothing and say so nowhere.
func TestAnUnreadableObserverStateIsRefused(t *testing.T) {
	run := newSoak(t)
	run.advance(time.Minute)
	run.cycle()

	if err := os.WriteFile(
		filepath.Join(run.chain, qualifyStateFilename),
		[]byte(`{"schema":"something else"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenQualifier(run.chain, testSession,
		run.reader.store, run.reader.rootJournal, run.reader.userJournal); err == nil {
		t.Fatal("an unreadable observer state was opened over")
	}
}

// Measured on the host this ran on: the awake clock advances further than the
// continuous one in about half of all reads, by tens of microseconds, because
// the two are read one after the other. With no tolerance every second cycle
// was filed as two clocks that could not both be right — and one divergence
// anywhere blocks the gate, so a 72-hour soak could never finish.
func TestMicrosecondsBetweenTwoClockReadsAreNotAnAnomaly(t *testing.T) {
	run := newSoak(t)
	for cycle := 0; cycle < 6; cycle++ {
		run.advance(time.Minute)
		// The awake clock lands a hair ahead, exactly as it does in half of
		// the real reads.
		run.awake += 25 * time.Microsecond
		run.cycle()
	}
	progress := run.progress()
	if progress.Diverged != 0 {
		t.Fatalf("%d divergences from reading two clocks in sequence: %v",
			progress.Diverged, kinds(run.records()))
	}
	if progress.EligibleSeconds != 360 {
		t.Fatalf("%d eligible seconds, want 360", progress.EligibleSeconds)
	}
}

// The tolerance must not swallow the thing it exists beside. A clock that
// disagrees by more than a moment is still two clocks that cannot both be
// right, and every measurement around them is read off those readings.
func TestClocksThatDisagreeByMoreThanAMomentStillCount(t *testing.T) {
	run := newSoak(t)
	run.wall = run.wall.Add(time.Minute)
	run.continuous += time.Minute
	run.awake += time.Minute + 2*clockSkewTolerance
	run.cycle()

	records := run.records()
	if len(records) != 1 || records[0].Kind != connectivityqualification.KindClockAnomaly {
		t.Fatalf("chain holds %v, want one clock anomaly", kinds(records))
	}
}

// A wake puts every time-sensitive component back on the hook for a complete
// restatement, and it is answered when nothing is on the hook any more.
// Asking instead whether anything is stale answers a wider question: a
// component can be stale because its own deadline passed, which is not a
// failure to come back from a sleep.
//
// This is the live host. Its user-owned components carry deadlines that have
// long expired, so they read stale whatever happens; when their owner answers
// the wake they stop owing anything, and the sleep has been survived. Under
// the wider question they would have blocked every sleep this host could ever
// record.
func TestAComponentStaleForItsOwnReasonsDoesNotBlockAWake(t *testing.T) {
	run := newSoak(t)
	run.advance(time.Minute)
	run.publishUser(1)
	run.cycle()
	if run.reader.runtime.Snapshot().Summary.Stale == 0 {
		t.Fatal("the user components are not stale, so there is no distinction to show")
	}

	run.sleep(2 * time.Hour)
	run.advance(time.Minute)
	// The owner answers the wake in the same round root does, which is what
	// the user daemon now does for itself.
	run.publishUser(2)
	run.cycle()

	snapshot := run.reader.runtime.Snapshot()
	if snapshot.Summary.Stale == 0 {
		t.Fatal("nothing is stale any more, so this proves nothing")
	}
	for _, component := range snapshot.Components {
		if component.RebaselineRequired {
			t.Fatalf("%s still owes the restatement the wake asked for",
				component.Component)
		}
	}

	progress := run.progress()
	if progress.SleepWakeCycles != 1 {
		t.Fatalf("%d sleep/wake cycles with nothing owing and two components "+
			"stale on their own deadlines: %v",
			progress.SleepWakeCycles, kinds(run.records()))
	}
	if progress.Diverged != 0 {
		t.Fatalf("a wake everything answered was recorded as a divergence: %+v",
			progress)
	}
}
