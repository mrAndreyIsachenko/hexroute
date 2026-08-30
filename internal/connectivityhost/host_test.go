package connectivityhost

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

func reachedEvidence() Evidence {
	return Evidence{
		Reached: true,
		Physical: observe.PhysicalNetwork{
			Interface: "en0",
			Gateway:   netip.MustParseAddr("192.0.2.1"),
			Link:      observe.LinkStateUp,
		},
		ConfiguredRoutes: 1,
		Routes: []observe.RouteObservation{{
			Destination: netip.MustParseAddr("192.0.2.1"),
			Interface:   "en0",
		}},
	}
}

func TestReaderTurnsOneCycleIntoASnapshot(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	status, _, err := reader.Observe(reachedEvidence(), connectivityreduce.PolicyDescriptor{}, 1000)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(status.Components) != len(connectivity.Components()) {
		t.Fatalf("%d components, want %d",
			len(status.Components), len(connectivity.Components()))
	}
	if status.SnapshotGeneration == 0 {
		t.Fatal("the first cycle produced no snapshot generation")
	}
}

// The cycle can return before it has observed everything — no managed TUN
// means it stops there. The read model must still describe what was seen
// rather than refusing the whole publication.
func TestEarlyReturningCycleStillProducesASnapshot(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	evidence := Evidence{
		Reached: true,
		Physical: observe.PhysicalNetwork{
			Interface: "en0",
			Gateway:   netip.MustParseAddr("192.0.2.1"),
			Link:      observe.LinkStateUp,
		},
		ConfiguredRoutes: 1,
		TUNError:         errNoManagedTUN,
	}
	status, _, err := reader.Observe(evidence, connectivityreduce.PolicyDescriptor{}, 1000)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(status.Components) != len(connectivity.Components()) {
		t.Fatalf("%d components, want %d",
			len(status.Components), len(connectivity.Components()))
	}
}

var errNoManagedTUN = errors.New("no managed TUN")
