package spool

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

// TestAppendCostDoesNotGrowWithEntryCount is the regression for what the
// operator's machine ran into.
//
// The spool bounds its size in bytes and nothing bounds the entry count, so
// small records reach tens of thousands inside that bound. When Append re-read
// and re-decoded every stable entry, the live root store reached 60,915 files
// and the daemon saturated a core and stopped answering its socket. The failure
// reads as an unresponsive daemon, not as a spool.
//
// The assertion is exact rather than timed. A stopwatch across sixteen times
// the records would say the cost fell, but only by how much on this machine
// today, and a threshold tuned until it passes measures the threshold.
//
// So the property is stated as behaviour: the stored records are made
// undecodable, and the append neither fails on them nor notices them. Reading
// one would quarantine it, and quarantine is reported — so an append that looks
// cannot stay silent.
func TestAppendDoesNotReadTheStoredRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	spool, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for index := 1; index <= 200; index++ {
		if err := os.WriteFile(
			spool.stablePath(uint64(index)),
			[]byte(`{"schema":"not a record at all"}`),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("open over undecodable records: %v", err)
	}

	if _, err := reopened.Append(costEvent(t, 1)); err != nil {
		t.Fatalf("Append() over undecodable stored records = %v", err)
	}
	if quarantined := reopened.TakeQuarantined(); len(quarantined) != 0 {
		t.Fatalf("appending set aside %d stored records; it read what it does not use",
			len(quarantined))
	}

	// And the size it reports comes from the same place, so asking costs no
	// reading either.
	if _, err := reopened.Size(); err != nil {
		t.Fatalf("Size() over undecodable stored records = %v", err)
	}
	if quarantined := reopened.TakeQuarantined(); len(quarantined) != 0 {
		t.Fatalf("reporting size set aside %d stored records", len(quarantined))
	}
}

func costEvent(t *testing.T, index int) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaIncident, event.Incident{
		IncidentID: fmt.Sprintf("append-cost-%d", index),
		Status:     event.IncidentOpened,
		Severity:   event.SeverityWarning,
		Category:   event.IncidentAvailability,
		Component:  control.ComponentTunnel,
		Generation: uint64(index + 1),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return encoded
}
