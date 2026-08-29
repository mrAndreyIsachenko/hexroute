package connectivityreduce

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

const evaluationTick = control.Tick(1100)

func activePolicy() PolicyDescriptor {
	return PolicyDescriptor{
		Present: true, Valid: true, Suspended: false,
		BundleGeneration: 7, RootGeneration: 3, UserGeneration: 2,
		ManifestDigest: "b8f1c0d2e3a4956677889900aabbccddeeff00112233445566778899aabbccdd",
	}
}

// harness runs facts through a real acceptor so the reducer sees exactly the
// decisions the acceptance layer would hand it.
type harness struct {
	acceptor *connectivityaccept.Acceptor
	snapshot *Snapshot
	t        *testing.T
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{acceptor: connectivityaccept.New(), t: t}
}

func (h *harness) offer(facts ...connectivity.Fact) []Event {
	h.t.Helper()
	events := make([]Event, 0, len(facts))
	for _, fact := range facts {
		acceptance, err := h.acceptor.Accept(fact, fact.Domain)
		if err != nil {
			h.t.Fatalf("accept %s: %v", fact.Component, err)
		}
		events = append(events, Event{Acceptance: acceptance, Fact: fact})
	}
	return events
}

func (h *harness) reduceAt(tick control.Tick, descriptor PolicyDescriptor, events []Event) Output {
	h.t.Helper()
	output, err := Reduce(Input{
		Prior: h.snapshot, Events: events, Policy: descriptor,
		BootID: connectivity.FixtureBootID, EvaluationTick: tick,
	})
	if err != nil {
		h.t.Fatalf("reduce: %v", err)
	}
	h.snapshot = &output.Snapshot
	return output
}

func (h *harness) reduce(events []Event) Output {
	h.t.Helper()
	return h.reduceAt(evaluationTick, activePolicy(), events)
}

func baselineFacts() []connectivity.Fact {
	return connectivity.FixtureBaselineSet()
}

func TestSnapshotAlwaysCarriesEveryComponent(t *testing.T) {
	h := newHarness(t)
	// Only two components ever report.
	output := h.reduce(h.offer(
		connectivity.FixtureBaseline(connectivity.ComponentDNS, 1),
		connectivity.FixtureBaseline(connectivity.ComponentRelays, 1),
	))
	if len(output.Snapshot.Components) != len(connectivity.Components()) {
		t.Fatalf("got %d records, want %d",
			len(output.Snapshot.Components), len(connectivity.Components()))
	}
	silent := 0
	for _, record := range output.Snapshot.Components {
		if record.State == StateUnknown {
			silent++
		}
	}
	if silent != len(connectivity.Components())-2 {
		t.Fatalf("%d components are unknown, want %d",
			silent, len(connectivity.Components())-2)
	}
}

// The summary is a projection, never a replacement: a broken resolver beside a
// working tunnel must stay visible as both.
func TestSummaryDoesNotFlattenComponents(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleFailed
			facts[index].Reason = connectivity.ReasonProbeFailed
		}
	}
	output := h.reduce(h.offer(facts...))

	states := make(map[connectivity.Component]ComponentState)
	for _, record := range output.Snapshot.Components {
		states[record.Component] = record.State
	}
	if states[connectivity.ComponentDNS] != StateFailed {
		t.Fatalf("dns is %q, want failed", states[connectivity.ComponentDNS])
	}
	if states[connectivity.ComponentTransports] != StateReady {
		t.Fatalf("transports is %q, want ready", states[connectivity.ComponentTransports])
	}
	if output.Snapshot.Summary.State != AggregateFailed {
		t.Fatalf("summary is %q, want failed", output.Snapshot.Summary.State)
	}
	if output.Snapshot.Summary.Ready == 0 {
		t.Fatal("the summary lost the components that are fine")
	}
}

func TestComponentWithoutAFreshFactBecomesStale(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	// Every fixture deadline is its tick plus 300; step past all of them.
	output := h.reduceAt(evaluationTick+400, activePolicy(), nil)
	for _, record := range output.Snapshot.Components {
		if record.State != StateStale {
			t.Fatalf("%s is %q, want stale", record.Component, record.State)
		}
		if record.Observed != connectivity.LifecycleReady {
			t.Fatalf("%s lost what it last observed", record.Component)
		}
	}
	if output.Snapshot.Summary.State != AggregateDegraded {
		t.Fatalf("summary is %q, want degraded", output.Snapshot.Summary.State)
	}
}

