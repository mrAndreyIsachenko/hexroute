package connectivityruntime

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const tick = control.Tick(1100)

type advancingClock struct{ step int64 }

func (clock *advancingClock) WallNow() time.Time {
	clock.step++
	return time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC).
		Add(time.Duration(clock.step) * time.Millisecond)
}

func (clock *advancingClock) MonotonicNow() time.Duration {
	return time.Duration(clock.step) * time.Millisecond
}

func activePolicy() connectivityreduce.PolicyDescriptor {
	return connectivityreduce.PolicyDescriptor{
		Present: true, Valid: true,
		BundleGeneration: 7, RootGeneration: 3, UserGeneration: 2,
		ManifestDigest: "b8f1c0d2e3a4956677889900aabbccddeeff00112233445566778899aabbccdd",
	}
}

type harness struct {
	runtime *Runtime
	root    string
	t       *testing.T
}

func newHarness(t *testing.T, enabled bool) *harness {
	t.Helper()
	base := t.TempDir()
	return openHarness(t, base, enabled)
}

func openHarness(t *testing.T, base string, enabled bool) *harness {
	t.Helper()
	options := Options{
		Enabled: enabled, BootID: connectivity.FixtureBootID, Random: rand.Reader,
		Preconditions: Preconditions{
			AtomicPolicyStartup: true, DomainMismatch: true,
			Suspension: true, RedactedStatus: true,
		},
	}
	if enabled {
		store, err := connectivitycheckpoint.Open(filepath.Join(base, "readmodel"),
			connectivitycheckpoint.Options{})
		if err != nil {
			t.Fatalf("checkpoints: %v", err)
		}
		rootJournal, err := connectivityjournal.Open(filepath.Join(base, "root"),
			policy.DomainRoot, connectivityjournal.Options{
				NodeID: metadata.UUID("33333333-3333-4333-8333-333333333333"),
				Clock:  &advancingClock{},
			})
		if err != nil {
			t.Fatalf("root journal: %v", err)
		}
		userJournal, err := connectivityjournal.Open(filepath.Join(base, "user"),
			policy.DomainUser, connectivityjournal.Options{
				NodeID: metadata.UUID("44444444-4444-4444-8444-444444444444"),
				Clock:  &advancingClock{},
			})
		if err != nil {
			t.Fatalf("user journal: %v", err)
		}
		options.Checkpoints = store
		options.RootJournal = rootJournal
		options.UserJournal = userJournal
	}
	runtime, err := New(options)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return &harness{runtime: runtime, root: base, t: t}
}

func rootFacts() []connectivity.Fact {
	facts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainRoot {
			facts = append(facts, fact)
		}
	}
	return facts
}

func userFacts() []connectivity.Fact {
	facts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainUser {
			facts = append(facts, fact)
		}
	}
	return facts
}

// The gate is the whole point: with it off, the path that exists today is
// untouched and no store is opened.
func TestDisabledRuntimeDoesNothing(t *testing.T) {
	runtime, err := New(Options{Enabled: false})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if runtime.Enabled() {
		t.Fatal("a disabled runtime reports itself enabled")
	}
	if _, err := runtime.Publish(rootFacts(), policy.DomainRoot); !errors.Is(err, ErrDisabled) {
		t.Fatalf("got %v, want %v", err, ErrDisabled)
	}
	if _, err := runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("got %v, want %v", err, ErrDisabled)
	}
	if runtime.Snapshot() != nil {
		t.Fatal("a disabled runtime produced a snapshot")
	}
}

