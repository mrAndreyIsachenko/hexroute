package rootdaemon

import (
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// The record carries the tick gap the wake cause was compared on.
//
// A disagreement about a wake has to be readable from the record that made it;
// without the gap, the only way to see why a wake was or was not named is a
// second store laid beside the decisions, which is what the grounds exist to end.
func TestTheRecordCarriesTheTickGap(t *testing.T) {
	plan := tunnelplan.Plan{
		Action: tunnelplan.ActionRebuildTunnel,
		Causes: []tunnelplan.Cause{tunnelplan.CauseWakeGap},
		Grounds: tunnelplan.Grounds{
			Complete: true, ProcessRunning: true,
			Slept: 130 * time.Second, TickGap: 190 * time.Second,
		},
	}
	record := tunnelDecisionRecord(plan, 0, nil, 7)
	if record.Grounds == nil || record.Grounds.TickGapMS == nil {
		t.Fatal("the record carries no tick gap")
	}
	if *record.Grounds.TickGapMS != 190000 {
		t.Fatalf("tick gap recorded %d ms, want 190000", *record.Grounds.TickGapMS)
	}
}

// The record says when the process was replaced rather than absent.
func TestTheRecordCarriesAReplacedProcess(t *testing.T) {
	plan := tunnelplan.Plan{
		Action: tunnelplan.ActionRebuildTunnel,
		Causes: []tunnelplan.Cause{tunnelplan.CauseProcessGone},
		Grounds: tunnelplan.Grounds{
			Complete: true, ProcessObserved: true, ProcessRunning: true,
			ProcessReplaced: true, TickGap: time.Minute,
		},
	}
	record := tunnelDecisionRecord(plan, 0, nil, 7)
	if record.Grounds == nil || !record.Grounds.ProcessReplaced {
		t.Fatalf("the record does not say the process was replaced: %+v", record.Grounds)
	}
	// And that the cycle looked at all: without it a record cannot tell a cycle
	// that saw no tunnel from one that never looked.
	if !record.Grounds.ProcessObserved {
		t.Fatalf("the record does not say the process was observed: %+v", record.Grounds)
	}
}
