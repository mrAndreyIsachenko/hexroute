package reconciler

import (
	"errors"
	"strings"
	"testing"
)

func TestSyntheticOperationClassesRemainSeparated(t *testing.T) {
	classes := []OperationClass{
		OperationSyntheticTunnelInterface,
		OperationSyntheticScopedRoute,
		OperationSyntheticDNS,
		OperationSyntheticFirewall,
		OperationSyntheticProcess,
		OperationSyntheticUserAccess,
	}
	seen := make(map[OperationClass]struct{}, len(classes))
	for _, class := range classes {
		if !class.valid() {
			t.Fatalf("operation class %q is not allowlisted", class)
		}
		if _, exists := seen[class]; exists {
			t.Fatalf("duplicate operation class %q", class)
		}
		seen[class] = struct{}{}
	}
}

func TestSyntheticDiffNoopAndMissingStateRehydration(t *testing.T) {
	desired := syntheticDesired(
		desiredResource("resource.alpha", OperationSyntheticTunnelInterface, "desired-alpha"),
	)
	current := SyntheticState{Resources: []SyntheticResource{
		ownedResource("resource.alpha", OperationSyntheticTunnelInterface, "desired-alpha"),
	}}
	diff, err := DiffSyntheticState(current, desired)
	if err != nil {
		t.Fatalf("DiffSyntheticState(noop) error = %v", err)
	}
	if !diff.Noop || len(diff.Steps) != 0 || len(diff.Conflicts) != 0 {
		t.Fatalf("noop diff = %+v", diff)
	}

	diff, err = DiffSyntheticState(SyntheticState{}, desired)
	if err != nil {
		t.Fatalf("DiffSyntheticState(missing) error = %v", err)
	}
	if diff.Noop || len(diff.Steps) != 1 || len(diff.Conflicts) != 0 {
		t.Fatalf("missing diff = %+v", diff)
	}
	step := diff.Steps[0]
	if step.Operation != OperationSyntheticTunnelInterface ||
		step.BeforeSHA256 != syntheticMissingStateSHA256() ||
		step.AppliedSHA256 != testDigest("desired-alpha") ||
		strings.Contains(step.ID, "restart") {
		t.Fatalf("rehydration step = %+v", step)
	}
}

func TestSyntheticDiffOwnedDivergenceAndConflicts(t *testing.T) {
	desired := syntheticDesired(
		desiredResource("resource.route", OperationSyntheticScopedRoute, "desired-route"),
	)
	current := SyntheticState{Resources: []SyntheticResource{
		ownedResource("resource.route", OperationSyntheticScopedRoute, "old-route"),
	}}
	diff, err := DiffSyntheticState(current, desired)
	if err != nil {
		t.Fatalf("DiffSyntheticState(divergence) error = %v", err)
	}
	if len(diff.Steps) != 1 || len(diff.Conflicts) != 0 ||
		diff.Steps[0].BeforeSHA256 != testDigest("old-route") {
		t.Fatalf("owned divergence diff = %+v", diff)
	}

	tests := []struct {
		name     string
		resource SyntheticResource
		reason   Reason
	}{
		{
			name:     "foreign",
			resource: foreignResource("resource.route", OperationSyntheticScopedRoute, "old-route"),
			reason:   ReasonOwnership,
		},
		{
			name: "ambiguous",
			resource: SyntheticResource{
				ID: "resource.route", Operation: OperationSyntheticScopedRoute,
				StateSHA256: testDigest("old-route"), Ownership: SyntheticAmbiguous,
			},
			reason: ReasonOwnership,
		},
		{
			name: "protected",
			resource: SyntheticResource{
				ID: "resource.route", Operation: OperationSyntheticScopedRoute,
				StateSHA256: testDigest("old-route"), Ownership: SyntheticOwned,
				OwnerActionID: testActionID, OwnerAttemptID: testAttemptID, Protected: true,
			},
			reason: ReasonPolicy,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := DiffSyntheticState(SyntheticState{Resources: []SyntheticResource{test.resource}}, desired)
			if err != nil {
				t.Fatalf("DiffSyntheticState() error = %v", err)
			}
			if len(diff.Steps) != 0 || len(diff.Conflicts) != 1 ||
				diff.Conflicts[0].Reason != test.reason {
				t.Fatalf("conflict diff = %+v", diff)
			}
		})
	}
}

