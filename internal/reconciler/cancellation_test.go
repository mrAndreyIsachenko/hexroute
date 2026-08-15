package reconciler

import (
	"errors"
	"sync"
	"testing"
)

func TestCancellationIntentBlocksNextUnstartedStepAndCancelsBeforeApply(t *testing.T) {
	journal := NewMemoryAttemptJournal()
	intents := NewMemoryCancellationIntentStore()
	registry := NewMemorySyntheticResourceRegistry()
	binding := attemptBinding()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestCancellation(journal, intents, binding.ActionID, testRequestID); err != nil {
		t.Fatalf("RequestCancellation() error = %v", err)
	}
	if err := CanStartNextSyntheticStep(intents, binding); !errors.Is(err, ErrCancellationBlocked) {
		t.Fatalf("CanStartNextSyntheticStep() error = %v, want %v", err, ErrCancellationBlocked)
	}
	resolution, err := ResolveCancellation(journal, intents, registry, nil, binding, nil)
	if err != nil {
		t.Fatalf("ResolveCancellation() error = %v", err)
	}
	if resolution.Attempt.Attempt.State != AttemptCancelled ||
		resolution.Compensation.Outcome != AttemptCancelled ||
		len(resolution.Compensation.Compensated) != 0 ||
		resolution.Outcome.Outcome != OutcomeCancelled ||
		resolution.Outcome.ReportDelivery != ReportPending {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestCancellationCompensatesVerifiedAppliedPrefixInReverseOrder(t *testing.T) {
	journal := NewMemoryAttemptJournal()
	intents := NewMemoryCancellationIntentStore()
	registry := NewMemorySyntheticResourceRegistry()
	binding := attemptBinding()
	adapter := memoryAdapterForDesired(t,
		desiredResource("resource.alpha", OperationSyntheticDNS, "alpha"),
		desiredResource("resource.beta", OperationSyntheticFirewall, "beta"),
	)
	steps := desiredSteps(t, adapter,
		desiredResource("resource.alpha", OperationSyntheticDNS, "alpha"),
		desiredResource("resource.beta", OperationSyntheticFirewall, "beta"),
	)
	for _, step := range steps {
		if _, err := adapter.Apply(step); err != nil {
			t.Fatal(err)
		}
		if err := adapter.Verify(step); err != nil {
			t.Fatal(err)
		}
	}
	startRunningAttempt(t, journal, binding)
	if _, err := RequestCancellation(journal, intents, binding.ActionID, testRequestID); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolveCancellation(journal, intents, registry, adapter, binding, steps)
	if err != nil {
		t.Fatalf("ResolveCancellation() error = %v", err)
	}
	if resolution.Attempt.Attempt.State != AttemptRolledBack ||
		resolution.Outcome.Outcome != OutcomeRolledBack ||
		resolution.Compensation.Reason != ReasonCompensation {
		t.Fatalf("resolution = %+v", resolution)
	}
	if got, want := resolution.Compensation.Compensated, []string{steps[1].ID, steps[0].ID}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("compensated = %v, want %v", got, want)
	}
	state, err := adapter.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Resources) != 0 {
		t.Fatalf("state after rollback = %+v", state)
	}
}

func TestCancellationRoutesUncertainCompensationOrCleanupToSafeMode(t *testing.T) {
	t.Run("foreign changed applied state", func(t *testing.T) {
		journal := NewMemoryAttemptJournal()
		intents := NewMemoryCancellationIntentStore()
		registry := NewMemorySyntheticResourceRegistry()
		binding := attemptBinding()
		adapter, err := NewMemorySyntheticAdapter([]SyntheticResource{
			foreignResource("resource.alpha", OperationSyntheticProcess, "alpha"),
		})
		if err != nil {
			t.Fatal(err)
		}
		step := desiredSteps(t, memoryAdapterForDesired(t,
			desiredResource("resource.alpha", OperationSyntheticProcess, "alpha"),
		), desiredResource("resource.alpha", OperationSyntheticProcess, "alpha"))[0]
		startRunningAttempt(t, journal, binding)
		if _, err := RequestCancellation(journal, intents, binding.ActionID, testRequestID); err != nil {
			t.Fatal(err)
		}
		resolution, err := ResolveCancellation(journal, intents, registry, adapter, binding, []SyntheticPlanStep{step})
		if err != nil {
			t.Fatalf("ResolveCancellation() error = %v", err)
		}
		if resolution.Attempt.Attempt.State != AttemptSafeMode ||
			resolution.Compensation.Incident == nil ||
			resolution.Outcome.Outcome != OutcomeSafeMode {
			t.Fatalf("resolution = %+v", resolution)
		}
	})

	t.Run("cleanup failure", func(t *testing.T) {
		journal := NewMemoryAttemptJournal()
		intents := NewMemoryCancellationIntentStore()
		registry := NewMemorySyntheticResourceRegistry()
		binding := attemptBinding()
		if _, err := registry.Register(binding, ResourceSyntheticHelper, "synthetic.helper"); err != nil {
			t.Fatal(err)
		}
		if err := registry.MarkFailed("synthetic.helper"); err != nil {
			t.Fatal(err)
		}
		startClaimedAttempt(t, journal, binding)
		if _, err := RequestCancellation(journal, intents, binding.ActionID, testRequestID); err != nil {
			t.Fatal(err)
		}
		resolution, err := ResolveCancellation(journal, intents, registry, nil, binding, nil)
		if err != nil {
			t.Fatalf("ResolveCancellation() error = %v", err)
		}
		if resolution.Attempt.Attempt.State != AttemptSafeMode ||
			resolution.Cleanup.Incident == nil ||
			resolution.Outcome.Outcome != OutcomeSafeMode {
			t.Fatalf("resolution = %+v", resolution)
		}
	})
}

