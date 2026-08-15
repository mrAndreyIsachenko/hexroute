package reconciler

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestAttemptBindingUsesExistingLeaseExecutionClaim(t *testing.T) {
	lease := attemptLease()
	claim := attemptClaim(lease)
	binding, err := AttemptBindingFromLeaseClaim(lease, claim, 4)
	if err != nil {
		t.Fatalf("AttemptBindingFromLeaseClaim() error = %v", err)
	}
	if binding.ActionID != lease.ActionID ||
		binding.AttemptID != claim.AttemptID ||
		binding.Nonce != lease.Nonce ||
		binding.BootID != lease.BootID ||
		binding.ControlGeneration != lease.ControlStateGeneration ||
		binding.PlanSHA256 != lease.PlanSHA256 {
		t.Fatalf("binding = %+v", binding)
	}

	claim.Nonce = testRecordID
	if _, err := AttemptBindingFromLeaseClaim(lease, claim, 4); !errors.Is(err, ErrAttemptJournal) {
		t.Fatalf("mismatched claim error = %v, want %v", err, ErrAttemptJournal)
	}
}

func TestAttemptJournalCompareAndSwapLifecycle(t *testing.T) {
	journal := NewMemoryAttemptJournal()
	binding := attemptBinding()
	pending, err := journal.AppendPending(binding, ReasonAccepted)
	if err != nil {
		t.Fatalf("AppendPending() error = %v", err)
	}
	if pending.Sequence != 1 || pending.Attempt.State != AttemptPending {
		t.Fatalf("pending = %+v", pending)
	}

	for index, transition := range []struct {
		from AttemptState
		to   AttemptState
	}{
		{AttemptPending, AttemptClaimed},
		{AttemptClaimed, AttemptRunning},
		{AttemptRunning, AttemptVerifying},
		{AttemptVerifying, AttemptCommitted},
	} {
		entry, err := journal.CompareAndSwap(binding, transition.from, transition.to, ReasonAccepted)
		if err != nil {
			t.Fatalf("transition %s->%s error = %v", transition.from, transition.to, err)
		}
		if entry.Sequence != uint64(index+2) || entry.Previous != transition.from ||
			entry.Attempt.State != transition.to || entry.Binding != binding {
			t.Fatalf("entry = %+v", entry)
		}
	}
	latest, exists, err := journal.Latest(binding.ActionID)
	if err != nil || !exists {
		t.Fatalf("Latest() exists=%v error=%v", exists, err)
	}
	if latest.Attempt.State != AttemptCommitted {
		t.Fatalf("latest = %+v", latest)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptCommitted, AttemptRunning, ReasonAccepted); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("terminal transition error = %v, want %v", err, ErrAttemptTransition)
	}
}

func TestFileAttemptJournalPersistsAppendOnlyAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root", "attempts.log")
	journal, err := OpenFileAttemptJournal(path)
	if err != nil {
		t.Fatalf("OpenFileAttemptJournal() error = %v", err)
	}
	binding := attemptBinding()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatalf("AppendPending() error = %v", err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatalf("claim: %v", err)
	}

	reopened, err := OpenFileAttemptJournal(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	latest, exists, err := reopened.Latest(binding.ActionID)
	if err != nil || !exists {
		t.Fatalf("Latest() exists=%v error=%v", exists, err)
	}
	if latest.Attempt.State != AttemptClaimed || latest.Sequence != 2 {
		t.Fatalf("latest = %+v", latest)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(encoded, []byte("\n")); lines != 2 {
		t.Fatalf("append-only line count = %d, want 2", lines)
	}
}

func TestAttemptJournalRejectsDuplicateWorkerAndImmutableBindingDrift(t *testing.T) {
	journal := NewMemoryAttemptJournal()
	binding := attemptBinding()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); !errors.Is(err, ErrAttemptCAS) {
		t.Fatalf("duplicate claim error = %v, want %v", err, ErrAttemptCAS)
	}

	drifted := binding
	drifted.AttemptID = testRecordID
	if _, err := journal.CompareAndSwap(drifted, AttemptClaimed, AttemptRunning, ReasonAccepted); !errors.Is(err, ErrAttemptImmutable) {
		t.Fatalf("binding drift error = %v, want %v", err, ErrAttemptImmutable)
	}
}

