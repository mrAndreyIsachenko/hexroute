package event

import (
	"encoding/json"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
)

func journalRecord(t *testing.T, fact connectivity.Fact, hostSequence uint64) (Schema, ConnectivityFact) {
	t.Helper()
	schema, record, err := CanonicalConnectivityRecord(fact, hostSequence, hostSequence, "accepted", "authoritative")
	if err != nil {
		t.Fatalf("build record: %v", err)
	}
	return schema, record
}

func TestConnectivityRecordsRoundTrip(t *testing.T) {
	for index, fact := range connectivity.FixtureBaselineSet() {
		schema, record := journalRecord(t, fact, uint64(index+1))
		encoded, err := Encode(schema, record)
		if err != nil {
			t.Fatalf("encode %s: %v", fact.Component, err)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", fact.Component, err)
		}
		carried, ok := asConnectivityFact(decoded.Payload)
		if !ok {
			t.Fatalf("%s: payload is not a connectivity record", fact.Component)
		}
		if !sameCanonicalFact(carried, record) {
			t.Fatalf("%s: the journalled fact changed", fact.Component)
		}
		restored, err := DecodeConnectivityFact(carried)
		if err != nil {
			t.Fatalf("%s: %v", fact.Component, err)
		}
		if restored.SourceSequence != fact.SourceSequence {
			t.Fatalf("%s: sequence %d, want %d",
				fact.Component, restored.SourceSequence, fact.SourceSequence)
		}
	}
}

// A baseline is what clears a gap, so it must outrank an ordinary observation
// when the journal has to free space.
func TestBaselineOutranksObservationForRetention(t *testing.T) {
	baseline, ok := DefinitionFor(SchemaConnectivityBaseline)
	if !ok {
		t.Fatal("baseline schema is not registered")
	}
	observation, ok := DefinitionFor(SchemaConnectivityObservation)
	if !ok {
		t.Fatal("observation schema is not registered")
	}
	if baseline.Priority != PriorityCritical {
		t.Fatalf("baseline priority %q, want critical", baseline.Priority)
	}
	if observation.Priority != PriorityOperational {
		t.Fatalf("observation priority %q, want operational", observation.Priority)
	}
}

// The mirrored identity exists so a record can be handled without decoding the
// fact. It must never be able to say something the fact does not.
func TestMirroredIdentityCannotDriftFromTheFact(t *testing.T) {
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	_, valid := journalRecord(t, fact, 5)

	tests := []struct {
		name   string
		mutate func(*ConnectivityFact)
	}{
		{"component", func(r *ConnectivityFact) { r.Component = connectivity.ComponentRelays }},
		{"source", func(r *ConnectivityFact) { r.SourceID = "root.routes" }},
		{"boot", func(r *ConnectivityFact) { r.BootID = "boot-9999999999999999" }},
		{"sequence", func(r *ConnectivityFact) { r.SourceSequence = 99 }},
		{"baseline flag", func(r *ConnectivityFact) { r.Baseline = false }},
		{"digest", func(r *ConnectivityFact) {
			r.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
		}},
		{"an accepted record with no place in the accepted order",
			func(r *ConnectivityFact) { r.HostSequence = 0 }},
		{"a refused record claiming a place in the accepted order",
			func(r *ConnectivityFact) { r.Outcome = "conflict" }},
		{"fold position", func(r *ConnectivityFact) { r.FoldPosition = 0 }},
		{"role", func(r *ConnectivityFact) { r.Role = "owner" }},
		{"fact body", func(r *ConnectivityFact) {
			r.Fact = json.RawMessage(`{"schema":"hexroute.connectivity-fact.v1"}`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			test.mutate(&record)
			if _, err := Encode(SchemaConnectivityBaseline, record); err == nil {
				t.Fatal("a record disagreeing with its fact was accepted")
			}
		})
	}
}

// Filing an observation under the baseline schema would give it a retention
// priority it has not earned.
func TestRecordCannotBeFiledUnderTheWrongSchema(t *testing.T) {
	baseline := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	_, record := journalRecord(t, baseline, 1)
	if _, err := Encode(SchemaConnectivityObservation, record); err == nil {
		t.Fatal("a baseline was accepted as an ordinary observation")
	}

	ordinary := connectivity.FixtureBaseline(connectivity.ComponentDNS, 2)
	ordinary.Baseline = false
	ordinary.Reason = connectivity.ReasonProbeSucceeded
	_, plain, err := CanonicalConnectivityRecord(ordinary, 2, 2, "accepted", "authoritative")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := Encode(SchemaConnectivityBaseline, plain); err == nil {
		t.Fatal("an observation was accepted as a baseline")
	}
}
