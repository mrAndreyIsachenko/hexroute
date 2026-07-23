package routeplan

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

func testInput() Input {
	physical := Path{
		Link:      safety.LinkPhysical,
		Interface: "en7",
		Gateway:   netip.MustParseAddr("192.0.2.1"),
	}
	tun := Path{
		Link:      safety.LinkTwilightTUN,
		Interface: "utun8",
	}
	return Input{
		Physical: physical,
		TUN:      tun,
		Targets: []Target{
			{Name: "ingress-a", Destination: netip.MustParseAddr("192.0.2.20"), Role: RoleIngress},
			{Name: "corporate-a", Destination: netip.MustParseAddr("198.51.100.10"), Role: RoleCorporate},
			{Name: "gitlab-https", Destination: netip.MustParseAddr("198.51.100.11"), Role: RoleGitLabHTTPS},
			{Name: "gitlab-ssh", Destination: netip.MustParseAddr("198.51.100.12"), Role: RoleGitLabSSH},
			{Name: "codex-a", Destination: netip.MustParseAddr("203.0.113.20"), Role: RoleCodexFallback},
		},
		Codex: CodexState{NormalReady: true, TwilightReady: true},
		Current: map[netip.Addr]ObservedRoute{
			netip.MustParseAddr("192.0.2.20"): {
				Destination: netip.MustParseAddr("192.0.2.20"),
				Interface:   physical.Interface,
				Gateway:     physical.Gateway,
			},
			netip.MustParseAddr("198.51.100.10"): {
				Destination: netip.MustParseAddr("198.51.100.10"),
				Interface:   tun.Interface,
			},
			netip.MustParseAddr("198.51.100.11"): {
				Destination: netip.MustParseAddr("198.51.100.11"),
				Interface:   tun.Interface,
			},
			netip.MustParseAddr("198.51.100.12"): {
				Destination: netip.MustParseAddr("198.51.100.12"),
				Interface:   physical.Interface,
				Gateway:     physical.Gateway,
			},
		},
	}
}

func TestBuildPreservesApprovedScopedRoutePolicy(t *testing.T) {
	input := testInput()

	plan, err := Build(input)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if !plan.ObserveOnly {
		t.Fatal("Build() returned a plan with mutation authority")
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Build() operations = %+v, want none", plan.Operations)
	}
}

func TestBuildPlansOnlyRequiredHostRouteRepairs(t *testing.T) {
	input := testInput()
	delete(input.Current, netip.MustParseAddr("192.0.2.20"))
	input.Current[netip.MustParseAddr("198.51.100.10")] = ObservedRoute{
		Destination: netip.MustParseAddr("198.51.100.10"),
		Interface:   input.Physical.Interface,
		Gateway:     input.Physical.Gateway,
	}
	input.Current[netip.MustParseAddr("198.51.100.12")] = ObservedRoute{
		Destination: netip.MustParseAddr("198.51.100.12"),
		Interface:   input.TUN.Interface,
	}

	plan, err := Build(input)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(plan.Operations) != 3 {
		t.Fatalf("Build() operations = %+v, want 3 repairs", plan.Operations)
	}
	for _, operation := range plan.Operations {
		if operation.Kind != OperationEnsureHostRoute {
			t.Fatalf("unexpected operation: %+v", operation)
		}
		if !operation.Destination.Is4() {
			t.Fatalf("operation is not an IPv4 host route: %+v", operation)
		}
		switch operation.Role {
		case RoleIngress, RoleGitLabSSH:
			if operation.Path.Interface != input.Physical.Interface {
				t.Fatalf("physical role captured by TUN: %+v", operation)
			}
		case RoleCorporate:
			if operation.Path.Interface != input.TUN.Interface {
				t.Fatalf("corporate role not assigned to TUN: %+v", operation)
			}
		default:
			t.Fatalf("unexpected repaired role: %+v", operation)
		}
	}
}