func TestPublishAndReduce(t *testing.T) {
	h := newHarness(t, true)
	report, err := h.runtime.Publish(rootFacts(), policy.DomainRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if int(report.Accepted) != len(rootFacts()) {
		t.Fatalf("accepted %d, want %d", report.Accepted, len(rootFacts()))
	}
	output, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !output.Changed || output.Snapshot.Generation != 1 {
		t.Fatalf("changed=%v generation=%d", output.Changed, output.Snapshot.Generation)
	}
	if len(output.Snapshot.Components) != len(connectivity.Components()) {
		t.Fatal("the snapshot lost components")
	}
}

// Each domain publishes into its own journal, and neither may use the other's.
func TestDomainsAreSeparate(t *testing.T) {
	h := newHarness(t, true)
	if _, err := h.runtime.Publish(userFacts(), policy.DomainUser); err != nil {
		t.Fatalf("user publish: %v", err)
	}
	report, err := h.runtime.Publish(userFacts(), policy.DomainRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if report.Accepted != 0 || report.Rejected == 0 {
		t.Fatalf("user facts on the root channel were accepted: %+v", report)
	}
}

// A reduction that changes nothing must not add a link to the lineage.
func TestNoOpDoesNotCheckpoint(t *testing.T) {
	h := newHarness(t, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); err != nil {
		t.Fatalf("tick: %v", err)
	}
	store, err := connectivitycheckpoint.Open(filepath.Join(h.root, "readmodel"),
		connectivitycheckpoint.Options{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first, err := store.Index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	output, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if output.Changed {
		t.Fatal("an empty batch counted as a change")
	}
	second, err := store.Index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("lineage grew from %d to %d on a no-op", len(first), len(second))
	}
}

// A restart must land on the same read model, not a fresh one.
func TestRestartResumesTheReadModel(t *testing.T) {
	base := t.TempDir()
	h := openHarness(t, base, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	first, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	before, err := first.Snapshot.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	restarted := openHarness(t, base, true)
	resume := restarted.runtime.Resume()
	if !resume.Usable() {
		t.Fatalf("restart could not resume: %s", resume)
	}
	snapshot := restarted.runtime.Snapshot()
	if snapshot == nil {
		t.Fatal("restart produced no snapshot")
	}
	after, err := snapshot.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if before != after {
		t.Fatal("the restart landed on a different read model")
	}

	// The resumed acceptor must continue the host order, not restart it.
	next := connectivity.FixtureBaseline(connectivity.ComponentDNS, 50)
	report, err := restarted.runtime.Publish([]connectivity.Fact{next}, policy.DomainRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if report.Watermark != first.Snapshot.ConsumedHostSequence+1 {
		t.Fatalf("watermark %d, want %d",
			report.Watermark, first.Snapshot.ConsumedHostSequence+1)
	}
}

// When the user daemon is not there, root records that fact about the link and
// answers nothing on the user's behalf.
func TestAbsentUserLinkLeavesUserComponentsToGoStale(t *testing.T) {
	h := newHarness(t, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("root publish: %v", err)
	}
	if _, err := h.runtime.Publish(userFacts(), policy.DomainUser); err != nil {
		t.Fatalf("user publish: %v", err)
	}
	if _, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); err != nil {
		t.Fatalf("tick: %v", err)
	}

	h.runtime.SetUserLink(UserLinkAbsent)
	if h.runtime.UserLink() != UserLinkAbsent {
		t.Fatal("the link state was not recorded")
	}

	// Time passes and the user domain says nothing. Root does not fill in.
	output, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick + 400})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	for _, record := range output.Snapshot.Components {
		if record.Domain != policy.DomainUser {
			continue
		}
		if record.State != connectivityreduce.StateStale {
			t.Fatalf("%s is %q, want stale", record.Component, record.State)
		}
		// What the user last said is retained; nothing was invented over it.
		if record.Source == "" {
			t.Fatalf("%s lost its owner", record.Component)
		}
	}
}

func TestUnknownDomainIsRefused(t *testing.T) {
	h := newHarness(t, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.Domain("other")); !errors.Is(err, ErrUnknownDomain) {
		t.Fatalf("got %v, want %v", err, ErrUnknownDomain)
	}
}

func TestEnabledRuntimeNeedsItsStores(t *testing.T) {
	options := Options{
		Enabled: true, BootID: connectivity.FixtureBootID,
		Preconditions: Preconditions{
			AtomicPolicyStartup: true, DomainMismatch: true,
			Suspension: true, RedactedStatus: true,
		},
	}
	if _, err := New(options); !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("got %v, want %v", err, ErrMisconfigured)
	}
}

// The gate may not open on a foundation that is not finished, and the refusal
// has to name which part is missing rather than fail generically.
func TestGateRefusesAnUnestablishedFoundation(t *testing.T) {
	complete := Preconditions{
		AtomicPolicyStartup: true, DomainMismatch: true,
		Suspension: true, RedactedStatus: true,
	}
	tests := []struct {
		name   string
		mutate func(*Preconditions)
		want   string
	}{
		{"policy startup", func(p *Preconditions) { p.AtomicPolicyStartup = false },
			"atomic policy startup revalidation"},
		{"domain mismatch", func(p *Preconditions) { p.DomainMismatch = false },
			"domain mismatch handling"},
		{"suspension", func(p *Preconditions) { p.Suspension = false },
			"authorization suspension handling"},
		{"redacted status", func(p *Preconditions) { p.RedactedStatus = false },
			"redacted local status"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preconditions := complete
			test.mutate(&preconditions)
			if missing := preconditions.Missing(); missing != test.want {
				t.Fatalf("missing %q, want %q", missing, test.want)
			}
			_, err := New(Options{
				Enabled: true, BootID: connectivity.FixtureBootID,
				Random: rand.Reader, Preconditions: preconditions,
			})
			if !errors.Is(err, ErrPrecondition) {
				t.Fatalf("got %v, want %v", err, ErrPrecondition)
			}
		})
	}
}