func TestSemanticNoOpDoesNotAdvanceGeneration(t *testing.T) {
	h := newHarness(t)
	first := h.reduce(h.offer(baselineFacts()...))
	if !first.Changed || first.Snapshot.Generation != 1 {
		t.Fatalf("first reduction: changed=%v generation=%d", first.Changed, first.Snapshot.Generation)
	}

	// The same states restated under new sequences: new bookkeeping, no new
	// meaning.
	repeat := make([]connectivity.Fact, 0)
	for index, component := range connectivity.Components() {
		fact := connectivity.FixtureBaseline(component, uint64(index+1+len(connectivity.Components())))
		repeat = append(repeat, fact)
	}
	second := h.reduce(h.offer(repeat...))
	if second.Changed {
		t.Fatal("re-observing the same state counted as a change")
	}
	if second.Snapshot.Generation != first.Snapshot.Generation {
		t.Fatalf("generation moved from %d to %d on a no-op",
			first.Snapshot.Generation, second.Snapshot.Generation)
	}
	if second.Snapshot.ConsumedHostSequence <= first.Snapshot.ConsumedHostSequence {
		t.Fatal("the no-op did not consume its facts")
	}
}

func TestEffectiveChangeAdvancesGenerationExactlyOnce(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	// One batch carrying several distinct changes must still advance once.
	changes := []connectivity.Fact{}
	for _, component := range []connectivity.Component{
		connectivity.ComponentDNS,
		connectivity.ComponentRelays,
		connectivity.ComponentTransports,
	} {
		fact := connectivity.FixtureBaseline(component, 20)
		fact.Lifecycle = connectivity.LifecycleDegraded
		fact.Reason = connectivity.ReasonProbeFailed
		changes = append(changes, fact)
	}
	output := h.reduce(h.offer(changes...))
	if !output.Changed {
		t.Fatal("three changed components did not count as a change")
	}
	if output.Snapshot.Generation != 2 {
		t.Fatalf("generation %d, want 2", output.Snapshot.Generation)
	}
}

// Independent sources may interleave differently between runs. The host order
// differs, but what the snapshot means must not.
func TestIndependentSourcePermutationsAgree(t *testing.T) {
	forward := newHarness(t)
	facts := baselineFacts()
	forward.reduce(forward.offer(facts...))

	reversed := newHarness(t)
	reversed.reduce(reversed.offer(interleaveBySource(facts)...))

	first, err := SemanticDigest(*forward.snapshot)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	second, err := SemanticDigest(*reversed.snapshot)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if first != second {
		t.Fatal("the same observations in a different interleaving meant something different")
	}
}

// interleaveBySource reorders arrivals across sources while preserving each
// source's own order. A source's internal order is not a permutation to be
// tolerated: it is the sequence the whole acceptance layer is built on.
func interleaveBySource(facts []connectivity.Fact) []connectivity.Fact {
	order := make([]connectivity.SourceID, 0)
	buckets := make(map[connectivity.SourceID][]connectivity.Fact)
	for _, fact := range facts {
		if _, seen := buckets[fact.SourceID]; !seen {
			order = append(order, fact.SourceID)
		}
		buckets[fact.SourceID] = append(buckets[fact.SourceID], fact)
	}
	out := make([]connectivity.Fact, 0, len(facts))
	for round := 0; len(out) < len(facts); round++ {
		for index := len(order) - 1; index >= 0; index-- {
			bucket := buckets[order[index]]
			if round < len(bucket) {
				out = append(out, bucket[round])
			}
		}
	}
	return out
}

// Reordering one source's own stream is not equivalent: the later sequence
// wins and the earlier one lands behind the watermark.
func TestReorderingOneSourceIsNotEquivalent(t *testing.T) {
	facts := baselineFacts()
	var first, second int = -1, -1
	for index, fact := range facts {
		if fact.SourceID != "root.network" {
			continue
		}
		if first < 0 {
			first = index
			continue
		}
		second = index
		break
	}
	if first < 0 || second < 0 {
		t.Skip("no source owns two components in this build")
	}

	forward := newHarness(t)
	forward.reduce(forward.offer(facts...))

	swapped := append([]connectivity.Fact(nil), facts...)
	swapped[first], swapped[second] = swapped[second], swapped[first]
	backward := newHarness(t)
	backward.reduce(backward.offer(swapped...))

	before, _ := SemanticDigest(*forward.snapshot)
	after, _ := SemanticDigest(*backward.snapshot)
	if before == after {
		t.Fatal("reordering a source's own stream was treated as equivalent")
	}
}

func TestBatchingDoesNotChangeMeaning(t *testing.T) {
	whole := newHarness(t)
	whole.reduce(whole.offer(baselineFacts()...))

	piecewise := newHarness(t)
	for _, fact := range baselineFacts() {
		piecewise.reduce(piecewise.offer(fact))
	}

	first, _ := SemanticDigest(*whole.snapshot)
	second, _ := SemanticDigest(*piecewise.snapshot)
	if first != second {
		t.Fatal("one batch and many batches disagreed")
	}
}

