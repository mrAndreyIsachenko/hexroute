package configprove

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	proveVersionID    = metadata.UUID("11111111-1111-4111-8111-111111111111")
	proveDeploymentID = metadata.UUID("22222222-2222-4222-8222-222222222222")
)

type stubLedger struct {
	version Version
	err     error
	calls   []string
	reason  string
}

func (ledger *stubLedger) LatestVersion(context.Context, string, string) (Version, error) {
	if ledger.err != nil {
		return Version{}, ledger.err
	}
	return ledger.version, nil
}

func (ledger *stubLedger) MarkActive(_ context.Context, _ Version, _ metadata.UUID, _ time.Time) error {
	ledger.calls = append(ledger.calls, "active")
	return nil
}

func (ledger *stubLedger) MarkProven(context.Context, Version, time.Time) error {
	ledger.calls = append(ledger.calls, "proven")
	return nil
}

func (ledger *stubLedger) MarkRejected(_ context.Context, _ Version, _ time.Time, reason string) error {
	ledger.calls = append(ledger.calls, "rejected")
	ledger.reason = reason
	return nil
}

func publishedAt() time.Time { return time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC) }

func stagedVersion() Version {
	return Version{
		ConfigVersionID: proveVersionID,
		TargetKind:      "node",
		TargetKey:       "ingress-provider-b",
		VersionLabel:    "2026-09-06.1",
		Lifecycle:       LifecycleStaged,
		CreatedAt:       publishedAt(),
	}
}

func testProver(t *testing.T, ledger Ledger) *Prover {
	t.Helper()
	prover, err := New(ledger, time.Hour, 6*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return prover
}

// Delivery is not proof and one healthy answer is not a window. A version
// becomes proven only after the whole of it, and the ledger says nothing
// stronger in the meantime.
func TestAVersionIsProvenOnlyByHealthAcrossItsWholeWindow(t *testing.T) {
	ledger := &stubLedger{version: stagedVersion()}
	prover := testProver(t, ledger)
	healthy := Observation{Generation: "2026-09-06.1", Healthy: true}

	first, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
		healthy, proveDeploymentID, publishedAt().Add(time.Minute))
	if err != nil || first.Outcome != OutcomeActivated ||
		first.Lifecycle != string(LifecycleActive) {
		t.Fatalf("first: %+v %v", first, err)
	}

	// Active and inside the window: still nothing proven.
	ledger.version.Lifecycle = LifecycleActive
	ledger.version.ActivatedAt = publishedAt().Add(time.Minute)
	pending, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
		healthy, proveDeploymentID, publishedAt().Add(30*time.Minute))
	if err != nil || pending.Outcome != OutcomePending {
		t.Fatalf("pending: %+v %v", pending, err)
	}

	proven, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
		healthy, proveDeploymentID, publishedAt().Add(2*time.Hour))
	if err != nil || proven.Outcome != OutcomeProven {
		t.Fatalf("proven: %+v %v", proven, err)
	}
	if strings.Join(ledger.calls, ",") != "active,proven" {
		t.Fatalf("ledger calls: %v", ledger.calls)
	}
}

// Everything that is not this exact generation, healthy, is not proof.
func TestNothingWeakerThanTheRunningGenerationCounts(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		observation Observation
		reason      string
	}{
		{
			name:        "no heartbeat at all",
			observation: Observation{},
			reason:      "heartbeat_unavailable",
		},
		{
			name:        "a host still running the version before",
			observation: Observation{Generation: "2026-09-05.1", Healthy: true},
			reason:      "generation_not_running",
		},
		{
			name:        "the right version and a transport that does not work",
			observation: Observation{Generation: "2026-09-06.1"},
			reason:      "transport_unhealthy",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ledger := &stubLedger{version: stagedVersion()}
			prover := testProver(t, ledger)

			// Inside the deadline this is only news about now.
			pending, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
				testCase.observation, proveDeploymentID, publishedAt().Add(time.Hour))
			if err != nil || pending.Outcome != OutcomePending ||
				pending.Reason != testCase.reason || len(ledger.calls) != 0 {
				t.Fatalf("pending: %+v %v %v", pending, err, ledger.calls)
			}

			rejected, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
				testCase.observation, proveDeploymentID, publishedAt().Add(7*time.Hour))
			if err != nil || rejected.Outcome != OutcomeRejected ||
				rejected.Reason != testCase.reason {
				t.Fatalf("rejected: %+v %v", rejected, err)
			}
			if strings.Join(ledger.calls, ",") != "rejected" || ledger.reason != testCase.reason {
				t.Fatalf("ledger: %v %q", ledger.calls, ledger.reason)
			}
		})
	}
}

// A settled version is left alone. Reopening one would let a host that came
// back healthy after a rejection quietly reverse a decision already recorded.
func TestASettledVersionIsNotReopened(t *testing.T) {
	for _, lifecycle := range []Lifecycle{LifecycleProven, LifecycleRejected, LifecycleRetired} {
		ledger := &stubLedger{version: stagedVersion()}
		ledger.version.Lifecycle = lifecycle
		prover := testProver(t, ledger)
		result, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
			Observation{Generation: "2026-09-06.1", Healthy: true},
			proveDeploymentID, publishedAt().Add(48*time.Hour))
		if err != nil || result.Outcome != OutcomeSettled || len(ledger.calls) != 0 {
			t.Fatalf("%s: %+v %v %v", lifecycle, result, err, ledger.calls)
		}
	}
}

func TestAProverNeedsAWindowThatFitsInsideItsDeadline(t *testing.T) {
	ledger := &stubLedger{}
	for _, testCase := range []struct {
		name     string
		window   time.Duration
		deadline time.Duration
	}{
		{name: "no window", deadline: time.Hour},
		{name: "no deadline", window: time.Hour},
		{name: "a deadline inside the window", window: 2 * time.Hour, deadline: time.Hour},
		{name: "a deadline equal to the window", window: time.Hour, deadline: time.Hour},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := New(ledger, testCase.window, testCase.deadline); !errors.Is(err, ErrProve) {
				t.Fatalf("accepted %s", testCase.name)
			}
		})
	}
	if _, err := New(nil, time.Hour, 2*time.Hour); !errors.Is(err, ErrProve) {
		t.Fatal("accepted a prover with no ledger")
	}
}

func TestATargetWithNothingPublishedIsSaidSo(t *testing.T) {
	ledger := &stubLedger{err: ErrNoVersion}
	prover := testProver(t, ledger)
	if _, err := prover.Prove(context.Background(), "node", "ingress-provider-b",
		Observation{}, proveDeploymentID, publishedAt()); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("error: %v", err)
	}
}

// The probe already distinguishes a host running something else from a host
// running this and failing. Collapsing those two would make "not deployed"
// and "deployed and broken" the same record.
func TestTheProbeCategoriesMapOntoWhatWasMissing(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		observation Observation
		reason      string
	}{
		{
			name:        "the probe said OK",
			observation: Observation{Generation: "2026-09-06.1", Healthy: true},
		},
		{
			name:        "the probe said unhealthy",
			observation: Observation{Generation: "2026-09-06.1"},
			reason:      "transport_unhealthy",
		},
		{
			name:        "the probe said another generation",
			observation: Observation{Generation: "unreported"},
			reason:      "generation_not_running",
		},
		{
			name:   "the probe reached nothing",
			reason: "heartbeat_unavailable",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if reason := unprovenReason(testCase.observation, "2026-09-06.1"); reason != testCase.reason {
				t.Fatalf("reason = %q, want %q", reason, testCase.reason)
			}
		})
	}
}