// A disabled runtime asks nothing of the foundation, because it does nothing.
func TestDisabledGateIgnoresPreconditions(t *testing.T) {
	if _, err := New(Options{Enabled: false}); err != nil {
		t.Fatalf("a disabled runtime demanded preconditions: %v", err)
	}
}

// A checkpoint is written only when a reduction was effective, so facts can be
// accepted and journalled and still lie beyond the newest one. Restoring from
// the checkpoint alone left the acceptor about to hand those sequences out
// again, and the journal then held two different facts under one number —
// which made it unreadable in full, permanently, and took the evidence chain
// with it. This is that sequence, reproduced.
func TestRestartDoesNotReissueJournalledSequences(t *testing.T) {
	base := t.TempDir()
	h := openHarness(t, base, true)

	// One effective reduction, so a checkpoint exists.
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// More facts accepted and journalled, with no reduction after them: this
	// is the window a crash or a no-op leaves behind.
	beyond := make([]connectivity.Fact, 0)
	for index, fact := range rootFacts() {
		fact.SourceSequence = uint64(index + 1 + len(rootFacts()))
		fact.Baseline = false
		fact.Reason = connectivity.ReasonProbeSucceeded
		beyond = append(beyond, fact)
	}
	if _, err := h.runtime.Publish(beyond, policy.DomainRoot); err != nil {
		t.Fatalf("publish beyond: %v", err)
	}

	// Restart against the same store and journals.
	restarted := openHarness(t, base, true)
	after := make([]connectivity.Fact, 0)
	for index, fact := range rootFacts() {
		fact.SourceSequence = uint64(index + 1 + 2*len(rootFacts()))
		fact.Baseline = false
		fact.Reason = connectivity.ReasonProbeFailed
		fact.Lifecycle = connectivity.LifecycleDegraded
		after = append(after, fact)
	}
	if _, err := restarted.runtime.Publish(after, policy.DomainRoot); err != nil {
		t.Fatalf("publish after restart: %v", err)
	}

	// The journal must still read. A reused sequence makes it refuse in full.
	journal, err := connectivityjournal.Open(filepath.Join(base, "root"),
		policy.DomainRoot, connectivityjournal.Options{
			NodeID: metadata.UUID("33333333-3333-4333-8333-333333333333"),
			Clock:  &advancingClock{},
		})
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	records, err := journal.Records()
	if err != nil {
		t.Fatalf("the journal became unreadable after a restart: %v", err)
	}
	seen := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		if _, clash := seen[record.HostSequence]; clash {
			t.Fatalf("host sequence %d was issued twice", record.HostSequence)
		}
		seen[record.HostSequence] = struct{}{}
	}
	if len(records) != 3*len(rootFacts()) {
		t.Fatalf("%d records retained, want %d", len(records), 3*len(rootFacts()))
	}
}

