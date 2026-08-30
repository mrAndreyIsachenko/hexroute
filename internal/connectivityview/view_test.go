package connectivityview

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

const evaluationTick = control.Tick(1100)

func activePolicy() connectivityreduce.PolicyDescriptor {
	return connectivityreduce.PolicyDescriptor{
		Present: true, Valid: true,
		BundleGeneration: 7, RootGeneration: 3, UserGeneration: 2,
		ManifestDigest: "b8f1c0d2e3a4956677889900aabbccddeeff00112233445566778899aabbccdd",
	}
}

func u16(value uint16) *uint16 { return &value }

func managedPolicy() []connectivityreduce.ComponentPolicy {
	path := connectivity.PathTunneled
	resolver := connectivity.ResolverScoped
	selected := connectivity.SelectedPrimary
	profile := connectivity.ProfileConfigured
	return []connectivityreduce.ComponentPolicy{
		{Component: connectivity.ComponentPhysicalNetwork, Managed: true,
			Expect: connectivityreduce.Expectation{Lifecycle: connectivity.LifecycleReady}},
		{Component: connectivity.ComponentDefaultPath, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, PathClass: &path}},
		{Component: connectivity.ComponentDNS, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, ResolverClass: &resolver}},
		{Component: connectivity.ComponentScopedRoutes, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, MinInstalledRoutes: u16(7)}},
		{Component: connectivity.ComponentTransports, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, MinReadyTransports: u16(3)}},
		{Component: connectivity.ComponentRelays, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, SelectedClass: &selected}},
		{Component: connectivity.ComponentUserAccess, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady, ProfileClass: &profile}},
		{Component: connectivity.ComponentSessionExpiry, Managed: true,
			Expect: connectivityreduce.Expectation{Lifecycle: connectivity.LifecycleReady}},
	}
}

// reduced builds a real reduction, optionally breaking one component so the
// views have something other than a uniformly healthy host to render.
func reduced(t *testing.T, breakComponent connectivity.Component) connectivityreduce.Output {
	t.Helper()
	acceptor := connectivityaccept.New()
	events := make([]connectivityreduce.Event, 0)
	for index, component := range connectivity.Components() {
		fact := connectivity.FixtureBaseline(component, uint64(index+1))
		if component == breakComponent {
			fact.Lifecycle = connectivity.LifecycleDegraded
			fact.Reason = connectivity.ReasonProbeFailed
		}
		acceptance, err := acceptor.Accept(fact, fact.Domain)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		events = append(events, connectivityreduce.Event{Acceptance: acceptance, Fact: fact})
	}
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Events: events, Policy: activePolicy(), PolicyComponents: managedPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	return output
}

// The operator keeps every component beside the aggregate, with the reason it
// is in that state and what its divergence would need.
func TestLocalViewKeepsEveryComponentBesideTheAggregate(t *testing.T) {
	output := reduced(t, connectivity.ComponentDNS)
	status := Local(output.Snapshot, output.Diff, output.Proposals)

	if len(status.Components) != len(connectivity.Components()) {
		t.Fatalf("rendered %d components, want %d",
			len(status.Components), len(connectivity.Components()))
	}
	if status.Aggregate != connectivityreduce.AggregateDegraded {
		t.Fatalf("aggregate %q, want degraded", status.Aggregate)
	}
	var dns LocalComponent
	for _, component := range status.Components {
		if component.Component == connectivity.ComponentDNS {
			dns = component
		}
	}
	if dns.State != connectivityreduce.StateDegraded {
		t.Fatalf("dns state %q", dns.State)
	}
	if dns.Classification != connectivityreduce.ClassDivergent {
		t.Fatalf("dns classification %q", dns.Classification)
	}
	if dns.ProposalClass != connectivityreduce.ProposalReconcile {
		t.Fatalf("dns proposal class %q", dns.ProposalClass)
	}
	if dns.DeadlineIn <= 0 {
		t.Fatal("a fresh component reported no headroom")
	}
	if len(status.ProposalClasses) == 0 {
		t.Fatal("the operator view lost the proposal summary")
	}
}

func TestLocalViewShowsGapsAndConflicts(t *testing.T) {
	acceptor := connectivityaccept.New()
	events := make([]connectivityreduce.Event, 0)
	first := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	skipped := connectivity.FixtureBaseline(connectivity.ComponentDNS, 5)
	skipped.Baseline = false
	skipped.Reason = connectivity.ReasonProbeSucceeded
	for _, fact := range []connectivity.Fact{first, skipped} {
		acceptance, err := acceptor.Accept(fact, fact.Domain)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		events = append(events, connectivityreduce.Event{Acceptance: acceptance, Fact: fact})
	}
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Events: events, Policy: activePolicy(), PolicyComponents: managedPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	status := Local(output.Snapshot, output.Diff, output.Proposals)
	if status.OpenGaps != 1 {
		t.Fatalf("open gaps %d, want 1", status.OpenGaps)
	}
	found := false
	for _, source := range status.Sources {
		if len(source.Gaps) > 0 && source.AwaitingBaseline {
			found = true
		}
	}
	if !found {
		t.Fatal("the operator view lost the source integrity detail")
	}
}

