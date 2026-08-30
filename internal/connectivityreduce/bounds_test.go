package connectivityreduce

import (
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

func observation(component connectivity.Component, sequence uint64) connectivity.Fact {
	fact := connectivity.FixtureBaseline(component, sequence)
	fact.Baseline = false
	fact.Reason = connectivity.ReasonProbeSucceeded
	fact.FreshnessDeadline = evaluationTick + 10_000
	return fact
}

func watermarkOf(t *testing.T, snapshot *Snapshot, source connectivity.SourceID) SourceWatermark {
	t.Helper()
	for _, watermark := range snapshot.Sources {
		if watermark.Source == source {
			return watermark
		}
	}
	t.Fatalf("no watermark for %q", source)
	return SourceWatermark{}
}

// The snapshot is what a restart rebuilds the acceptor from. If it can hold
// more holes than the acceptor accepts, the host stores a checkpoint it can
// never resume from, and a lossy source breaks its own lineage.
func TestSnapshotHoldsNoMoreHolesThanTheAcceptorAccepts(t *testing.T) {
	h := newHarness(t)
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentRelays, 1)))

	sequence := uint64(1)
	for index := 0; index < connectivityaccept.MaxGapRanges+10; index++ {
		sequence += 2
		h.reduce(h.offer(observation(connectivity.ComponentRelays, sequence)))
	}

	watermark := watermarkOf(t, h.snapshot, source)
	if len(watermark.Gaps) != connectivityaccept.MaxGapRanges {
		t.Fatalf("snapshot holds %d holes, want the acceptor bound of %d",
			len(watermark.Gaps), connectivityaccept.MaxGapRanges)
	}
	if !watermark.GapOverflow || !h.snapshot.Summary.GapOverflow {
		t.Fatal("the acceptor dropped holes and the snapshot did not say so")
	}
	// The whole point of the bound: a restart can rebuild from this.
	state := connectivityaccept.State{
		HostSequence: h.snapshot.ConsumedHostSequence,
		Sources:      make(map[connectivity.SourceID]*connectivityaccept.SourceState),
	}
	for _, entry := range h.snapshot.Sources {
		state.Sources[entry.Source] = &connectivityaccept.SourceState{
			BootID:          entry.BootID,
			LastSequence:    entry.LastSequence,
			Gaps:            entry.Gaps,
			GapOverflow:     entry.GapOverflow,
			PendingBaseline: entry.PendingBaseline,
			Conflicts:       entry.Conflicts,
		}
	}
	if _, err := connectivityaccept.Restore(state); err != nil {
		t.Fatalf("a restart cannot resume from this snapshot: %v", err)
	}
	if err := h.snapshot.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// Retained refusals are evidence, and evidence that grows without a bound
// eventually cannot be stored at all.
func TestConflictEvidenceStaysBoundedAndSaysWhenItWasEvicted(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentRelays, 1)))

	for round := uint64(0); round < uint64(MaxConflictRecords)+12; round++ {
		sequence := 2 + round
		fresh := observation(connectivity.ComponentRelays, sequence)
		h.reduce(h.offer(fresh))
		clash := fresh
		clash.Lifecycle = connectivity.LifecycleDegraded
		clash.Reason = connectivity.ReasonProbeFailed
		h.reduce(h.offer(clash))
	}

	if len(h.snapshot.Conflicts) != MaxConflictRecords {
		t.Fatalf("retained %d conflict records, want the bound of %d",
			len(h.snapshot.Conflicts), MaxConflictRecords)
	}
	if !h.snapshot.Summary.ConflictOverflow {
		t.Fatal("conflict evidence was evicted without saying so")
	}
	// Within a stream the retained evidence is the most recent, because the
	// dropped positions are ones the stream has already moved past.
	lowest := h.snapshot.Conflicts[0].Sequence
	for _, record := range h.snapshot.Conflicts {
		if record.Sequence < lowest {
			lowest = record.Sequence
		}
	}
	if lowest <= 2 {
		t.Fatalf("eviction kept the oldest refusals: lowest retained sequence %d", lowest)
	}
	if err := h.snapshot.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// The bound is per source. A stream that refuses constantly must not be able