// Facts accepted after the newest checkpoint are folded by the next reduction
// rather than dropped: they were accepted, and a restart that forgets them
// publishes a model built on part of its own evidence.
func TestRestartFoldsFactsAcceptedAfterTheCheckpoint(t *testing.T) {
	base := t.TempDir()
	h := openHarness(t, base, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick}); err != nil {
		t.Fatalf("tick: %v", err)
	}
	degraded := make([]connectivity.Fact, 0)
	for index, fact := range rootFacts() {
		fact.SourceSequence = uint64(index + 1 + len(rootFacts()))
		fact.Baseline = false
		fact.Reason = connectivity.ReasonProbeFailed
		fact.Lifecycle = connectivity.LifecycleDegraded
		degraded = append(degraded, fact)
	}
	if _, err := h.runtime.Publish(degraded, policy.DomainRoot); err != nil {
		t.Fatalf("publish degraded: %v", err)
	}

	restarted := openHarness(t, base, true)
	output, err := restarted.runtime.Tick(TickInput{
		Policy: activePolicy(), EvaluationTick: tick + 1,
	})
	if err != nil {
		t.Fatalf("the first reduction after a restart failed: %v", err)
	}
	if output.Snapshot.ConsumedHostSequence != uint64(2*len(rootFacts())) {
		t.Fatalf("consumed %d, want %d",
			output.Snapshot.ConsumedHostSequence, 2*len(rootFacts()))
	}
	// The degraded observations were accepted before the restart; the model
	// must reflect them rather than the checkpoint they never reached.
	for _, record := range output.Snapshot.Components {
		if record.Component == connectivity.ComponentRelays &&
			record.Observed != connectivity.LifecycleDegraded {
			t.Fatalf("relays observed %q, want the state accepted before the restart",
				record.Observed)
		}
	}
}

// A publication report folded stale arrivals into its rejected count, which
// left a caller unable to tell a malformed publisher from a slow one. They are
// different events: a rejected fact never entered the order and is not
// reduced, while a stale one arrived late and is still folded as evidence.
func TestALateArrivalIsCountedAsStaleRatherThanRejected(t *testing.T) {
	h := newHarness(t, true)
	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	high := uint64(0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.SourceID == source && fact.SourceSequence > high {
			high = fact.SourceSequence
		}
	}
	skip := connectivity.FixtureBaseline(connectivity.ComponentRelays, high+2)
	skip.Baseline = false
	skip.Reason = connectivity.ReasonProbeSucceeded
	if _, err := h.runtime.Publish([]connectivity.Fact{skip}, policy.DomainRoot); err != nil {
		t.Fatalf("skip: %v", err)
	}
	// The hole is now open, so the sequence it left behind is behind the
	// watermark rather than a reuse of an accepted one.
	late := connectivity.FixtureBaseline(connectivity.ComponentRelays, high+1)
	late.Baseline = false
	late.Reason = connectivity.ReasonProbeSucceeded
	report, err := h.runtime.Publish([]connectivity.Fact{late}, policy.DomainRoot)
	if err != nil {
		t.Fatalf("late: %v", err)
	}
	if report.Stale != 1 {
		t.Fatalf("report counts %d stale arrivals, want 1: %+v", report.Stale, report)
	}
	if report.Rejected != 0 {
		t.Fatalf("a late arrival was reported as rejected: %+v", report)
	}
}
