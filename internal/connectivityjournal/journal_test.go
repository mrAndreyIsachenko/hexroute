package connectivityjournal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

const testNodeID = metadata.UUID("22222222-2222-4222-8222-222222222222")

type advancingClock struct{ tick int64 }

func (clock *advancingClock) WallNow() time.Time {
	clock.tick++
	return time.Date(2026, time.July, 23, 18, 30, 0, 0, time.UTC).
		Add(time.Duration(clock.tick) * time.Millisecond)
}

func (clock *advancingClock) MonotonicNow() time.Duration {
	return time.Duration(clock.tick) * time.Millisecond
}

func openJournal(t *testing.T, domain policy.Domain, maxBytes int64) *Journal {
	t.Helper()
	name := "root"
	if domain == policy.DomainUser {
		name = "user"
	}
	journal, err := Open(filepath.Join(t.TempDir(), name), domain, Options{
		MaxBytes: maxBytes, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return journal
}

func TestAcceptedFactsReadBackInHostOrder(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	rootFacts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainRoot {
			rootFacts = append(rootFacts, fact)
		}
	}
	// Append out of order; the journal must still read back in order.
	for index := len(rootFacts) - 1; index >= 0; index-- {
		if err := journal.Append(rootFacts[index], uint64(index+1), uint64(index+1), "accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	records, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != len(rootFacts) {
		t.Fatalf("got %d records, want %d", len(records), len(rootFacts))
	}
	for index, record := range records {
		if record.HostSequence != uint64(index+1) {
			t.Fatalf("record %d has host sequence %d", index, record.HostSequence)
		}
	}
}

// A user fact in the root journal would put credentialed observation on the
// wrong side of the privilege boundary.
func TestJournalRefusesTheOtherDomain(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	fact := connectivity.FixtureBaseline(connectivity.ComponentUserAccess, 1)
	if err := journal.Append(fact, 1, 1, "accepted", safety.RoleAuthoritative); !errors.Is(err, ErrDomainMismatch) {
		t.Fatalf("got %v, want %v", err, ErrDomainMismatch)
	}
}

// The acceptor already checked ownership. The journal checks again, because a
// journal that trusted its caller would be a way to write a fact nobody
// accepted.
func TestJournalRechecksOwnership(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	fact.SourceID = "root.routes"
	if err := journal.Append(fact, 1, 1, "accepted", safety.RoleAuthoritative); !errors.Is(err, safety.ErrUnknownSource) {
		t.Fatalf("got %v, want %v", err, safety.ErrUnknownSource)
	}
}

func TestRecordsAfterReportsContinuity(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	facts := []connectivity.Fact{
		connectivity.FixtureBaseline(connectivity.ComponentDNS, 1),
		connectivity.FixtureBaseline(connectivity.ComponentRelays, 1),
		connectivity.FixtureBaseline(connectivity.ComponentTransports, 1),
	}
	for index, fact := range facts {
		if err := journal.Append(fact, uint64(index+1), uint64(index+1), "accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	records, continuous, err := journal.RecordsAfter(1)
	if err != nil {
		t.Fatalf("records after: %v", err)
	}
	if !continuous || len(records) != 2 {
		t.Fatalf("continuous=%v records=%d, want true/2", continuous, len(records))
	}

	// A hole in the accepted order is reported, not folded over.
	gapped := openJournal(t, policy.DomainRoot, 0)
	if err := gapped.Append(facts[0], 1, 1, "accepted", safety.RoleAuthoritative); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := gapped.Append(facts[1], 4, 4, "accepted", safety.RoleAuthoritative); err != nil {
		t.Fatalf("append: %v", err)
	}
	_, continuous, err = gapped.RecordsAfter(1)
	if err != nil {
		t.Fatalf("records after: %v", err)
	}
	if continuous {
		t.Fatal("a gap in the accepted order was reported as continuous")
	}
}

// Retention must not be able to remove what a gap needs in order to close.
func TestBaselinesSurviveEvictionOfObservations(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 64*1024)

	baselines := make(map[connectivity.Component]uint64)
	sequence := uint64(0)
	for _, component := range []connectivity.Component{
		connectivity.ComponentDNS,
		connectivity.ComponentRelays,
		connectivity.ComponentTransports,
	} {
		sequence++
		fact := connectivity.FixtureBaseline(component, sequence)
		if err := journal.Append(fact, sequence, sequence, "accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append baseline: %v", err)
		}
		baselines[component] = sequence
	}

	// Flood the journal with ordinary observations until eviction must run.
	for index := 0; index < 400; index++ {
		sequence++
		fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, sequence)
		fact.Baseline = false
		fact.Reason = connectivity.ReasonProbeSucceeded
		if err := journal.Append(fact, sequence, sequence, "accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append observation %d: %v", index, err)
		}
	}

	records, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if uint64(len(records)) >= sequence {
		t.Fatal("nothing was evicted; the retention property is untested")
	}

	retained, err := journal.LatestBaselines()
	if err != nil {
		t.Fatalf("baselines: %v", err)
	}
	for component, hostSequence := range baselines {
		record, kept := retained[component]
		if !kept {
			t.Fatalf("the baseline for %s was evicted before ordinary observations", component)
		}
		if record.HostSequence != hostSequence {
			t.Fatalf("%s kept host sequence %d, want %d",
				component, record.HostSequence, hostSequence)
		}
	}
}