func TestBuildActivatesAndRestoresScopedCodexFallback(t *testing.T) {
	input := testInput()
	codexAddress := netip.MustParseAddr("203.0.113.20")
	input.Codex.NormalReady = false

	active, err := Build(input)
	if err != nil {
		t.Fatalf("Build(fallback) error: %v", err)
	}
	if len(active.Operations) != 1 {
		t.Fatalf("fallback operations = %+v, want 1", active.Operations)
	}
	operation := active.Operations[0]
	if operation.Role != RoleCodexFallback ||
		operation.Kind != OperationEnsureHostRoute ||
		operation.Destination != codexAddress ||
		operation.Path.Interface != input.TUN.Interface ||
		operation.Reason != ReasonFallbackRequired {
		t.Fatalf("fallback operation = %+v", operation)
	}

	input.Codex.NormalReady = true
	input.Current[codexAddress] = ObservedRoute{
		Destination: codexAddress,
		Interface:   input.TUN.Interface,
		Owned:       true,
	}
	restored, err := Build(input)
	if err != nil {
		t.Fatalf("Build(restored) error: %v", err)
	}
	if len(restored.Operations) != 1 ||
		restored.Operations[0].Kind != OperationRemoveOwnedHostRoute ||
		restored.Operations[0].Reason != ReasonFallbackRestored {
		t.Fatalf("restored operations = %+v", restored.Operations)
	}

	input.Current[codexAddress] = ObservedRoute{
		Destination: codexAddress,
		Interface:   input.TUN.Interface,
		Owned:       false,
	}
	unowned, err := Build(input)
	if err != nil {
		t.Fatalf("Build(unowned) error: %v", err)
	}
	if len(unowned.Operations) != 0 {
		t.Fatalf("planner removed an unowned route: %+v", unowned.Operations)
	}
}

func TestBuildDoesNotActivateFallbackWithoutVerifiedTwilight(t *testing.T) {
	input := testInput()
	input.Codex = CodexState{}

	plan, err := Build(input)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("unverified fallback operations = %+v", plan.Operations)
	}
}

func TestBuildRejectsIngressSelfRoute(t *testing.T) {
	input := testInput()
	input.Physical = Path{
		Link:      safety.LinkTwilightTUN,
		Interface: input.TUN.Interface,
	}

	_, err := Build(input)
	if !errors.Is(err, safety.ErrIngressSelfRoute) {
		t.Fatalf("Build() error = %v, want %v", err, safety.ErrIngressSelfRoute)
	}
}

func TestBuildRejectsConflictingRolesForOneDestination(t *testing.T) {
	input := testInput()
	input.Targets = append(input.Targets, Target{
		Name:        "conflicting-ssh",
		Destination: netip.MustParseAddr("198.51.100.11"),
		Role:        RoleGitLabSSH,
	})

	_, err := Build(input)
	if !errors.Is(err, ErrAmbiguousOwnership) {
		t.Fatalf("Build() error = %v, want %v", err, ErrAmbiguousOwnership)
	}
}

func TestBuildKeepsPreferredIngressOnIndependentUpstream(t *testing.T) {
	input := testInput()
	upstream := Path{
		Link:      safety.LinkUpstreamVPN,
		Interface: "utun3",
	}
	input.Upstream = &upstream
	input.Targets[0].Preferred = safety.LinkUpstreamVPN
	delete(input.Current, input.Targets[0].Destination)

	plan, err := Build(input)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("Build() operations = %+v, want one ingress repair", plan.Operations)
	}
	if operation := plan.Operations[0]; operation.Path.Link != safety.LinkUpstreamVPN ||
		operation.Path.Interface != "utun3" {
		t.Fatalf("upstream ingress operation = %+v", operation)
	}
}

func TestBuildRejectsTwilightTUNAsUpstreamCarrier(t *testing.T) {
	input := testInput()
	input.Upstream = &Path{
		Link:      safety.LinkUpstreamVPN,
		Interface: input.TUN.Interface,
	}
	input.Targets[0].Preferred = safety.LinkUpstreamVPN

	_, err := Build(input)
	if !errors.Is(err, safety.ErrIngressSelfRoute) {
		t.Fatalf("Build() error = %v, want %v", err, safety.ErrIngressSelfRoute)
	}
}