func TestIdenticalInputsProduceIdenticalCanonicalOutput(t *testing.T) {
	digests := make([]string, 2)
	for run := range digests {
		h := newHarness(t)
		output := h.reduce(h.offer(baselineFacts()...))
		digest, err := output.Snapshot.Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		digests[run] = digest
	}
	if digests[0] != digests[1] {
		t.Fatal("reduction is not deterministic")
	}
}

// The gap the earlier sections could only return now has to be state.
func TestSequenceGapIsVisibleInTheSnapshot(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)))

	skipped := connectivity.FixtureBaseline(connectivity.ComponentDNS, 5)
	skipped.Baseline = false
	skipped.Reason = connectivity.ReasonProbeSucceeded
	output := h.reduce(h.offer(skipped))

	source, _ := connectivity.FixtureSource(connectivity.ComponentDNS)
	var found bool
	for _, watermark := range output.Snapshot.Sources {
		if watermark.Source != source {
			continue
		}
		found = true
		if len(watermark.Gaps) != 1 || watermark.Gaps[0].From != 2 || watermark.Gaps[0].To != 4 {
			t.Fatalf("gaps %+v, want one 2..4", watermark.Gaps)
		}
		if !watermark.AwaitingBaseline {
			t.Fatal("the source is not marked as owing a baseline")
		}
	}
	if !found {
		t.Fatal("the source has no watermark in the snapshot")
	}
	if output.Snapshot.Summary.OpenGaps != 1 {
		t.Fatalf("summary reports %d open gaps, want 1", output.Snapshot.Summary.OpenGaps)
	}
}

func TestConflictIsRecordedAndKeepsTheAcceptedFact(t *testing.T) {
	h := newHarness(t)
	first := connectivity.FixtureBaseline(connectivity.ComponentRelays, 1)
	h.reduce(h.offer(first))

	altered := first
	altered.Lifecycle = connectivity.LifecycleFailed
	altered.Reason = connectivity.ReasonProbeFailed
	output := h.reduce(h.offer(altered))

	if len(output.Snapshot.Conflicts) != 1 {
		t.Fatalf("conflicts %+v, want one", output.Snapshot.Conflicts)
	}
	for _, record := range output.Snapshot.Components {
		if record.Component != connectivity.ComponentRelays {
			continue
		}
		if record.State != StateConflict {
			t.Fatalf("state %q, want conflict", record.State)
		}
		// The accepted fact was not replaced by the reuse.
		if record.Observed != connectivity.LifecycleReady {
			t.Fatalf("observed %q, want the accepted ready", record.Observed)
		}
	}
	if output.Snapshot.Summary.SourceConflicts != 1 {
		t.Fatalf("summary reports %d source conflicts, want 1",
			output.Snapshot.Summary.SourceConflicts)
	}
}

func TestBaselineClearsConflictAndGap(t *testing.T) {
	h := newHarness(t)
	first := connectivity.FixtureBaseline(connectivity.ComponentRelays, 1)
	h.reduce(h.offer(first))
	altered := first
	altered.Lifecycle = connectivity.LifecycleFailed
	altered.Reason = connectivity.ReasonProbeFailed
	h.reduce(h.offer(altered))

	recovered := connectivity.FixtureBaseline(connectivity.ComponentRelays, 2)
	output := h.reduce(h.offer(recovered))
	for _, record := range output.Snapshot.Components {
		if record.Component == connectivity.ComponentRelays && record.State != StateReady {
			t.Fatalf("state %q after a baseline, want ready", record.State)
		}
	}
	if output.Snapshot.Summary.SourceConflicts != 0 {
		t.Fatal("a baseline did not settle the conflict")
	}
}

// Corroboration is evidence. It is retained, it may disagree, and it may not
// become the component's state.
func TestCorroborationIsEvidenceNotState(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)))

	probe := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	probe.SourceID = "root.probe"
	probe.Lifecycle = connectivity.LifecycleFailed
	probe.Reason = connectivity.ReasonProbeFailed
	output := h.reduce(h.offer(probe))

	for _, record := range output.Snapshot.Components {
		if record.Component != connectivity.ComponentDNS {
			continue
		}
		if record.State != StateReady {
			t.Fatalf("state %q, want the owner's ready", record.State)
		}
		if len(record.Corroborations) != 1 {
			t.Fatalf("corroborations %+v, want one", record.Corroborations)
		}
		if record.Corroborations[0].Agrees {
			t.Fatal("a disagreeing probe was recorded as agreeing")
		}
		if record.Corroborations[0].Lifecycle != connectivity.LifecycleFailed {
			t.Fatal("the probe's opinion was not retained")
		}
	}
}