func TestLatestBaselineWinsPerComponent(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	for sequence := uint64(1); sequence <= 3; sequence++ {
		fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, sequence)
		if err := journal.Append(fact, sequence, sequence, "accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	latest, err := journal.LatestBaselines()
	if err != nil {
		t.Fatalf("baselines: %v", err)
	}
	record, ok := latest[connectivity.ComponentDNS]
	if !ok || record.HostSequence != 3 {
		t.Fatalf("kept host sequence %d, want 3", record.HostSequence)
	}
}

func TestUserJournalIsSeparate(t *testing.T) {
	user := openJournal(t, policy.DomainUser, 0)
	fact := connectivity.FixtureBaseline(connectivity.ComponentSessionExpiry, 1)
	if err := user.Append(fact, 1, 1, "accepted", safety.RoleAuthoritative); err != nil {
		t.Fatalf("append: %v", err)
	}
	if user.Domain() != policy.DomainUser {
		t.Fatalf("domain %q, want user", user.Domain())
	}
	records, err := user.Records()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
}

// The event package spells the accepted outcome for itself, because it may not
// depend on the acceptor. Two spellings of one wire value drift silently, and
// the drift would land where it matters most: a record that entered the
// accepted order would stop being recognised as one.
func TestTheTwoSpellingsOfAcceptedAgree(t *testing.T) {
	if string(connectivityaccept.OutcomeAccepted) != event.OutcomeAccepted {
		t.Fatalf("the acceptor says %q and the journal record says %q",
			connectivityaccept.OutcomeAccepted, event.OutcomeAccepted)
	}
}

// A journal written by an older build is not damage, and the difference is
// not academic: records this build cannot read made the read model refuse to
// start, every ten seconds under launchd, for as long as they were on disk.
// No restart cleared it and no amount of waiting would have.
func TestAJournalInAFormatThisBuildCannotReadIsSetAside(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "root")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	// Something is in there and nothing says what format it is, which is
	// exactly what a journal written before the marker existed looks like.
	if err := os.WriteFile(filepath.Join(path, "0001.json"),
		[]byte(`{"schema":"from a build that came before"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	journal, err := Open(path, policy.DomainRoot, Options{
		MaxBytes: 1 << 20, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("a journal this build cannot read refused to open: %v", err)
	}
	if !journal.Superseded() {
		t.Fatal("the unreadable journal was opened as if it were ours")
	}
	records, err := journal.Records()
	if err != nil {
		t.Fatalf("the fresh journal is unreadable: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("the fresh journal came up holding %d records", len(records))
	}
	// The evidence is set aside, not destroyed.
	if _, err := os.Stat(filepath.Join(directory, "root.superseded", "0001.json")); err != nil {
		t.Fatalf("the superseded records were not kept: %v", err)
	}

	// And the next start is an ordinary one.
	again, err := Open(path, policy.DomainRoot, Options{
		MaxBytes: 1 << 20, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if again.Superseded() {
		t.Fatal("a journal this build wrote was set aside as foreign")
	}
}

// A second supersession would overwrite the first, and losing evidence
// quietly is the one thing this may not do.
func TestASecondSupersessionRefusesRatherThanOverwrite(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "root")
	for _, name := range []string{path, path + ".superseded"} {
		if err := os.MkdirAll(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "0001.json"),
		[]byte(`{"schema":"older still"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, policy.DomainRoot, Options{
		MaxBytes: 1 << 20, NodeID: testNodeID, Clock: &advancingClock{},
	}); err == nil {
		t.Fatal("a second supersession overwrote the first")
	}
}

// countingSink stands for the retention archive. Failing on demand is the
// point: the journal is evidence and the archive is a convenience, and the
// convenience must never be able to cost the evidence.
type countingSink struct {
	taken   [][]byte
	failing bool
}

func (sink *countingSink) Append(encoded []byte) (uint64, error) {
	if sink.failing {
		return 0, errors.New("sink is unavailable")
	}
	held := make([]byte, len(encoded))
	copy(held, encoded)
	sink.taken = append(sink.taken, held)
	return uint64(len(sink.taken)), nil
}

// Everything the journal writes reaches the mirror. Holding this in the
// journal rather than at the call site is what makes it true by construction:
// two places agreeing to encode the same fact the same way is an agreement
// that eventually stops holding.
func TestTheMirrorReceivesEveryRecordTheJournalWrites(t *testing.T) {
	sink := &countingSink{}
	journal, err := Open(filepath.Join(t.TempDir(), "root"), policy.DomainRoot,
		Options{NodeID: testNodeID, Clock: &advancingClock{}, Mirror: sink})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	facts := []connectivity.Fact{
		connectivity.FixtureBaseline(connectivity.ComponentDNS, 1),
		connectivity.FixtureBaseline(connectivity.ComponentRelays, 1),
		connectivity.FixtureBaseline(connectivity.ComponentTransports, 1),
	}
	for index, fact := range facts {
		if err := journal.Append(fact, uint64(index+1), uint64(index+1),
			"accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	records, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(sink.taken) != len(records) {
		t.Fatalf("the journal holds %d records and the mirror took %d",
			len(records), len(sink.taken))
	}
	if journal.MirrorFailures() != 0 {
		t.Fatalf("%d records were reported missed", journal.MirrorFailures())
	}
	// What the mirror took has to be the record, not a description of it.
	for _, encoded := range sink.taken {
		if _, err := event.Decode(encoded); err != nil {
			t.Fatalf("the mirror was handed something undecodable: %v", err)
		}
	}
}

// A mirror that fails costs a copy and never a write. The journal is what the
// lineage replays from, and a full disk under the archive must not be able to
// stop the host recording what it observed.
func TestAFailingMirrorIsCountedAndNeverFailsTheJournal(t *testing.T) {
	sink := &countingSink{failing: true}
	journal, err := Open(filepath.Join(t.TempDir(), "root"), policy.DomainRoot,
		Options{NodeID: testNodeID, Clock: &advancingClock{}, Mirror: sink})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for index := 1; index <= 3; index++ {
		fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, uint64(index))
		if err := journal.Append(fact, uint64(index), uint64(index),
			"accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("a failing mirror failed the journal: %v", err)
		}
	}
	records, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("the journal holds %d records, wrote 3", len(records))
	}
	if journal.MirrorFailures() != 3 {
		t.Fatalf("reported %d misses, want 3", journal.MirrorFailures())
	}
}
