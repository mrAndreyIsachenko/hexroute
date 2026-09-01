package cloudconnectivity

import (
	"reflect"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// The store's own tests need PostgreSQL and skip without it, so until this
// file the whole package contributed nothing to a run of the gate. What is
// checkable without a database is the part that decides what an operator is
// shown, and the part that decides what may be stored at all.

var reference = time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

// A cloud row rendered as current when it is not is the one way telemetry
// could mislead an operator about a host they cannot reach. Every uncertain
// answer here therefore has to be "stale".
func TestStaleFailsClosed(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		snapshot Snapshot
		after    time.Duration
		want     bool
	}{
		{
			name:     "never observed",
			snapshot: Snapshot{},
			after:    time.Hour,
			want:     true,
		},
		{
			name:     "no freshness expected",
			snapshot: Snapshot{ObservedAt: reference},
			after:    0,
			want:     true,
		},
		{
			name:     "negative freshness",
			snapshot: Snapshot{ObservedAt: reference},
			after:    -time.Hour,
			want:     true,
		},
		{
			name:     "inside the window",
			snapshot: Snapshot{ObservedAt: reference.Add(-30 * time.Minute)},
			after:    time.Hour,
			want:     false,
		},
		{
			name:     "exactly at the deadline is not yet stale",
			snapshot: Snapshot{ObservedAt: reference.Add(-time.Hour)},
			after:    time.Hour,
			want:     false,
		},
		{
			name:     "past the deadline",
			snapshot: Snapshot{ObservedAt: reference.Add(-2 * time.Hour)},
			after:    time.Hour,
			want:     true,
		},
		{
			name:     "observed in the future is not stale",
			snapshot: Snapshot{ObservedAt: reference.Add(time.Hour)},
			after:    time.Minute,
			want:     false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.snapshot.Stale(reference, testCase.after); got != testCase.want {
				t.Fatalf("Stale = %v, want %v", got, testCase.want)
			}
		})
	}
}

// The caller's clock may be in any zone; the stored time is UTC. Comparing
// without converting would make freshness depend on where the dashboard runs.
func TestStaleComparesInUTC(t *testing.T) {
	zone := time.FixedZone("ahead", 9*60*60)
	snapshot := Snapshot{ObservedAt: reference.Add(-30 * time.Minute)}
	if snapshot.Stale(reference.In(zone), time.Hour) {
		t.Fatal("a fresh row read as stale when the caller's clock was not UTC")
	}
}

// What the cloud may hold is a closed list. A field added here is a field the
// projection could carry, and the ones that must never arrive — an address, a
// hostname, a route prefix, a path, an endpoint, a credential reference — are
// exactly the ones nobody would add deliberately.
//
// So the shape is asserted rather than reviewed. Adding a field fails this
// until someone writes it down, which is the only moment the question gets
// asked.
func TestTheCloudHoldsNothingBeyondThisShape(t *testing.T) {
	for _, subject := range []struct {
		name   string
		value  any
		fields []string
	}{
		{
			name:   "Component",
			value:  Component{},
			fields: []string{"Component", "State", "Freshness", "DiffReason"},
		},
		{
			name:   "ProposalClass",
			value:  ProposalClass{},
			fields: []string{"Class", "Count"},
		},
		{
			name:  "Snapshot",
			value: Snapshot{},
			fields: []string{
				"NodeID", "EventID", "SessionID", "Sequence", "ObservedAt",
				"SnapshotGeneration", "ReducerVersion", "BundleGeneration",
				"RootGeneration", "UserGeneration",
				"Aggregate", "Authorization", "AuthorizationReason",
				"OpenGaps", "GapOverflow", "SourceConflicts",
				"AwaitingBaseline", "ConflictOverflow", "LineageReset",
				"UpdatedAt", "Components", "ProposalClasses",
			},
		},
	} {
		t.Run(subject.name, func(t *testing.T) {
			shape := reflect.TypeOf(subject.value)
			var held []string
			for index := 0; index < shape.NumField(); index++ {
				held = append(held, shape.Field(index).Name)
			}
			if !reflect.DeepEqual(held, subject.fields) {
				t.Fatalf("%s holds %v, and the recorded shape is %v;\n"+
					"a field added to the cloud projection has to be written "+
					"down here, because an address, a hostname, a path or a "+
					"credential reference must never become storable",
					subject.name, held, subject.fields)
			}
		})
	}
}

// Identity is a UUID here and nothing else. A node identified by a hostname
// would put the host's name in the cloud by way of its primary key.
func TestNodesAreIdentifiedOnlyByUUID(t *testing.T) {
	shape := reflect.TypeOf(Snapshot{})
	uuid := reflect.TypeOf(metadata.UUID(""))
	for _, name := range []string{"NodeID", "EventID", "SessionID"} {
		field, ok := shape.FieldByName(name)
		if !ok {
			t.Fatalf("%s is gone from the projection", name)
		}
		if field.Type != uuid {
			t.Fatalf("%s is %s, want %s", name, field.Type, uuid)
		}
	}
}