func TestAttemptJournalClassifiesExpiryRebootAndStartupRecoveryWithoutAutoRun(t *testing.T) {
	tests := []struct {
		name        string
		state       AttemptState
		targetState TargetRecoveryState
		mutate      func(*StartupObservation)
		wantClass   StartupRecoveryClass
		wantReason  Reason
	}{
		{
			name:  "untouched claimed attempt is not rerun",
			state: AttemptClaimed, targetState: TargetUntouched,
			wantClass: RecoveryUntouched, wantReason: ReasonAccepted,
		},
		{
			name:  "owned applied state requires verification",
			state: AttemptRunning, targetState: TargetExactlyOwnedApplied,
			wantClass: RecoveryVerifyOwned, wantReason: ReasonVerification,
		},
		{
			name:  "uncertain target goes safe mode",
			state: AttemptVerifying, targetState: TargetUncertain,
			wantClass: RecoverySafeMode, wantReason: ReasonSafeMode,
		},
		{
			name:  "reboot blocks resume",
			state: AttemptRunning, targetState: TargetExactlyOwnedApplied,
			mutate: func(observation *StartupObservation) {
				observation.BootID = testRecordID
			},
			wantClass: RecoveryBlocked, wantReason: ReasonGeneration,
		},
		{
			name:  "different attempt blocks resume",
			state: AttemptRunning, targetState: TargetExactlyOwnedApplied,
			mutate: func(observation *StartupObservation) {
				observation.AttemptID = testRecordID
			},
			wantClass: RecoveryBlocked, wantReason: ReasonOwnership,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := attemptEntryAtState(t, test.state)
			observation := StartupObservation{
				BootID: entry.Binding.BootID, AttemptID: entry.Binding.AttemptID,
				TargetState: test.targetState,
			}
			if test.mutate != nil {
				test.mutate(&observation)
			}
			recovery, err := ClassifyStartupAttempt(entry, observation)
			if err != nil {
				t.Fatalf("ClassifyStartupAttempt() error = %v", err)
			}
			if recovery.Class != test.wantClass ||
				recovery.Reason != test.wantReason ||
				recovery.CanAutoRun {
				t.Fatalf("recovery = %+v", recovery)
			}
		})
	}

	journal := NewMemoryAttemptJournal()
	binding := attemptBinding()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	expired, err := journal.CompareAndSwap(binding, AttemptPending, AttemptExpired, ReasonExpired)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	recovery, err := ClassifyStartupAttempt(expired, StartupObservation{
		BootID: binding.BootID, AttemptID: binding.AttemptID, TargetState: TargetUntouched,
	})
	if err != nil {
		t.Fatalf("ClassifyStartupAttempt(expired) error = %v", err)
	}
	if recovery.Class != RecoveryTerminal || recovery.CanAutoRun {
		t.Fatalf("expired recovery = %+v", recovery)
	}
}

func TestAttemptJournalDetectsTampering(t *testing.T) {
	journal := NewMemoryAttemptJournal()
	binding := attemptBinding()
	if _, err := journal.AppendPending(binding, ReasonAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.CompareAndSwap(binding, AttemptPending, AttemptClaimed, ReasonAccepted); err != nil {
		t.Fatal(err)
	}

	journal.mu.Lock()
	journal.records[binding.ActionID][1].Binding.PlanSHA256 = testDigest("tampered")
	journal.mu.Unlock()

	if _, _, err := journal.Latest(binding.ActionID); !errors.Is(err, ErrAttemptJournalTamper) {
		t.Fatalf("Latest() error = %v, want %v", err, ErrAttemptJournalTamper)
	}
}

func attemptEntryAtState(t *testing.T, state AttemptState) AttemptJournalEntry {
	t.Helper()
	journal := NewMemoryAttemptJournal()
	binding := attemptBinding()
	entry, err := journal.AppendPending(binding, ReasonAccepted)
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range []struct {
		from AttemptState
		to   AttemptState
	}{
		{AttemptPending, AttemptClaimed},
		{AttemptClaimed, AttemptRunning},
		{AttemptRunning, AttemptVerifying},
	} {
		if state == entry.Attempt.State {
			return entry
		}
		entry, err = journal.CompareAndSwap(binding, transition.from, transition.to, ReasonAccepted)
		if err != nil {
			t.Fatal(err)
		}
	}
	if state == entry.Attempt.State {
		return entry
	}
	t.Fatalf("unsupported state %s", state)
	return AttemptJournalEntry{}
}

func attemptBinding() AttemptBinding {
	return AttemptBinding{
		ActionID:               testActionID,
		AttemptID:              testAttemptID,
		Nonce:                  testNonce,
		Domain:                 policy.DomainRoot,
		Target:                 "synthetic.target",
		BootID:                 testBootID,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		PlanSHA256:             testDigest("plan"),
	}
}

func attemptLease() policy.ActionLease {
	return policy.ActionLease{
		Schema:                 policy.ActionLeaseSchema,
		ActionID:               testActionID,
		Domain:                 policy.DomainRoot,
		Capability:             policy.CapabilityOperatorResume,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlStateGeneration: 1,
		Target:                 "synthetic.target",
		PlanSHA256:             testDigest("plan"),
		IssuedAt:               "2026-08-15T12:00:00Z",
		ExpiresAt:              "2026-08-15T12:00:30Z",
		IssuedMonotonicNS:      10_000_000_000,
		ExpiresMonotonicNS:     40_000_000_000,
		BootID:                 testBootID,
		Nonce:                  testNonce,
		Status:                 policy.LeasePending,
	}
}

func attemptClaim(lease policy.ActionLease) policy.ActionLeaseExecutionClaim {
	return policy.ActionLeaseExecutionClaim{
		Schema:    policy.ActionLeaseExecutionSchema,
		ActionID:  lease.ActionID,
		Domain:    lease.Domain,
		Nonce:     lease.Nonce,
		AttemptID: testAttemptID,
		BootID:    lease.BootID,
		ClaimedAt: "2026-08-15T12:00:01Z",
	}
}
