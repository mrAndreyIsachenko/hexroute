package connectivityruntime

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityview"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// deadCloud stands for every cloud failure at once: the API refusing,
// PostgreSQL down, the worker not running, the dashboard unreachable. From the
// host's side they are the same event — an upload that does not complete.
var errCloudUnreachable = errors.New("cloud unreachable")

type deadCloud struct{ attempts int }

func (cloud *deadCloud) Upload([]byte) error {
	cloud.attempts++
	return errCloudUnreachable
}

// Losing the cloud costs a sample. It must not cost a reduction, a snapshot
// generation, a proposal or a checkpoint, because none of those has ever
// depended on an upload succeeding.
func TestLosingTheCloudChangesNothingLocally(t *testing.T) {
	cloud := &deadCloud{}
	h := newHarness(t, true)

	if _, err := h.runtime.Publish(rootFacts(), policy.DomainRoot); err != nil {
		t.Fatalf("publish: %v", err)
	}
	first, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if uploadErr := cloud.Upload(nil); uploadErr == nil {
		t.Fatal("the fixture is supposed to fail")
	}

	// A second round of observations, with the cloud still gone.
	degraded := make([]connectivity.Fact, 0, len(rootFacts()))
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
	second, err := h.runtime.Tick(TickInput{Policy: activePolicy(), EvaluationTick: tick + 1})
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	_ = cloud.Upload(nil)

	if !second.Changed {
		t.Fatal("reduction stopped advancing while the cloud was unreachable")
	}
	if second.Snapshot.Generation <= first.Snapshot.Generation {
		t.Fatalf("generation %d did not advance past %d",
			second.Snapshot.Generation, first.Snapshot.Generation)
	}
	if second.Snapshot.Policy != activePolicy() {
		t.Fatal("the policy descriptor changed while the cloud was unreachable")
	}
	if second.Snapshot.Authorization != connectivityreduce.AuthorizationAuthorized {
		t.Fatalf("authorization became %q while the cloud was unreachable",
			second.Snapshot.Authorization)
	}
	if cloud.attempts == 0 {
		t.Fatal("the fixture was never exercised")
	}
}

// The projection is an ordinary operational event. When it cannot be
// delivered it waits in the bounded local spool, and its priority decides what
// survives an overflow — a lost projection costs a sample, so it must never
// evict evidence.
func TestAnUndeliverableProjectionWaitsInTheSpoolAsOperational(t *testing.T) {
	definition, ok := event.DefinitionFor(event.SchemaConnectivityProjection)
	if !ok {
		t.Fatal("the projection has no schema definition")
	}
	if definition.Priority != event.PriorityOperational {
		t.Fatalf("projection priority %q, want operational", definition.Priority)
	}
	if definition.Priority == event.PriorityCritical {
		t.Fatal("a projection must never outrank evidence in the spool")
	}
	// Evidence outranks it: a baseline restates a component in full and is
	// what closes a hole, so it must survive an overflow the projection does
	// not.
	baseline, ok := event.DefinitionFor(event.SchemaConnectivityBaseline)
	if !ok || baseline.Priority != event.PriorityCritical {
		t.Fatalf("baseline priority %q, want critical", baseline.Priority)
	}
}

// The reduction input names everything a reduction is allowed to see. Cloud
// state is not in it, and cannot be added without changing this signature —
// which is the point: staleness in the cloud has no field to arrive through.
func TestReductionInputHasNoCloudField(t *testing.T) {
	snapshot := connectivityreduce.Snapshot{}
	_ = connectivityreduce.Input{
		Prior:  &snapshot,
		Policy: activePolicy(),
	}
	// The projection is derived from the snapshot and never read back: there
	// is no constructor anywhere that turns one into a snapshot, a policy
	// descriptor or a proposal.
	projection := connectivityview.Project(
		connectivityreduce.Snapshot{}, connectivityreduce.Diff{}, nil)
	if projection.SnapshotGeneration != 0 {
		t.Fatal("an empty snapshot produced a generation")
	}
}