// The projection is an allowlist. Anything that could name a host, a route, a
// process, an event or a decision must not be expressible in it.
func TestProjectionCarriesNoIdentifyingField(t *testing.T) {
	output := reduced(t, connectivity.ComponentRelays)
	projection := Project(output.Snapshot, output.Diff, output.Proposals)

	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	permitted := map[string]struct{}{
		"snapshot_generation": {}, "reducer_version": {},
		"bundle_generation": {}, "root_generation": {}, "user_generation": {},
		"aggregate": {}, "authorization": {}, "authorization_reason": {},
		"components": {}, "open_gaps": {}, "gap_overflow": {},
		"source_conflicts": {}, "proposal_classes": {},
	}
	for name := range generic {
		if _, ok := permitted[name]; !ok {
			t.Fatalf("the projection gained field %q", name)
		}
	}

	// Nothing in the encoded form may look like an address, a path, a digest
	// or a source identity.
	text := string(encoded)
	for _, forbidden := range []string{
		"98.91", "192.168", "10.0", "utun", "en0",
		"/", "root.", "user.", "boot-", "@",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the projection carries %q: %s", forbidden, text)
		}
	}
	// A 64 character hex run is a digest; none belongs here.
	for _, field := range strings.Split(text, `"`) {
		if len(field) != 64 {
			continue
		}
		hex := true
		for _, r := range field {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				hex = false
				break
			}
		}
		if hex {
			t.Fatalf("the projection carries a digest: %s", field)
		}
	}
}

// The projection must be buildable as a real event, so its bounds are the
// event schema's bounds and not a second opinion.
func TestProjectionEncodesUnderItsSchema(t *testing.T) {
	output := reduced(t, connectivity.ComponentDNS)
	projection := Project(output.Snapshot, output.Diff, output.Proposals)

	encoded, err := event.Encode(event.SchemaConnectivityProjection, projection)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	record, err := event.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if record.Priority != event.PriorityOperational {
		t.Fatalf("priority %q, want operational", record.Priority)
	}
	carried, ok := record.Payload.(*event.ConnectivityProjection)
	if !ok {
		t.Fatal("the decoded payload is not a projection")
	}
	if !reflect.DeepEqual(carried.Components, projection.Components) {
		t.Fatal("the projection changed on the wire")
	}
}

// The projection is built from the snapshot, not from the operator view, so a
// field added to the local view cannot reach the cloud by being carried along.
func TestProjectionIsNarrowerThanTheLocalView(t *testing.T) {
	output := reduced(t, connectivity.ComponentDNS)
	local := Local(output.Snapshot, output.Diff, output.Proposals)
	projection := Project(output.Snapshot, output.Diff, output.Proposals)

	localEncoded, err := json.Marshal(local)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The operator view legitimately carries source identities and payloads;
	// the projection must not, and the two are built independently.
	if !strings.Contains(string(localEncoded), "root.") {
		t.Fatal("the operator view lost the source identity it is meant to show")
	}
	projected, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(projected), "root.") {
		t.Fatal("a source identity reached the projection")
	}
	if len(projected) >= len(localEncoded) {
		t.Fatalf("the projection (%d bytes) is not narrower than the local view (%d bytes)",
			len(projected), len(localEncoded))
	}
}

func TestUnauthorizedProjectionStillReportsObservations(t *testing.T) {
	acceptor := connectivityaccept.New()
	events := make([]connectivityreduce.Event, 0)
	for index, component := range connectivity.Components() {
		fact := connectivity.FixtureBaseline(component, uint64(index+1))
		acceptance, err := acceptor.Accept(fact, fact.Domain)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		events = append(events, connectivityreduce.Event{Acceptance: acceptance, Fact: fact})
	}
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Events: events, Policy: connectivityreduce.PolicyDescriptor{},
		BootID: connectivity.FixtureBootID, EvaluationTick: evaluationTick,
	})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	projection := Project(output.Snapshot, output.Diff, output.Proposals)
	if projection.Authorization != string(connectivityreduce.AuthorizationUnauthorized) {
		t.Fatalf("authorization %q", projection.Authorization)
	}
	if len(projection.Components) != len(connectivity.Components()) {
		t.Fatal("an unauthorized projection dropped its observations")
	}
	if len(projection.ProposalClasses) != 0 {
		t.Fatal("an unauthorized projection carried proposals")
	}
}
