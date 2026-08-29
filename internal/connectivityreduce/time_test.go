package connectivityreduce

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

func (h *harness) reduceWith(input Input) (Output, error) {
	h.t.Helper()
	input.Prior = h.snapshot
	if input.Policy == (PolicyDescriptor{}) {
		input.Policy = activePolicy()
	}
	if input.BootID == "" {
		input.BootID = connectivity.FixtureBootID
	}
	if input.EvaluationTick == 0 {
		input.EvaluationTick = evaluationTick
	}
	output, err := Reduce(input)
	if err == nil {
		h.snapshot = &output.Snapshot
	}
	return output, err
}

func statesByComponent(snapshot Snapshot) map[connectivity.Component]ComponentState {
	states := make(map[connectivity.Component]ComponentState, len(snapshot.Components))
	for _, record := range snapshot.Components {
		states[record.Component] = record.State
	}
	return states
}

// A wake withdraws the assumption that time-sensitive observations survived.
func TestFullWakeHoldsTimeSensitiveComponentsStale(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	output, err := h.reduceWith(Input{Wake: &Wake{Tick: evaluationTick + 1}})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	states := statesByComponent(output.Snapshot)
	for _, component := range connectivity.Components() {
		if component.TimeSensitive() {
			if states[component] != StateStale {
				t.Fatalf("%s is %q after a wake, want stale", component, states[component])
			}
			continue
		}
		if states[component] == StateStale {
			t.Fatalf("%s went stale on a wake it cannot be affected by", component)
		}
	}
}

// Only a complete restatement clears it. A fresh ordinary observation
// describes now without accounting for the time the host was not running.
func TestOnlyABaselineClearsAWake(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))
	h.reduceWith(Input{Wake: &Wake{Tick: evaluationTick + 1}})

	ordinary := connectivity.FixtureBaseline(connectivity.ComponentDNS, 20)
	ordinary.Baseline = false
	ordinary.Reason = connectivity.ReasonProbeSucceeded
	output, err := h.reduceWith(Input{Events: h.offer(ordinary)})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if statesByComponent(output.Snapshot)[connectivity.ComponentDNS] != StateStale {
		t.Fatal("an ordinary observation cleared a wake")
	}

	restated := connectivity.FixtureBaseline(connectivity.ComponentDNS, 21)
	restated.Reason = connectivity.ReasonWakeRebaseline
	output, err = h.reduceWith(Input{Events: h.offer(restated)})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if statesByComponent(output.Snapshot)[connectivity.ComponentDNS] != StateReady {
		t.Fatal("a complete restatement did not clear the wake")
	}
	// The components that have not restated are still held.
	if statesByComponent(output.Snapshot)[connectivity.ComponentRelays] != StateStale {
		t.Fatal("clearing one component cleared the others")
	}
}

// A baseline arriving in the same batch as the wake needs nothing further.
func TestBaselineInTheWakeBatchClearsItImmediately(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	restated := connectivity.FixtureBaseline(connectivity.ComponentDNS, 30)
	restated.Reason = connectivity.ReasonWakeRebaseline
	output, err := h.reduceWith(Input{
		Events: h.offer(restated), Wake: &Wake{Tick: evaluationTick + 1},
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if statesByComponent(output.Snapshot)[connectivity.ComponentDNS] != StateReady {
		t.Fatal("a baseline in the wake batch did not clear it")
	}
}

// A reboot invalidates monotonic deadlines outright, and requires the same
// complete restatement a wake does.
func TestRebootRequiresRebaselining(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))

	output, err := h.reduceWith(Input{BootID: "boot-3333333333333333"})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	for _, record := range output.Snapshot.Components {
		if record.State != StateStale {
			t.Fatalf("%s is %q under a new boot, want stale", record.Component, record.State)
		}
	}
	// The deadline is retained for display but stays attributed to the boot
	// that issued it, which is what stops it from being compared against the
	// current one.
	for _, record := range output.Snapshot.Components {
		if record.FreshnessDeadline == 0 {
			continue
		}
		if record.BootID == output.Snapshot.BootID {
			t.Fatalf("%s claims the new boot without having been re-observed",
				record.Component)
		}
		if record.BootID != connectivity.FixtureBootID {
			t.Fatalf("%s lost the boot its deadline belongs to", record.Component)
		}
	}
}