func TestUnauthorizedPolicyPreservesObservations(t *testing.T) {
	tests := []struct {
		name       string
		descriptor PolicyDescriptor
		want       AuthorizationReason
	}{
		{"absent", PolicyDescriptor{}, AuthorizationReasonAbsent},
		{"invalid", PolicyDescriptor{Present: true}, AuthorizationReasonInvalid},
		{"suspended", func() PolicyDescriptor {
			descriptor := activePolicy()
			descriptor.Suspended = true
			return descriptor
		}(), AuthorizationReasonSuspended},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			output := h.reduceAt(evaluationTick, test.descriptor, h.offer(baselineFacts()...))
			if output.Snapshot.Authorization != AuthorizationUnauthorized {
				t.Fatalf("authorization %q, want unauthorized", output.Snapshot.Authorization)
			}
			if output.Snapshot.Reason != test.want {
				t.Fatalf("reason %q, want %q", output.Snapshot.Reason, test.want)
			}
			// Observations survive the loss of authority.
			for _, record := range output.Snapshot.Components {
				if record.State != StateReady {
					t.Fatalf("%s is %q; observations were withheld",
						record.Component, record.State)
				}
			}
		})
	}
}

func TestPolicyMovingBackwardsIsUnauthorized(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	older := activePolicy()
	older.BundleGeneration--
	output := h.reduceAt(evaluationTick, older, nil)
	if output.Snapshot.Reason != AuthorizationReasonGenerationGap {
		t.Fatalf("reason %q, want policy_generation_mismatch", output.Snapshot.Reason)
	}
}

// A policy change is itself a semantic change: the prior reduction was bound
// to generations that no longer apply.
func TestPolicyGenerationChangeAdvancesGeneration(t *testing.T) {
	h := newHarness(t)
	first := h.reduce(h.offer(baselineFacts()...))

	next := activePolicy()
	next.BundleGeneration++
	output := h.reduceAt(evaluationTick, next, nil)
	if !output.Changed {
		t.Fatal("a new policy generation was treated as a no-op")
	}
	if output.Snapshot.Generation != first.Snapshot.Generation+1 {
		t.Fatalf("generation %d, want %d",
			output.Snapshot.Generation, first.Snapshot.Generation+1)
	}
}

func TestPartialComponentFailureLeavesTheRestIntact(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	failed := connectivity.FixtureBaseline(connectivity.ComponentUserAccess, 20)
	failed.Lifecycle = connectivity.LifecycleFailed
	failed.Reason = connectivity.ReasonProbeFailed
	failed.Payload = connectivity.Payload{UserAccess: &connectivity.UserAccessPayload{
		ProfileClass: connectivity.ProfileConfigured,
	}}
	output := h.reduce(h.offer(failed))

	for _, record := range output.Snapshot.Components {
		switch record.Component {
		case connectivity.ComponentUserAccess:
			if record.State != StateFailed {
				t.Fatalf("user access is %q, want failed", record.State)
			}
		default:
			if record.State != StateReady {
				t.Fatalf("%s became %q when an unrelated component failed",
					record.Component, record.State)
			}
		}
	}
}

func TestOutOfOrderAcceptedFactsAreRefused(t *testing.T) {
	h := newHarness(t)
	events := h.offer(baselineFacts()...)
	// Hand the reducer the batch with the first two accepted facts swapped.
	events[0], events[1] = events[1], events[0]
	_, err := Reduce(Input{
		Events: events, Policy: activePolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("got %v, want %v", err, ErrOutOfOrder)
	}
}

func TestRejectedArrivalsNeverReachReduction(t *testing.T) {
	_, err := Reduce(Input{
		Events: []Event{{
			Acceptance: connectivityaccept.Acceptance{
				Outcome: connectivityaccept.OutcomeRejected,
			},
			Fact: connectivity.FixtureBaseline(connectivity.ComponentDNS, 1),
		}},
		Policy: activePolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: evaluationTick,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want %v", err, ErrInvalidInput)
	}
}

func TestBootChangeInvalidatesPriorFreshness(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	// Same tick, still inside every deadline — but a different boot.
	output, err := Reduce(Input{
		Prior: h.snapshot, Policy: activePolicy(),
		BootID: "boot-2222222222222222", EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	for _, record := range output.Snapshot.Components {
		if record.State != StateStale {
			t.Fatalf("%s is %q under a new boot, want stale",
				record.Component, record.State)
		}
	}
}
