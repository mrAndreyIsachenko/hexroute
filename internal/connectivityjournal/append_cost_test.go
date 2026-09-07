package connectivityjournal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// TestJournalAppendDoesNotReadTheStoredRecords covers the layer the daemon
// actually uses.
//
// `hexrouted` does not call the spool; it calls this journal, once per fact per
// publication. A repair that bounded the spool and left this path reading every
// record would look fixed in one package and behave exactly as before on the
// machine — which is how the fault presented in the first place: as a daemon
// that would not answer, with nothing in it named spool.
//
// The assertion is exact rather than timed. A stopwatch here measured the race
// detector as much as the code, and a threshold tuned until it passed would
// have measured the threshold.
func TestJournalAppendDoesNotReadTheStoredRecords(t *testing.T) {
	// Opened at a known path so the test can reach the records on disk.
	root := filepath.Join(t.TempDir(), "root")
	journal, err := Open(root, policy.DomainRoot, Options{
		MaxBytes: 64 * 1024 * 1024, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	rootFacts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainRoot {
			rootFacts = append(rootFacts, fact)
		}
	}
	if len(rootFacts) == 0 {
		t.Fatal("no root facts in the baseline fixture")
	}

	const seeded = 8
	for index := 1; index <= seeded; index++ {
		fact := rootFacts[index%len(rootFacts)]
		if err := journal.Append(
			fact, uint64(index), uint64(index), "accepted", safety.RoleAuthoritative,
		); err != nil {
			t.Fatalf("seed append %d: %v", index, err)
		}
	}

	// Make every stored record unreadable. An append that opens them would set
	// them aside, and setting aside is reported — so an append that looks
	// cannot do it quietly.
	spoolDir := filepath.Join(root, "spool")
	names, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool directory: %v", err)
	}
	damaged := 0
	for _, name := range names {
		if name.IsDir() || name.Name()[0] == '.' {
			continue
		}
		if err := os.WriteFile(
			filepath.Join(spoolDir, name.Name()),
			[]byte(`{"schema":"not a record"}`), 0o600,
		); err != nil {
			t.Fatal(err)
		}
		damaged++
	}
	if damaged == 0 {
		t.Fatal("nothing was stored to damage; the fixture proved nothing")
	}

	if err := journal.Append(
		rootFacts[0], uint64(seeded+1), uint64(seeded+1), "accepted", safety.RoleAuthoritative,
	); err != nil {
		t.Fatalf("Append() over %d damaged stored records = %v; "+
			"recording stopped because of records already lost", damaged, err)
	}
	// Reading one would have set it aside, which renames the file, so the
	// directory says whether the append looked.
	after, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatal(err)
	}
	aside := 0
	for _, name := range after {
		if strings.HasPrefix(name.Name(), ".quarantine-") {
			aside++
		}
	}
	if aside != 0 {
		t.Fatalf("appending set aside %d stored records; the journal's append path reads them", aside)
	}
}