// The wall clock is for display. A jump in it must not make anything fresher
// or staler than the monotonic tick says.
func TestWallClockJumpDoesNotAffectFreshness(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	h.reduce(h.offer(facts...))
	before := statesByComponent(*h.snapshot)

	jumped := make([]connectivity.Fact, 0, len(facts))
	for index, component := range connectivity.Components() {
		fact := connectivity.FixtureBaseline(component, uint64(index+1+len(facts)))
		// A year into the past, same monotonic tick domain.
		fact.ObservedAt = fact.ObservedAt.AddDate(-1, 0, 0).UTC()
		jumped = append(jumped, fact)
	}
	output, err := h.reduceWith(Input{Events: h.offer(jumped...)})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	after := statesByComponent(output.Snapshot)
	for component, state := range before {
		if after[component] != state {
			t.Fatalf("%s changed from %q to %q on a wall-clock jump",
				component, state, after[component])
		}
	}
}

// Monotonic time may not run backwards inside one boot: a deadline measured
// against a regressed tick would make a stale component look fresh again.
func TestMonotonicTickCannotRegressWithinABoot(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))
	h.reduceWith(Input{EvaluationTick: evaluationTick + 100})

	_, err := Reduce(Input{
		Prior: h.snapshot, Policy: activePolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick + 50,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want %v", err, ErrInvalidInput)
	}

	// Across a boot the comparison is meaningless, so it is not made.
	if _, err := Reduce(Input{
		Prior: h.snapshot, Policy: activePolicy(),
		BootID: "boot-4444444444444444", EvaluationTick: 1,
	}); err != nil {
		t.Fatalf("a new boot was refused for restarting its clock: %v", err)
	}
}

// A session that expired while the host slept must not be summarised as valid.
func TestExpiredSessionSurvivesTheWakeAsStale(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentSessionExpiry {
			facts[index].Payload.SessionExpiry = &connectivity.SessionExpiryPayload{
				ExpiryClass: connectivity.ExpiryExpiring, Sessions: 1,
			}
		}
	}
	h.reduce(h.offer(facts...))

	output, err := h.reduceWith(Input{Wake: &Wake{Tick: evaluationTick + 1}})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if statesByComponent(output.Snapshot)[connectivity.ComponentSessionExpiry] != StateStale {
		t.Fatal("a session was carried across a sleep as if it were still valid")
	}
	if output.Snapshot.Summary.State == AggregateReady {
		t.Fatal("the summary reported a woken host as ready")
	}
}

// A baseline that has not arrived yet leaves the component stale rather than
// letting the previous answer stand in for it.
func TestDelayedBaselineLeavesTheComponentStale(t *testing.T) {
	h := newHarness(t)
	h.reduce(h.offer(baselineFacts()...))
	h.reduceWith(Input{Wake: &Wake{Tick: evaluationTick + 1}})

	for step := control.Tick(1); step <= 3; step++ {
		output, err := h.reduceWith(Input{EvaluationTick: evaluationTick + step})
		if err != nil {
			t.Fatalf("reduce: %v", err)
		}
		if statesByComponent(output.Snapshot)[connectivity.ComponentRelays] != StateStale {
			t.Fatal("waiting turned into an answer")
		}
	}
}

// A proposal minted before a sleep or a reboot describes a host that has since
// stopped running. It cannot become current again without a fresh reduction.
func TestProposalsFromBeforeASleepCannotBeResumed(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleDegraded
			facts[index].Reason = connectivity.ReasonProbeFailed
		}
	}
	first := h.reduceManaged(h.offer(facts...), managedPolicy())
	if len(first.Proposals) == 0 {
		t.Fatal("the fixture produced no proposal to go stale")
	}
	proposal := first.Proposals[0]

	for _, transition := range []struct {
		name  string
		input Input
	}{
		{"wake", Input{Wake: &Wake{Tick: evaluationTick + 1}, PolicyComponents: managedPolicy()}},
		{"reboot", Input{BootID: "boot-5555555555555555", PolicyComponents: managedPolicy()}},
	} {
		t.Run(transition.name, func(t *testing.T) {
			fresh := newHarness(t)
			fresh.reduceManaged(fresh.offer(facts...), managedPolicy())
			input := transition.input
			input.Prior = fresh.snapshot
			input.Policy = activePolicy()
			if input.BootID == "" {
				input.BootID = connectivity.FixtureBootID
			}
			input.EvaluationTick = evaluationTick + 1
			output, err := Reduce(input)
			if err != nil {
				t.Fatalf("reduce: %v", err)
			}
			digest, err := output.Diff.Digest()
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			if err := proposal.VerifyCurrent(output.Snapshot, digest); !errors.Is(err, ErrStaleProposal) {
				t.Fatalf("a pre-%s proposal was still current: %v", transition.name, err)
			}
		})
	}
}

var _ = time.UTC
