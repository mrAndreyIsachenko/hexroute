package connectivityjournal

import (
	"errors"
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