func TestTypedSyntheticResourceRegistrationAndCleanup(t *testing.T) {
	registry := NewMemorySyntheticResourceRegistry()
	binding := attemptBinding()
	for _, item := range []struct {
		kind ResourceKind
		id   string
	}{
		{ResourceSyntheticHelper, "synthetic.helper"},
		{ResourceSyntheticFile, "synthetic.file"},
		{ResourceSyntheticLease, "synthetic.lease"},
	} {
		record, err := registry.Register(binding, item.kind, item.id)
		if err != nil {
			t.Fatalf("Register(%s) error = %v", item.id, err)
		}
		if record.State != ResourceRegistered ||
			record.OwnerSHA256 != AttemptBindingSHA256(binding) {
			t.Fatalf("record = %+v", record)
		}
	}
	cleanup := registry.CloseAll(binding)
	if cleanup.Uncertainty || len(cleanup.Closed) != 3 || len(cleanup.Failed) != 0 {
		t.Fatalf("cleanup = %+v", cleanup)
	}
	for _, record := range registry.Snapshot() {
		if record.State != ResourceClosed {
			t.Fatalf("record after cleanup = %+v", record)
		}
	}
}

func TestCancellationRacesSelectOneTerminalPath(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding)
		race  func(journal *MemoryAttemptJournal, binding AttemptBinding) error
	}{
		{
			name: "cancel versus claim",
			setup: func(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
				if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
					t.Fatal(err)
				}
			},
			race: func(journal *MemoryAttemptJournal, binding AttemptBinding) error {
				_, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted)
				return err
			},
		},
		{
			name: "cancel versus apply-start",
			setup: func(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
				startClaimedAttempt(t, journal, binding)
			},
			race: func(journal *MemoryAttemptJournal, binding AttemptBinding) error {
				_, err := journal.CompareAndSwap(binding, AttemptClaimed, AttemptRunning, ReasonAccepted)
				return err
			},
		},
		{
			name: "cancel versus commit",
			setup: func(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
				startVerifyingAttempt(t, journal, binding)
			},
			race: func(journal *MemoryAttemptJournal, binding AttemptBinding) error {
				_, err := journal.CompareAndSwap(binding, AttemptVerifying, AttemptCommitted, ReasonAccepted)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := NewMemoryAttemptJournal()
			intents := NewMemoryCancellationIntentStore()
			registry := NewMemorySyntheticResourceRegistry()
			binding := attemptBinding()
			test.setup(t, journal, binding)
			var wait sync.WaitGroup
			errs := make(chan error, 2)
			wait.Add(2)
			go func() {
				defer wait.Done()
				_, err := RequestCancellation(journal, intents, binding.ActionID, testRequestID)
				if err == nil {
					_, err = ResolveCancellation(journal, intents, registry, nil, binding, nil)
				}
				errs <- err
			}()
			go func() {
				defer wait.Done()
				errs <- test.race(journal, binding)
			}()
			wait.Wait()
			close(errs)
			for err := range errs {
				if err != nil &&
					!errors.Is(err, ErrAttemptCAS) &&
					!errors.Is(err, ErrAttemptTransition) &&
					!errors.Is(err, ErrCancellationRejected) {
					t.Fatalf("unexpected race error = %v", err)
				}
			}
			latest, exists, err := journal.Latest(binding.ActionID)
			if err != nil || !exists {
				t.Fatalf("Latest() exists=%v err=%v", exists, err)
			}
			if !terminalAttemptState(latest.Attempt.State) &&
				latest.Attempt.State != AttemptClaimed &&
				latest.Attempt.State != AttemptRunning &&
				latest.Attempt.State != AttemptVerifying {
				t.Fatalf("unexpected latest state = %+v", latest)
			}
		})
	}
}

func startClaimedAttempt(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
	t.Helper()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
}

func startRunningAttempt(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
	t.Helper()
	startClaimedAttempt(t, journal, binding)
	if _, err := journal.CompareAndSwap(binding, AttemptClaimed, AttemptRunning, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
}

func startVerifyingAttempt(t *testing.T, journal *MemoryAttemptJournal, binding AttemptBinding) {
	t.Helper()
	startRunningAttempt(t, journal, binding)
	if _, err := journal.CompareAndSwap(binding, AttemptRunning, AttemptVerifying, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
}

func memoryAdapterForDesired(t *testing.T, resources ...SyntheticDesiredResource) *MemorySyntheticAdapter {
	t.Helper()
	adapter, err := NewMemorySyntheticAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func desiredSteps(t *testing.T, adapter *MemorySyntheticAdapter, resources ...SyntheticDesiredResource) []SyntheticPlanStep {
	t.Helper()
	diff, err := adapter.SemanticCompare(syntheticDesired(resources...))
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Steps) != len(resources) {
		t.Fatalf("steps=%d resources=%d diff=%+v", len(diff.Steps), len(resources), diff)
	}
	return diff.Steps
}
