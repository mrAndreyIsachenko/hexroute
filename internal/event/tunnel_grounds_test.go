package event

import (
	"strings"
	"testing"
)

// The grounds are validated on the way in, and the refusals are what keep them
// readable.
//
// Two of these checks were added with nothing driving them, and mutating them
// away left every test passing. The digest check is the one that matters most:
// it is what stops a whole carrier signature — which is made of destinations —
// from being stored in the field that exists so those never are.
func TestGroundsThatCannotBeReadAreRefused(t *testing.T) {
	complete := func() TunnelGrounds {
		entries, planned := 3, uint16(4)
		return TunnelGrounds{
			Complete:       true,
			ProcessRunning: true,
			SleptMS:        1500,
			CarrierDigest:  "0123456789ab",
			CarrierEntries: &entries,
			RoutesPlanned:  &planned,
		}
	}
	for _, item := range []struct {
		name    string
		break_  func(*TunnelGrounds)
		refused bool
	}{
		{"a signature where a digest belongs", func(g *TunnelGrounds) {
			g.CarrierDigest = "198.51.100.20=utun4 203.0.113.20=en0"
		}, true},
		{"a digest longer than one", func(g *TunnelGrounds) {
			g.CarrierDigest = strings.Repeat("a", 64)
		}, true},
		{"a digest that is not hex", func(g *TunnelGrounds) {
			g.CarrierDigest = "zzzzzzzzzzzz"
		}, true},
		{"a complete cycle with no carrier count", func(g *TunnelGrounds) {
			g.CarrierEntries = nil
		}, true},
		{"a complete cycle with no route count", func(g *TunnelGrounds) {
			g.RoutesPlanned = nil
		}, true},
		{"an incomplete cycle reporting what it saw", func(g *TunnelGrounds) {
			g.Complete = false
		}, true},
		{"a machine that slept a negative time", func(g *TunnelGrounds) {
			g.SleptMS = -1
		}, true},
		{"a carrier that was observed and covers nothing", func(g *TunnelGrounds) {
			zero := 0
			g.CarrierEntries = &zero
		}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			grounds := complete()
			item.break_(&grounds)
			decision := TunnelDecision{
				Action: "rebuild_tunnel", Causes: []string{"process_gone"},
				Grounds: &grounds,
			}
			_, err := Encode(SchemaTunnelDecision, decision)
			if item.refused && err == nil {
				t.Fatalf("%s was stored", item.name)
			}
			if !item.refused && err != nil {
				t.Fatalf("%s was refused: %v", item.name, err)
			}
		})
	}
}

// A decision with no grounds at all is still a decision.
//
// Records written before the grounds existed are in the archive, and refusing
// to read them would turn a widened record into a broken store.
func TestADecisionWithoutGroundsIsStillReadable(t *testing.T) {
	if _, err := Encode(SchemaTunnelDecision, TunnelDecision{
		Action: "none",
	}); err != nil {
		t.Fatalf("a decision without grounds was refused: %v", err)
	}
}
