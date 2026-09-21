package rootdaemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// A recorded decision carries its grounds and none of its addresses.
//
// The signature here is the shape the machine produces — several real
// destinations against the interfaces carrying them — rather than a fixture
// with one entry. A fixture that hid the shape is how the carrier signature's
// own defect survived two mutations once already.
func TestARecordedDecisionCarriesGroundsAndNoDestinations(t *testing.T) {
	carried := []tunnelplan.CarriedDestination{
		{Destination: "198.51.100.20", Interface: "utun4"},
		{Destination: "203.0.113.20", Interface: "en0"},
		{Destination: "192.0.2.7", Interface: "utun4"},
	}
	plan := tunnelplan.Plan{
		Action: "rebuild_tunnel",
		Causes: []tunnelplan.Cause{"link_returned", "routes_drifted"},
		Grounds: tunnelplan.Grounds{
			Complete:       true,
			ProcessRunning: true,
			Slept:          1500 * time.Millisecond,
			Carrier:        tunnelplan.NewSignature(carried),
			CarrierEntries: len(carried),
			LinkPresent:    true,
			LinkFailures:   0,
			PayloadOK:      true,
			RoutesDrifted:  true,
		},
	}
	record := tunnelDecisionRecord(plan, 4, false, nil, 7)

	if record.Grounds == nil {
		t.Fatal("the decision was recorded without its grounds")
	}
	if record.Grounds.SleptMS != 1500 {
		t.Fatalf("slept_ms = %d, want 1500", record.Grounds.SleptMS)
	}
	if record.Grounds.CarrierEntries == nil || *record.Grounds.CarrierEntries != 3 {
		t.Fatalf("carrier_entries = %v, want 3", record.Grounds.CarrierEntries)
	}
	if record.Grounds.RoutesPlanned == nil || *record.Grounds.RoutesPlanned != 4 {
		t.Fatalf("routes_planned = %v, want 4", record.Grounds.RoutesPlanned)
	}
	if len(record.Grounds.CarrierDigest) != 12 {
		t.Fatalf("carrier_digest = %q, want twelve hex characters",
			record.Grounds.CarrierDigest)
	}

	// The whole record, as it would be stored.
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{
		"198.51.100.20", "203.0.113.20", "192.0.2.7", "utun4", "en0",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("the record carries %q: %s", secret, encoded)
		}
	}

	// And it must survive the path that actually stores it. Checking a copy of
	// the rule beside the code would not: a payload its own validation refused
	// reached the machine that way once before.
	if _, err := event.Encode(event.SchemaTunnelDecision, record); err != nil {
		t.Fatalf("the record this runtime writes is refused on the path that "+
			"stores it: %v", err)
	}
}

// A cycle that saw nothing reports nothing about what it saw.
func TestAnIncompleteCycleRecordsNoCarrierOrRoutes(t *testing.T) {
	record := tunnelDecisionRecord(tunnelplan.Plan{
		Action:  "rebuild_tunnel",
		Causes:  []tunnelplan.Cause{"process_gone"},
		Grounds: tunnelplan.Grounds{Complete: false, ProcessRunning: false},
	}, 0, false, nil, 7)
	if record.Grounds.CarrierEntries != nil {
		t.Fatalf("an incomplete cycle reported %d carrier entries",
			*record.Grounds.CarrierEntries)
	}
	if record.Grounds.RoutesPlanned != nil {
		t.Fatalf("an incomplete cycle reported %d planned routes",
			*record.Grounds.RoutesPlanned)
	}
	if record.Grounds.CarrierDigest != "" {
		t.Fatalf("an incomplete cycle reported a carrier digest %q",
			record.Grounds.CarrierDigest)
	}
	if _, err := event.Encode(event.SchemaTunnelDecision, record); err != nil {
		t.Fatalf("a record from an incomplete cycle is refused: %v", err)
	}
}
