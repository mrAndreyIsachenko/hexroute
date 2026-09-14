package event

import "testing"

func tickGapDecision(gap *int64) TunnelDecision {
	entries := 3
	planned := uint16(0)
	return TunnelDecision{
		Action: "rebuild_tunnel",
		Causes: []string{"wake_gap"},
		Grounds: &TunnelGrounds{
			Complete: true, ProcessRunning: true, SleptMS: 130000,
			TickGapMS: gap, CarrierEntries: &entries, RoutesPlanned: &planned,
		},
	}
}

// A decision carries the tick gap its wake cause was compared on.
func TestADecisionCarriesItsTickGap(t *testing.T) {
	gap := int64(190000)
	if !validTunnelDecision(tickGapDecision(&gap)) {
		t.Fatal("a decision carrying its tick gap was refused")
	}
}

// A record written before the tick gap was carried is still read.
//
// The archive holds thousands of them, and a reader that refused them would
// make the history before this change unreadable.
func TestARecordWithoutATickGapIsStillRead(t *testing.T) {
	if !validTunnelDecision(tickGapDecision(nil)) {
		t.Fatal("a record without a tick gap was refused")
	}
}

func TestANegativeTickGapIsRefused(t *testing.T) {
	gap := int64(-1)
	if validTunnelDecision(tickGapDecision(&gap)) {
		t.Fatal("a negative tick gap was accepted")
	}
}
