package connectivityreduce

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func u16(value uint16) *uint16 { return &value }

// managedPolicy asks for everything the fixtures report, so the baseline set
// converges and any single deviation stands out.
func managedPolicy() []ComponentPolicy {
	path := connectivity.PathTunneled
	resolver := connectivity.ResolverScoped
	selected := connectivity.SelectedPrimary
	profile := connectivity.ProfileConfigured
	return []ComponentPolicy{
		{Component: connectivity.ComponentPhysicalNetwork, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady}},
		{Component: connectivity.ComponentDefaultPath, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady, PathClass: &path}},
		{Component: connectivity.ComponentDNS, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady, ResolverClass: &resolver}},
		{Component: connectivity.ComponentScopedRoutes, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady, MinInstalledRoutes: u16(7)}},
		{Component: connectivity.ComponentTransports, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady, MinReadyTransports: u16(3)}},
		{Component: connectivity.ComponentRelays, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady,
				SelectedClass: &selected, MinReachableRelays: u16(3)}},
		{Component: connectivity.ComponentUserAccess, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady, ProfileClass: &profile}},
		{Component: connectivity.ComponentSessionExpiry, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady}},
	}
}

func (h *harness) reduceManaged(events []Event, components []ComponentPolicy) Output {
	h.t.Helper()
	output, err := Reduce(Input{
		Prior: h.snapshot, Events: events, Policy: activePolicy(),
		PolicyComponents: components,
		BootID:           connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		h.t.Fatalf("reduce: %v", err)
	}
	h.snapshot = &output.Snapshot
	return output
}

func classFor(diff Diff, component connectivity.Component) ComponentDiff {
	for _, entry := range diff.Components {
		if entry.Component == component {
			return entry
		}
	}
	return ComponentDiff{}
}

func TestDesiredStateAlwaysCoversEveryComponent(t *testing.T) {
	desired, err := Desire(EffectivePolicy{
		Descriptor: activePolicy(),
		Components: []ComponentPolicy{{
			Component: connectivity.ComponentDNS, Managed: true,
			Expect: Expectation{Lifecycle: connectivity.LifecycleReady},
		}},
	})
	if err != nil {
		t.Fatalf("desire: %v", err)
	}
	if len(desired.Components) != len(connectivity.Components()) {
		t.Fatalf("got %d components, want %d",
			len(desired.Components), len(connectivity.Components()))
	}
	managed := 0
	for _, component := range desired.Components {
		if component.Managed {
			managed++
		}
		if component.Domain == "" {
			t.Fatalf("%s has no owning domain", component.Component)
		}
	}
	if managed != 1 {
		t.Fatalf("%d components are managed, want 1", managed)
	}
}

// An unauthorized policy cannot express a desire at all, and the difference
// between that and "nothing is wanted" must stay visible.
func TestUnauthorizedPolicyDesiresNothingAndProposesNothing(t *testing.T) {
	h := newHarness(t)
	output, err := Reduce(Input{
		Events: h.offer(baselineFacts()...), Policy: PolicyDescriptor{},
		PolicyComponents: managedPolicy(),
		BootID:           connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if output.Desired.Authorized || output.Diff.Authorized {
		t.Fatal("an absent policy produced an authorized desire")
	}
	for _, component := range output.Desired.Components {
		if component.Managed {
			t.Fatalf("%s is managed under an unauthorized policy", component.Component)
		}
	}
	for _, entry := range output.Diff.Components {
		if entry.Reason != DiffReasonUnauthorized {
			t.Fatalf("%s reason %q, want policy_unauthorized", entry.Component, entry.Reason)
		}
	}
	if len(output.Proposals) != 0 {
		t.Fatalf("%d proposals under an unauthorized policy", len(output.Proposals))
	}
}

func TestPolicyCannotPinAnotherComponentsAspect(t *testing.T) {
	resolver := connectivity.ResolverScoped
	_, err := Desire(EffectivePolicy{
		Descriptor: activePolicy(),
		Components: []ComponentPolicy{{
			Component: connectivity.ComponentScopedRoutes, Managed: true,
			Expect: Expectation{
				Lifecycle: connectivity.LifecycleReady, ResolverClass: &resolver,
			},
		}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want %v", err, ErrInvalidInput)
	}
}

func TestConvergedHostProposesNothing(t *testing.T) {
	h := newHarness(t)
	output := h.reduceManaged(h.offer(baselineFacts()...), managedPolicy())
	for _, entry := range output.Diff.Components {
		if entry.Classification != ClassConverged {
			t.Fatalf("%s is %q/%q, want converged",
				entry.Component, entry.Classification, entry.Reason)
		}
	}
	if len(output.Proposals) != 0 {
		t.Fatalf("%d proposals for a converged host", len(output.Proposals))
	}
}

func TestDivergenceIsClassifiedAndProposed(t *testing.T) {
	tests := []struct {
		name      string
		component connectivity.Component
		mutate    func(*connectivity.Fact)
		class     Classification
		reason    DiffReason
		proposal  ProposalClass
	}{
		{
			name: "route count below expectation", component: connectivity.ComponentScopedRoutes,
			mutate: func(f *connectivity.Fact) {
				f.Payload.ScopedRoutes = &connectivity.ScopedRoutesPayload{Configured: 7, Installed: 4}
			},
			class: ClassDivergent, reason: DiffReasonBelowExpected, proposal: ProposalReconcile,
		},
		{
			name: "nothing installed", component: connectivity.ComponentScopedRoutes,
			mutate: func(f *connectivity.Fact) {
				f.Payload.ScopedRoutes = &connectivity.ScopedRoutesPayload{Configured: 7, Installed: 0}
			},
			class: ClassMissing, reason: DiffReasonNothingPresent, proposal: ProposalEstablish,
		},
		{
			name: "path class mismatch", component: connectivity.ComponentDefaultPath,
			mutate: func(f *connectivity.Fact) {
				f.Payload.DefaultPath = &connectivity.DefaultPathPayload{
					PathClass: connectivity.PathDirect, GatewayPresent: true}
			},
			class: ClassDivergent, reason: DiffReasonClassMismatch, proposal: ProposalReconcile,
		},
		{
			name: "relay fell back to reserve", component: connectivity.ComponentRelays,
			mutate: func(f *connectivity.Fact) {
				f.Payload.Relays = &connectivity.RelaysPayload{
					Configured: 3, Reachable: 3, Reserve: 1,
					SelectedClass: connectivity.SelectedReserve}
			},
			class: ClassDivergent, reason: DiffReasonClassMismatch, proposal: ProposalReconcile,
		},
		{
			name: "observed degraded", component: connectivity.ComponentTransports,
			mutate: func(f *connectivity.Fact) {
				f.Lifecycle = connectivity.LifecycleDegraded
				f.Reason = connectivity.ReasonProbeFailed
			},
			class: ClassDivergent, reason: DiffReasonDegraded, proposal: ProposalReconcile,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			facts := baselineFacts()
			for index := range facts {
				if facts[index].Component == test.component {
					test.mutate(&facts[index])
				}
			}
			output := h.reduceManaged(h.offer(facts...), managedPolicy())

			entry := classFor(output.Diff, test.component)
			if entry.Classification != test.class || entry.Reason != test.reason {
				t.Fatalf("got %q/%q, want %q/%q",
					entry.Classification, entry.Reason, test.class, test.reason)
			}
			if len(output.Proposals) != 1 {
				t.Fatalf("%d proposals, want 1", len(output.Proposals))
			}
			proposal := output.Proposals[0]
			if proposal.Target != test.component || proposal.Class != test.proposal {
				t.Fatalf("proposal %s/%s, want %s/%s",
					proposal.Target, proposal.Class, test.component, test.proposal)
			}
			if err := proposal.Verify(); err != nil {
				t.Fatalf("verify: %v", err)
			}
		})
	}
}

func TestUncertaintyProposesObservationNotChange(t *testing.T) {
	h := newHarness(t)
	h.reduceManaged(h.offer(baselineFacts()...), managedPolicy())

	// Let every deadline pass.
	output, err := Reduce(Input{
		Prior: h.snapshot, Policy: activePolicy(), PolicyComponents: managedPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick + 400,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if len(output.Proposals) != len(connectivity.Components()) {
		t.Fatalf("%d proposals, want one per component", len(output.Proposals))
	}
	for _, proposal := range output.Proposals {
		if proposal.Class != ProposalObserve {
			t.Fatalf("%s proposes %q, want observe", proposal.Target, proposal.Class)
		}
	}
}

// A state a new policy stopped permitting is reported and left alone. This is
// the difference between a read model and a disconnect.
func TestGrandfatheredStateIsReportedButNeverWithdrawn(t *testing.T) {
	h := newHarness(t)
	h.reduceManaged(h.offer(baselineFacts()...), managedPolicy())

	// A newer policy stops asking for user access entirely.
	narrowed := managedPolicy()
	for index := range narrowed {
		if narrowed[index].Component == connectivity.ComponentUserAccess {
			narrowed[index].Managed = false
		}
	}
	newer := activePolicy()
	newer.BundleGeneration++

	output, err := Reduce(Input{
		Prior: h.snapshot, Policy: newer, PolicyComponents: narrowed,
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	entry := classFor(output.Diff, connectivity.ComponentUserAccess)
	if entry.Classification != ClassGrandfathered {
		t.Fatalf("got %q, want grandfathered_noncompliant", entry.Classification)
	}
	for _, proposal := range output.Proposals {
		if proposal.Target == connectivity.ComponentUserAccess {
			t.Fatalf("an established state produced a %q proposal", proposal.Class)
		}
	}
}

// Something policy never asked for, appearing without a prior generation that
// permitted it, is a different case and does get a withdrawal proposal.
func TestNeverPermittedStateIsUnexpected(t *testing.T) {
	h := newHarness(t)
	narrowed := managedPolicy()
	for index := range narrowed {
		if narrowed[index].Component == connectivity.ComponentUserAccess {
			narrowed[index].Managed = false
		}
	}
	output := h.reduceManaged(h.offer(baselineFacts()...), narrowed)

	entry := classFor(output.Diff, connectivity.ComponentUserAccess)
	if entry.Classification != ClassUnexpected {
		t.Fatalf("got %q, want unexpected", entry.Classification)
	}
	found := false
	for _, proposal := range output.Proposals {
		if proposal.Target == connectivity.ComponentUserAccess {
			found = proposal.Class == ProposalWithdraw
		}
	}
	if !found {
		t.Fatal("no withdrawal proposal for a state policy never permitted")
	}
}

func TestProposalIsImmutableAndDigestAddressed(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleDegraded
			facts[index].Reason = connectivity.ReasonProbeFailed
		}
	}
	output := h.reduceManaged(h.offer(facts...), managedPolicy())
	if len(output.Proposals) != 1 {
		t.Fatalf("%d proposals, want 1", len(output.Proposals))
	}
	proposal := output.Proposals[0]

	edits := []struct {
		name   string
		mutate func(*Proposal)
	}{
		{"class", func(p *Proposal) { p.Class = ProposalWithdraw }},
		{"target", func(p *Proposal) { p.Target = connectivity.ComponentRelays }},
		{"domain", func(p *Proposal) { p.Domain = policy.DomainUser }},
		{"generation", func(p *Proposal) { p.SnapshotGeneration++ }},
		{"diff digest", func(p *Proposal) {
			p.DiffDigest = "0000000000000000000000000000000000000000000000000000000000000000"
		}},
	}
	for _, edit := range edits {
		t.Run(edit.name, func(t *testing.T) {
			altered := proposal
			edit.mutate(&altered)
			if err := altered.Verify(); !errors.Is(err, ErrInvalidProposal) {
				t.Fatalf("got %v, want %v", err, ErrInvalidProposal)
			}
		})
	}
}

// A proposal describes one moment. After the state or the policy moves, it can
// only be produced again, never resumed.
func TestStaleProposalCannotBeResumed(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleDegraded
			facts[index].Reason = connectivity.ReasonProbeFailed
		}
	}
	first := h.reduceManaged(h.offer(facts...), managedPolicy())
	proposal := first.Proposals[0]
	firstDigest, _ := first.Diff.Digest()
	if err := proposal.VerifyCurrent(first.Snapshot, firstDigest); err != nil {
		t.Fatalf("a fresh proposal was rejected: %v", err)
	}

	// The component recovers: the snapshot moves on.
	recovered := connectivity.FixtureBaseline(connectivity.ComponentDNS, 30)
	second := h.reduceManaged(h.offer(recovered), managedPolicy())
	secondDigest, _ := second.Diff.Digest()
	if err := proposal.VerifyCurrent(second.Snapshot, secondDigest); !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("got %v, want %v", err, ErrStaleProposal)
	}
}

func TestProposalGoesStaleOnAPolicyChange(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleDegraded
			facts[index].Reason = connectivity.ReasonProbeFailed
		}
	}
	first := h.reduceManaged(h.offer(facts...), managedPolicy())
	proposal := first.Proposals[0]

	newer := activePolicy()
	newer.BundleGeneration++
	second, err := Reduce(Input{
		Prior: h.snapshot, Policy: newer, PolicyComponents: managedPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	digest, _ := second.Diff.Digest()
	if err := proposal.VerifyCurrent(second.Snapshot, digest); !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("got %v, want %v", err, ErrStaleProposal)
	}
}

// The proposal type has no field that could carry a command, a path, an
// endpoint or a credential. This states that as a property of the struct, so
// adding such a field fails here rather than in review.
func TestProposalHasNoFreeFormField(t *testing.T) {
	permitted := map[string]struct{}{
		"Schema": {}, "Version": {}, "SnapshotGeneration": {},
		"BundleGeneration": {}, "DomainPolicyGeneration": {}, "Domain": {},
		"Target": {}, "Class": {}, "Reason": {}, "DiffDigest": {}, "Digest": {},
	}
	structure := reflect.TypeOf(Proposal{})
	for index := 0; index < structure.NumField(); index++ {
		field := structure.Field(index)
		if _, allowed := permitted[field.Name]; !allowed {
			t.Fatalf("proposal gained field %q; a proposal may not describe how", field.Name)
		}
	}

	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentDNS {
			facts[index].Lifecycle = connectivity.LifecycleFailed
			facts[index].Reason = connectivity.ReasonProbeFailed
			facts[index].Payload.DNS = &connectivity.DNSPayload{
				ResolverClass: connectivity.ResolverNone}
		}
	}
	output := h.reduceManaged(h.offer(facts...), managedPolicy())
	encoded, err := json.Marshal(output.Proposals)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic []map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, proposal := range generic {
		for name, value := range proposal {
			text, isString := value.(string)
			if !isString {
				continue
			}
			// Every string is an identifier, an enum member or a hex digest.
			for _, r := range text {
				switch {
				case r >= 'a' && r <= 'z', r >= '0' && r <= '9',
					r == '_', r == '-', r == '.':
				default:
					t.Fatalf("proposal field %q holds free-form text %q", name, text)
				}
			}
		}
	}
}

func TestProposalBindsTheOwningDomainGeneration(t *testing.T) {
	h := newHarness(t)
	facts := baselineFacts()
	for index := range facts {
		if facts[index].Component == connectivity.ComponentUserAccess {
			facts[index].Lifecycle = connectivity.LifecycleFailed
			facts[index].Reason = connectivity.ReasonProbeFailed
			facts[index].Payload.UserAccess = &connectivity.UserAccessPayload{
				ProfileClass: connectivity.ProfileNone}
		}
	}
	output := h.reduceManaged(h.offer(facts...), managedPolicy())
	for _, proposal := range output.Proposals {
		if proposal.Target != connectivity.ComponentUserAccess {
			continue
		}
		if proposal.Domain != policy.DomainUser {
			t.Fatalf("domain %q, want user", proposal.Domain)
		}
		if proposal.DomainPolicyGeneration != activePolicy().UserGeneration {
			t.Fatalf("domain generation %d, want %d",
				proposal.DomainPolicyGeneration, activePolicy().UserGeneration)
		}
	}
}

var _ = control.Tick(0)
