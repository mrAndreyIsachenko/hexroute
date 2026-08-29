package connectivityreduce

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// Classification is how one component compares to what policy wants.
type Classification string

const (
	ClassConverged     Classification = "converged"
	ClassMissing       Classification = "missing"
	ClassUnexpected    Classification = "unexpected"
	ClassDivergent     Classification = "divergent"
	ClassStale         Classification = "stale"
	ClassUnknown       Classification = "unknown"
	ClassConflict      Classification = "conflict"
	ClassGrandfathered Classification = "grandfathered_noncompliant"
)

// DiffReason is the bounded explanation for a classification.
type DiffReason string

const (
	DiffReasonNone             DiffReason = "none"
	DiffReasonNotObserved      DiffReason = "not_observed"
	DiffReasonStaleObservation DiffReason = "stale_observation"
	DiffReasonOwnerConflict    DiffReason = "owner_conflict"
	DiffReasonNothingPresent   DiffReason = "nothing_present"
	DiffReasonBelowExpected    DiffReason = "below_expected_count"
	DiffReasonClassMismatch    DiffReason = "class_mismatch"
	DiffReasonFailed           DiffReason = "observed_failed"
	DiffReasonDegraded         DiffReason = "observed_degraded"
	DiffReasonNotManaged       DiffReason = "not_managed_by_policy"
	DiffReasonPolicyExcluded   DiffReason = "excluded_by_new_policy"
	DiffReasonUnauthorized     DiffReason = "policy_unauthorized"
)

// ComponentDiff is one component's comparison result.
type ComponentDiff struct {
	Component      connectivity.Component `json:"component"`
	Domain         policy.Domain          `json:"domain"`
	Classification Classification         `json:"classification"`
	Reason         DiffReason             `json:"reason"`
}

// Diff is the whole comparison at one snapshot generation.
type Diff struct {
	Schema             string           `json:"schema"`
	Version            uint16           `json:"version"`
	SnapshotGeneration uint64           `json:"snapshot_generation"`
	Policy             PolicyDescriptor `json:"policy"`
	Authorized         bool             `json:"authorized"`
	Components         []ComponentDiff  `json:"components"`
}

// Digest returns the canonical digest a proposal binds itself to.
func (diff Diff) Digest() (string, error) {
	digest, _, err := policy.CanonicalSHA256(diff)
	if err != nil {
		return "", ErrInvalidSnapshot
	}
	return digest, nil
}

// Compare classifies every component against the desired state.
//
// prior is consulted only to tell a state that policy has just stopped
// permitting from one that was never permitted. Nothing here changes anything:
// the diff is a description.
func Compare(snapshot Snapshot, desired DesiredState, prior *Snapshot) Diff {
	diff := Diff{
		Schema:             DiffSchema,
		Version:            DiffSchemaVersion,
		SnapshotGeneration: snapshot.Generation,
		Policy:             snapshot.Policy,
		Authorized:         snapshot.Authorization == AuthorizationAuthorized && desired.Authorized,
	}
	wanted := make(map[connectivity.Component]DesiredComponent, len(desired.Components))
	for _, component := range desired.Components {
		wanted[component.Component] = component
	}

	for _, record := range snapshot.Components {
		entry := ComponentDiff{Component: record.Component, Domain: record.Domain}
		if !diff.Authorized {
			// Without authority there is no desired state to compare against,
			// so nothing is divergent — the observations simply stand alone.
			entry.Classification = ClassUnknown
			entry.Reason = DiffReasonUnauthorized
			diff.Components = append(diff.Components, entry)
			continue
		}
		entry.Classification, entry.Reason = classify(record, wanted[record.Component], prior, snapshot)
		diff.Components = append(diff.Components, entry)
	}
	return diff
}

func classify(
	record ComponentRecord,
	want DesiredComponent,
	prior *Snapshot,
	snapshot Snapshot,
) (Classification, DiffReason) {
	// Uncertainty is reported as uncertainty. A component whose owner is in
	// conflict or has gone quiet is not evidence for or against convergence.
	switch record.State {
	case StateConflict:
		return ClassConflict, DiffReasonOwnerConflict
	case StateStale:
		return ClassStale, DiffReasonStaleObservation
	case StateUnknown:
		return ClassUnknown, DiffReasonNotObserved
	}

	if !want.Managed {
		if record.State == StateNotApplicable {
			return ClassConverged, DiffReasonNotManaged
		}
		// Something established is running that the current policy does not
		// ask for. If an earlier policy generation did, it is grandfathered:
		// reported, never withdrawn by this change.
		if establishedUnderEarlierPolicy(record, prior, snapshot) {
			return ClassGrandfathered, DiffReasonPolicyExcluded
		}
		return ClassUnexpected, DiffReasonNotManaged
	}

	if want.Expect.Lifecycle == connectivity.LifecycleNotApplicable {
		if record.State == StateNotApplicable {
			return ClassConverged, DiffReasonNone
		}
		if establishedUnderEarlierPolicy(record, prior, snapshot) {
			return ClassGrandfathered, DiffReasonPolicyExcluded
		}
		return ClassUnexpected, DiffReasonNone
	}

	switch record.State {
	case StateFailed:
		if absent(record) {
			return ClassMissing, DiffReasonNothingPresent
		}
		return ClassDivergent, DiffReasonFailed
	case StateDegraded:
		return ClassDivergent, DiffReasonDegraded
	case StateNotApplicable:
		return ClassMissing, DiffReasonNothingPresent
	}

	if class, reason, ok := pinMismatch(record, want.Expect); ok {
		return class, reason
	}
	return ClassConverged, DiffReasonNone
}

