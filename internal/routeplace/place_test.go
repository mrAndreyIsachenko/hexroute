package routeplace_test

import (
	"net/netip"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/routeplace"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

const (
	physicalInterface = "en7"
	upstreamInterface = "utun5"
	tunnelInterface   = "utun8"
)

func address(last int) netip.Addr {
	return netip.MustParseAddr("192.0.2." + itoa(last))
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// machineShape is this host's configuration as read on 2026-10-08: 2 ingress on
// two different links, 7 on the tunnel, and 12 fallback routes the
// configuration asks for nowhere while normal Codex is reachable.
func machineShape() routeplace.Input {
	targets := []routeplace.Target{
		{Name: "ingress-01", Destination: address(10), Role: routeplace.RoleIngress, Preferred: safety.LinkPhysical},
		{Name: "ingress-02", Destination: address(11), Role: routeplace.RoleIngress, Preferred: safety.LinkUpstreamVPN},
		{Name: "corporate-01", Destination: address(20), Role: routeplace.RoleCorporate},
		{Name: "gitlab-https", Destination: address(21), Role: routeplace.RoleGitLabHTTPS},
	}
	for index := 0; index < 5; index++ {
		targets = append(targets, routeplace.Target{
			Name:        "inherited-0" + itoa(index+1),
			Destination: address(30 + index),
			Role:        routeplace.RoleInherited,
		})
	}
	for index := 0; index < 12; index++ {
		targets = append(targets, routeplace.Target{
			Name:        "codex-fallback-" + itoa(index+1),
			Destination: address(40 + index),
			Role:        routeplace.RoleCodexFallback,
		})
	}
	return routeplace.Input{
		Targets: targets,
		Physical: routeplace.Path{
			Link:      safety.LinkPhysical,
			Interface: physicalInterface,
			Gateway:   netip.MustParseAddr("192.0.2.1"),
		},
		Upstream: &routeplace.Path{
			Link:      safety.LinkUpstreamVPN,
			Interface: upstreamInterface,
		},
		TUN: routeplace.Path{
			Link:      safety.LinkTwilightTUN,
			Interface: tunnelInterface,
		},
		Current: map[netip.Addr]routeplace.ObservedRoute{},
		// Normal Codex reachable, so the configuration asks for no fallback
		// route at all.
		Codex: routeplace.CodexState{NormalReady: true},
	}
}

// placed puts a route on an interface. The physical link is reached through a
// gateway, and a route on the right interface through another gateway is as
// misplaced as one on the wrong interface — so the gateway is set with it.
func placed(input routeplace.Input, destination netip.Addr, iface string) {
	route := routeplace.ObservedRoute{Destination: destination, Interface: iface}
	if iface == physicalInterface {
		route.Gateway = input.Physical.Gateway
	}
	input.Current[destination] = route
}

// Every role is sent to the link its own configuration asks for, and the
// ingress roles are never sent to the tunnel.
func TestEachRoleIsSentToTheLinkItAsksFor(t *testing.T) {
	decisions, err := routeplace.Decide(machineShape())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	wanted := map[routeplace.Role]string{
		routeplace.RoleCorporate:   tunnelInterface,
		routeplace.RoleGitLabHTTPS: tunnelInterface,
		routeplace.RoleInherited:   tunnelInterface,
	}
	asked := 0
	for _, decision := range decisions {
		switch decision.Target.Role {
		case routeplace.RoleIngress:
			asked++
			if decision.Path.Interface == tunnelInterface {
				t.Fatalf("%s was sent to the tunnel", decision.Target.Name)
			}
			switch decision.Target.Preferred {
			case safety.LinkPhysical:
				if decision.Path.Interface != physicalInterface {
					t.Fatalf("%s went to %q", decision.Target.Name, decision.Path.Interface)
				}
			case safety.LinkUpstreamVPN:
				if decision.Path.Interface != upstreamInterface {
					t.Fatalf("%s went to %q", decision.Target.Name, decision.Path.Interface)
				}
			}
		case routeplace.RoleCodexFallback:
			if decision.Active {
				t.Fatalf("%s was asked for while normal Codex is reachable", decision.Target.Name)
			}
		default:
			asked++
			if decision.Path.Interface != wanted[decision.Target.Role] {
				t.Fatalf("%s went to %q, want %q",
					decision.Target.Name, decision.Path.Interface, wanted[decision.Target.Role])
			}
		}
	}
	if asked != 9 {
		t.Fatalf("the configuration asks for %d routes, want 9", asked)
	}
}

// Ready is reachable for a configuration whose roles span three links. The old
// condition required all 21 declared routes on one interface.
func TestReadyIsReachableWhenEveryAskedRouteIsWhereItBelongs(t *testing.T) {
	input := machineShape()
	placed(input, address(10), physicalInterface)
	placed(input, address(11), upstreamInterface)
	placed(input, address(20), tunnelInterface)
	placed(input, address(21), tunnelInterface)
	for index := 0; index < 5; index++ {
		placed(input, address(30+index), tunnelInterface)
	}

	placement, err := routeplace.Place(input)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placement.Configured != 21 {
		t.Fatalf("configured = %d, want 21", placement.Configured)
	}
	if placement.Installed != 9 || placement.Conflicting != 0 || placement.Missing != 0 {
		t.Fatalf("placement = %+v, want installed 9 and nothing else", placement)
	}
}

// The live arrangement: the two ingress routes stand on each other's links.
// That is the tunnel owner's arrangement, and it is two routes out of
// twenty-one rather than the fourteen the old fact reported.
func TestTheLiveArrangementCountsTwoConflictsAndNotFourteen(t *testing.T) {
	input := machineShape()
	placed(input, address(10), upstreamInterface) // asks for physical
	placed(input, address(11), physicalInterface) // asks for upstream
	placed(input, address(20), tunnelInterface)
	placed(input, address(21), tunnelInterface)
	for index := 0; index < 5; index++ {
		placed(input, address(30+index), tunnelInterface)
	}
	// And the twelve fallback routes the host carries and the configuration
	// asks for nowhere. None of them is this runtime's to remove.
	for index := 0; index < 12; index++ {
		placed(input, address(40+index), physicalInterface)
	}

	placement, err := routeplace.Place(input)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placement.Conflicting != 2 {
		t.Fatalf("conflicting = %d, want 2", placement.Conflicting)
	}
	if placement.Installed != 7 {
		t.Fatalf("installed = %d, want 7", placement.Installed)
	}
	if placement.Missing != 0 {
		t.Fatalf("missing = %d, want 0", placement.Missing)
	}
}

// A route the configuration asks for nowhere is counted as neither present nor
// diverging, even when the host has it.
func TestARouteAskedForNowhereIsCountedAsNeither(t *testing.T) {
	input := machineShape()
	for index := 0; index < 12; index++ {
		placed(input, address(40+index), physicalInterface)
	}
	placement, err := routeplace.Place(input)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	// Nine asked for and none of them present: all nine missing, and not one of
	// the twelve counted.
	if placement.Missing != 9 || placement.Conflicting != 0 || placement.Installed != 0 {
		t.Fatalf("placement = %+v, want missing 9 and nothing else", placement)
	}
}

// Absent and misplaced are different states and are counted apart.
func TestAbsentIsCountedApartFromMisplaced(t *testing.T) {
	input := machineShape()
	placed(input, address(10), upstreamInterface) // asked for, wrong link
	placed(input, address(20), tunnelInterface)   // asked for, right link
	// address(11) and the rest are asked for and absent.
	placement, err := routeplace.Place(input)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placement.Conflicting != 1 {
		t.Fatalf("conflicting = %d, want 1", placement.Conflicting)
	}
	if placement.Installed != 1 {
		t.Fatalf("installed = %d, want 1", placement.Installed)
	}
	if placement.Missing != 7 {
		t.Fatalf("missing = %d, want 7", placement.Missing)
	}
}

// A fallback route the configuration no longer asks for, that this runtime owns,
// is present and unwanted: a route the host has and should not have as it is.
func TestAnUnwantedOwnedRouteIsCountedAsConflicting(t *testing.T) {
	input := machineShape()
	input.Current[address(40)] = routeplace.ObservedRoute{
		Destination: address(40),
		Interface:   tunnelInterface,
		Owned:       true,
	}
	placement, err := routeplace.Place(input)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placement.Conflicting != 1 {
		t.Fatalf("conflicting = %d, want 1", placement.Conflicting)
	}
	// The other eleven are not ours, so they are not counted at all.
	if placement.Installed != 0 || placement.Missing != 9 {
		t.Fatalf("placement = %+v", placement)
	}
}