// to push another stream's evidence out — least of all across a privilege
// boundary, which one fixed global eviction order would do systematically.
func TestOneNoisySourceCannotEvictAnothersEvidence(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	quiet := observation(connectivity.ComponentUserAccess, 20)
	h.reduce(h.offer(quiet))
	clash := quiet
	clash.Lifecycle = connectivity.LifecycleFailed
	clash.Reason = connectivity.ReasonProbeFailed
	h.reduce(h.offer(clash))

	for round := uint64(0); round < uint64(MaxConflictRecords)+10; round++ {
		sequence := 30 + round
		noisy := observation(connectivity.ComponentRelays, sequence)
		h.reduce(h.offer(noisy))
		noisyClash := noisy
		noisyClash.Lifecycle = connectivity.LifecycleFailed
		noisyClash.Reason = connectivity.ReasonProbeFailed
		h.reduce(h.offer(noisyClash))
	}

	userSource, _ := connectivity.FixtureSource(connectivity.ComponentUserAccess)
	var kept bool
	for _, record := range h.snapshot.Conflicts {
		if record.Source == userSource {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("a noisy root stream evicted the user domain's evidence: %+v",
			h.snapshot.Conflicts)
	}
	if err := h.snapshot.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// A refusal describes a position. Once the owner restates that component in
// full, the position is superseded and the record no longer describes now.
func TestACompleteRestatementRetiresThatComponentsConflicts(t *testing.T) {
	h := newHarness(t)
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	first := observation(connectivity.ComponentRelays, 1)
	h.reduce(h.offer(first))
	clash := first
	clash.Lifecycle = connectivity.LifecycleFailed
	clash.Reason = connectivity.ReasonProbeFailed
	h.reduce(h.offer(clash))

	if len(h.snapshot.Conflicts) != 1 {
		t.Fatalf("conflicts %+v, want one", h.snapshot.Conflicts)
	}
	if watermarkOf(t, h.snapshot, source).Conflicts != 1 {
		t.Fatal("the stream does not count its own refusal")
	}

	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentRelays, 2)))
	if len(h.snapshot.Conflicts) != 0 {
		t.Fatalf("a complete restatement left %+v behind", h.snapshot.Conflicts)
	}
	watermark := watermarkOf(t, h.snapshot, source)
	if watermark.Conflicts != 0 || watermark.AwaitingBaseline() {
		t.Fatalf("the stream did not settle: %+v", watermark)
	}
}

// The snapshot and the acceptor must not be able to hold two opinions about a
// stream, because a restart rebuilds one from the other.
func TestSnapshotIntegrityMatchesTheAcceptorExactly(t *testing.T) {
	h := newHarness(t)
	source, _ := connectivity.FixtureSource(connectivity.ComponentPhysicalNetwork)
	h.reduce(h.offer(
		connectivity.FixtureBaseline(connectivity.ComponentPhysicalNetwork, 1),
		connectivity.FixtureBaseline(connectivity.ComponentDefaultPath, 2),
	))
	h.reduce(h.offer(observation(connectivity.ComponentPhysicalNetwork, 9)))
	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentPhysicalNetwork, 10)))

	watermark := watermarkOf(t, h.snapshot, source)
	state := h.acceptor.State().Sources[source]
	if watermark.LastSequence != state.LastSequence ||
		len(watermark.Gaps) != len(state.Gaps) ||
		watermark.GapOverflow != state.GapOverflow ||
		len(watermark.PendingBaseline) != len(state.PendingBaseline) ||
		watermark.Conflicts != state.Conflicts {
		t.Fatalf("snapshot %+v disagrees with the acceptor %+v", watermark, state)
	}
	// And the shared stream is still open, because default_path never spoke.
	if !watermark.AwaitingBaseline() {
		t.Fatal("a baseline for one component settled a hole covering another")
	}
	if !containsComponent(watermark.PendingBaseline, connectivity.ComponentDefaultPath) {
		t.Fatalf("pending %v does not name the component that owes a restatement",
			watermark.PendingBaseline)
	}
	if h.snapshot.Summary.AwaitingBaseline != 1 {
		t.Fatalf("summary reports %d streams awaiting a baseline, want 1",
			h.snapshot.Summary.AwaitingBaseline)
	}
}

func containsComponent(list []connectivity.Component, value connectivity.Component) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// The bound is only meaningful while the ownership table can still put two
// components behind one stream.
func TestSharedStreamsStillExist(t *testing.T) {
	for _, declaration := range safety.ConnectivitySources() {
		if len(safety.ConnectivitySourceComponents(declaration.Source)) > 1 {
			return
		}
	}
	t.Fatal("no source speaks about more than one component; the coverage rule is void")
}

// A snapshot that exceeds the bounds could not have come from this reducer,
// and accepting one would break the lineage later rather than here.
func TestValidateRejectsSnapshotsBeyondTheBounds(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	tooManyGaps := *h.snapshot
	tooManyGaps.Sources = append([]SourceWatermark(nil), h.snapshot.Sources...)
	gaps := make([]connectivityaccept.GapRange, 0, connectivityaccept.MaxGapRanges+1)
	for index := 0; index <= connectivityaccept.MaxGapRanges; index++ {
		gaps = append(gaps, connectivityaccept.GapRange{
			From: uint64(index*2 + 1), To: uint64(index*2 + 1),
		})
	}
	tooManyGaps.Sources[0].Gaps = gaps
	tooManyGaps.Sources[0].PendingBaseline =
		safety.ConnectivitySourceComponents(tooManyGaps.Sources[0].Source)
	if err := tooManyGaps.Validate(); err == nil {
		t.Fatal("a snapshot beyond the gap bound validated")
	}

	settledButInterrupted := *h.snapshot
	settledButInterrupted.Sources = append([]SourceWatermark(nil), h.snapshot.Sources...)
	settledButInterrupted.Sources[0].Gaps = []connectivityaccept.GapRange{{From: 2, To: 3}}
	settledButInterrupted.Sources[0].PendingBaseline = nil
	if err := settledButInterrupted.Validate(); err == nil {
		t.Fatal("a stream both interrupted and settled validated")
	}

	tooManyConflicts := *h.snapshot
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	tooManyConflicts.Conflicts = make([]ConflictRecord, MaxConflictRecords+1)
	for index := range tooManyConflicts.Conflicts {
		tooManyConflicts.Conflicts[index].Source = source
	}
	if err := tooManyConflicts.Validate(); err == nil {
		t.Fatal("a snapshot beyond the conflict bound validated")
	}
}
