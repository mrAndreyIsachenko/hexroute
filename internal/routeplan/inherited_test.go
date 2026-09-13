package routeplan

import (
	"net/netip"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// A destination inherited from the previous owner goes through the tunnel.
//
// Five of them exist on this machine. They are in the supervisor's environment
// and in no repository's history, no variable names them, none resolves from
// one, and four of the five are CDN anycast — one service behind four edge
// addresses, which cannot be attributed from a host because that address serves
// every customer the CDN has.
//
// They are carried across the handover unchanged, because the handover changes
// who owns the tunnel and nothing about what it carries. Changing both at once
// would make any breakage afterwards impossible to attribute.
func TestAnInheritedDestinationGoesThroughTheTunnel(t *testing.T) {
	input := inheritedFixture(t)
	plan, err := Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, operation := range plan.Operations {
		if operation.Role != RoleInherited {
			continue
		}
		found = true
		if operation.Path.Link != safety.LinkTwilightTUN {
			t.Fatalf("an inherited destination was planned onto %q, want the tunnel",
				operation.Path.Link)
		}
	}
	if !found {
		t.Fatal("the inherited destination was planned no route at all")
	}
}

// It takes no preference, because it has none to express.
//
// Saying "prefer the physical link" about a destination whose purpose is
// unrecorded would be a claim nobody can support. The roles whose path is fixed
// already refuse a preference; this one joins them.
func TestAnInheritedDestinationRefusesAPreference(t *testing.T) {
	input := inheritedFixture(t)
	for index := range input.Targets {
		if input.Targets[index].Role == RoleInherited {
			input.Targets[index].Preferred = safety.LinkPhysical
		}
	}
	if _, err := Build(input); err == nil {
		t.Fatal("an inherited destination accepted a preference")
	}
}

func inheritedFixture(t *testing.T) Input {
	t.Helper()
	address := func(value string) netip.Addr {
		parsed, err := netip.ParseAddr(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	return Input{
		Targets: []Target{{
			Name:        "inherited-one",
			Destination: address("198.51.100.77"),
			Role:        RoleInherited,
		}},
		Physical: Path{
			Link:      safety.LinkPhysical,
			Interface: "en0",
			Gateway:   address("192.0.2.1"),
		},
		TUN: Path{
			Link:      safety.LinkTwilightTUN,
			Interface: "utun15",
		},
	}
}