func TestSyntheticDiffRejectsStaleOrUnauthorizedDesiredStateAndUnexpectedCurrent(t *testing.T) {
	desired := syntheticDesired(desiredResource("resource.dns", OperationSyntheticDNS, "desired-dns"))
	desired.Fresh = false
	if _, err := DiffSyntheticState(SyntheticState{}, desired); !errors.Is(err, ErrSyntheticAdapter) {
		t.Fatalf("stale desired error = %v, want %v", err, ErrSyntheticAdapter)
	}
	desired = syntheticDesired(desiredResource("resource.dns", OperationSyntheticDNS, "desired-dns"))
	desired.Authorized = false
	if _, err := DiffSyntheticState(SyntheticState{}, desired); !errors.Is(err, ErrSyntheticAdapter) {
		t.Fatalf("unauthorized desired error = %v, want %v", err, ErrSyntheticAdapter)
	}

	diff, err := DiffSyntheticState(SyntheticState{Resources: []SyntheticResource{
		ownedResource("resource.extra", OperationSyntheticDNS, "extra"),
	}}, syntheticDesired())
	if err != nil {
		t.Fatalf("DiffSyntheticState(unexpected) error = %v", err)
	}
	if len(diff.Steps) != 0 || len(diff.Conflicts) != 1 ||
		diff.Conflicts[0].Reason != ReasonLineage {
		t.Fatalf("unexpected current diff = %+v", diff)
	}
}

func TestMemorySyntheticAdapterApplyVerifyCompensateAndVerificationMismatch(t *testing.T) {
	adapter, err := NewMemorySyntheticAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := adapter.SemanticCompare(syntheticDesired(
		desiredResource("resource.firewall", OperationSyntheticFirewall, "desired-firewall"),
	))
	if err != nil {
		t.Fatalf("SemanticCompare() error = %v", err)
	}
	if len(diff.Steps) != 1 {
		t.Fatalf("diff = %+v", diff)
	}
	step := diff.Steps[0]
	if _, err := adapter.Apply(step); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := adapter.Verify(step); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	state, err := adapter.Compensate(step)
	if err != nil {
		t.Fatalf("Compensate() error = %v", err)
	}
	if len(state.Resources) != 0 {
		t.Fatalf("state after compensate = %+v", state)
	}
	if err := adapter.Verify(step); !errors.Is(err, ErrSyntheticVerification) {
		t.Fatalf("Verify(after compensate) error = %v, want %v", err, ErrSyntheticVerification)
	}
}

func TestCrashFixtureSyntheticAdapterFaultsAreDeterministic(t *testing.T) {
	adapter, err := NewCrashFixtureSyntheticAdapter(nil, []SyntheticFaultPoint{SyntheticFaultApply})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := adapter.SemanticCompare(syntheticDesired(
		desiredResource("resource.user", OperationSyntheticUserAccess, "desired-user"),
	))
	if err != nil {
		t.Fatalf("SemanticCompare() error = %v", err)
	}
	if _, err := adapter.Apply(diff.Steps[0]); !errors.Is(err, ErrSyntheticFault) {
		t.Fatalf("Apply() error = %v, want %v", err, ErrSyntheticFault)
	}
}

func syntheticDesired(resources ...SyntheticDesiredResource) SyntheticDesiredState {
	return SyntheticDesiredState{
		Fresh: true, Authorized: true, Owner: attemptBinding(),
		Resources: resources,
	}
}

func desiredResource(id string, operation OperationClass, state string) SyntheticDesiredResource {
	return SyntheticDesiredResource{
		ID: id, Operation: operation,
		InputSHA256: testDigest("input-" + id + "-" + string(operation)),
		StateSHA256: testDigest(state),
	}
}

func ownedResource(id string, operation OperationClass, state string) SyntheticResource {
	return SyntheticResource{
		ID: id, Operation: operation, StateSHA256: testDigest(state),
		Ownership: SyntheticOwned, OwnerActionID: testActionID, OwnerAttemptID: testAttemptID,
	}
}

func foreignResource(id string, operation OperationClass, state string) SyntheticResource {
	return SyntheticResource{
		ID: id, Operation: operation, StateSHA256: testDigest(state),
		Ownership: SyntheticForeign, OwnerActionID: testRecordID, OwnerAttemptID: testRequestID,
	}
}