// absent reports whether the component's own payload says nothing is there,
// which separates "it is broken" from "it was never established".
func absent(record ComponentRecord) bool {
	switch {
	case record.Payload.PhysicalNetwork != nil:
		return record.Payload.PhysicalNetwork.LinkClass == connectivity.LinkNone
	case record.Payload.DefaultPath != nil:
		return record.Payload.DefaultPath.PathClass == connectivity.PathNone
	case record.Payload.DNS != nil:
		return record.Payload.DNS.ResolverClass == connectivity.ResolverNone
	case record.Payload.ScopedRoutes != nil:
		return record.Payload.ScopedRoutes.Installed == 0
	case record.Payload.Transports != nil:
		return record.Payload.Transports.Ready == 0
	case record.Payload.Relays != nil:
		return record.Payload.Relays.Reachable == 0
	case record.Payload.UserAccess != nil:
		return record.Payload.UserAccess.ProfileClass == connectivity.ProfileNone
	case record.Payload.SessionExpiry != nil:
		return record.Payload.SessionExpiry.ExpiryClass == connectivity.ExpiryNone
	default:
		return true
	}
}

// pinMismatch compares a ready component against the pins policy set.
func pinMismatch(record ComponentRecord, expect Expectation) (Classification, DiffReason, bool) {
	if expect.PathClass != nil && record.Payload.DefaultPath != nil &&
		record.Payload.DefaultPath.PathClass != *expect.PathClass {
		return ClassDivergent, DiffReasonClassMismatch, true
	}
	if expect.ResolverClass != nil && record.Payload.DNS != nil &&
		record.Payload.DNS.ResolverClass != *expect.ResolverClass {
		return ClassDivergent, DiffReasonClassMismatch, true
	}
	if expect.SelectedClass != nil && record.Payload.Relays != nil &&
		record.Payload.Relays.SelectedClass != *expect.SelectedClass {
		return ClassDivergent, DiffReasonClassMismatch, true
	}
	if expect.ProfileClass != nil && record.Payload.UserAccess != nil &&
		record.Payload.UserAccess.ProfileClass != *expect.ProfileClass {
		return ClassDivergent, DiffReasonClassMismatch, true
	}
	if expect.MinInstalledRoutes != nil && record.Payload.ScopedRoutes != nil &&
		record.Payload.ScopedRoutes.Installed < *expect.MinInstalledRoutes {
		if record.Payload.ScopedRoutes.Installed == 0 {
			return ClassMissing, DiffReasonNothingPresent, true
		}
		return ClassDivergent, DiffReasonBelowExpected, true
	}
	if expect.MinReadyTransports != nil && record.Payload.Transports != nil &&
		record.Payload.Transports.Ready < *expect.MinReadyTransports {
		if record.Payload.Transports.Ready == 0 {
			return ClassMissing, DiffReasonNothingPresent, true
		}
		return ClassDivergent, DiffReasonBelowExpected, true
	}
	if expect.MinReachableRelays != nil && record.Payload.Relays != nil &&
		record.Payload.Relays.Reachable < *expect.MinReachableRelays {
		if record.Payload.Relays.Reachable == 0 {
			return ClassMissing, DiffReasonNothingPresent, true
		}
		return ClassDivergent, DiffReasonBelowExpected, true
	}
	return "", "", false
}

// establishedUnderEarlierPolicy reports whether this component was already
// established before the policy that now excludes it took effect.
func establishedUnderEarlierPolicy(record ComponentRecord, prior *Snapshot, snapshot Snapshot) bool {
	if prior == nil || !prior.Policy.Present {
		return false
	}
	if prior.Policy.BundleGeneration >= snapshot.Policy.BundleGeneration {
		return false
	}
	for _, before := range prior.Components {
		if before.Component != record.Component {
			continue
		}
		return before.State == StateReady || before.State == StateDegraded
	}
	return false
}
