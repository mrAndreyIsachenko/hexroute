package connectivityjournal

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

type markingUrgent struct {
	marks   int
	failing bool
}

func (urgent *markingUrgent) Mark() error {
	if urgent.failing {
		return errors.New("the marker cannot be written")
	}
	urgent.marks++
	return nil
}

func handback(generation uint64) event.Incident {
	return event.Incident{
		IncidentID: "tunnel-handback-1",
		Status:     event.IncidentOpened,
		Severity:   event.SeverityCritical,
		Category:   event.IncidentAvailability,
		Component:  control.ComponentRuntime,
		Generation: generation,
	}
}

// The incident has to reach the spool, because the spool is the upload queue.
// Writing it only to the archive is what the handback record already does, and
// that record is why nothing leaves this machine.
func TestAnIncidentIsWrittenMirroredAndMakesTheUploadDue(t *testing.T) {
	sink := &countingSink{}
	urgent := &markingUrgent{}
	journal := openJournalWith(t, policy.DomainRoot, Options{
		NodeID: testNodeID, Clock: &advancingClock{},
		Mirror: sink, Urgent: urgent,
	})

	if err := journal.AppendIncident(handback(7)); err != nil {
		t.Fatalf("append incident: %v", err)
	}

	if len(sink.taken) != 1 {
		t.Fatalf("the mirror took %d records, want 1", len(sink.taken))
	}
	decoded, err := event.Decode(sink.taken[0])
	if err != nil {
		t.Fatalf("the mirror was handed something undecodable: %v", err)
	}
	if decoded.Schema != event.SchemaIncident {
		t.Fatalf("mirrored schema is %q, want %q", decoded.Schema, event.SchemaIncident)
	}
	// Critical by its schema, which is what gets it into a spool that is at its
	// bound — and on this machine the spool is at its bound.
	if decoded.Priority != event.PriorityCritical {
		t.Fatalf("incident priority is %q, want %q",
			decoded.Priority, event.PriorityCritical)
	}
	if urgent.marks != 1 {
		t.Fatalf("the upload was marked due %d times, want 1", urgent.marks)
	}
	if journal.UrgentFailures() != 0 {
		t.Fatalf("%d marks were reported missed", journal.UrgentFailures())
	}
}

// Writing a fact must not say an upload is urgent. Every cycle writes one, so a
// fact that marked the upload due would start the agent every minute and the
// marker would stop meaning anything.
func TestAFactDoesNotMakeTheUploadDue(t *testing.T) {
	urgent := &markingUrgent{}
	journal := openJournalWith(t, policy.DomainRoot, Options{
		NodeID: testNodeID, Clock: &advancingClock{}, Urgent: urgent,
	})

	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	if err := journal.Append(fact, 1, 1, "accepted", safety.RoleAuthoritative); err != nil {
		t.Fatalf("append: %v", err)
	}
	if urgent.marks != 0 {
		t.Fatalf("a fact marked the upload due %d times, want 0", urgent.marks)
	}
}

// The readers were built to skip an entry that is not a fact, because the spool
// has always written its own incidents. This proves it of the entry this change
// adds rather than assuming the two are alike.
func TestTheFactStreamIsUnchangedByAnIncidentBesideIt(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	components := []connectivity.Component{
		connectivity.ComponentDNS,
		connectivity.ComponentRelays,
		connectivity.ComponentTransports,
	}
	for index, component := range components {
		fact := connectivity.FixtureBaseline(component, 1)
		if err := journal.Append(fact, uint64(index+1), uint64(index+1),
			"accepted", safety.RoleAuthoritative); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	before, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	newestBefore, _, err := journal.Newest()
	if err != nil {
		t.Fatalf("newest: %v", err)
	}
	baselinesBefore, err := journal.LatestBaselines()
	if err != nil {
		t.Fatalf("baselines: %v", err)
	}
	tailBefore, continuousBefore, err := journal.RecordsAfter(1)
	if err != nil {
		t.Fatalf("records after: %v", err)
	}

	if err := journal.AppendIncident(handback(3)); err != nil {
		t.Fatalf("append incident: %v", err)
	}

	after, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("the fact stream holds %d records with an incident beside it, held %d",
			len(after), len(before))
	}
	for index := range before {
		if after[index].Digest != before[index].Digest ||
			after[index].HostSequence != before[index].HostSequence ||
			after[index].FoldPosition != before[index].FoldPosition {
			t.Fatalf("record %d changed when an incident was written beside it", index)
		}
	}
	newestAfter, _, err := journal.Newest()
	if err != nil {
		t.Fatalf("newest: %v", err)
	}
	if newestAfter.Digest != newestBefore.Digest ||
		newestAfter.HostSequence != newestBefore.HostSequence {
		t.Fatal("the newest fact changed when an incident was written after it")
	}
	baselinesAfter, err := journal.LatestBaselines()
	if err != nil {
		t.Fatalf("baselines: %v", err)
	}
	if len(baselinesAfter) != len(baselinesBefore) {
		t.Fatalf("baselines went from %d to %d",
			len(baselinesBefore), len(baselinesAfter))
	}
	tailAfter, continuousAfter, err := journal.RecordsAfter(1)
	if err != nil {
		t.Fatalf("records after: %v", err)
	}
	if len(tailAfter) != len(tailBefore) || continuousAfter != continuousBefore {
		t.Fatalf("the tail after the watermark went from %d records (continuous=%v) to %d (continuous=%v)",
			len(tailBefore), continuousBefore, len(tailAfter), continuousAfter)
	}
}

// A payload the schema does not accept writes nothing and marks nothing. An
// incident that was refused must not leave an upload due for a record that is
// not there.
func TestARefusedIncidentWritesNothingAndMarksNothing(t *testing.T) {
	sink := &countingSink{}
	urgent := &markingUrgent{}
	journal := openJournalWith(t, policy.DomainRoot, Options{
		NodeID: testNodeID, Clock: &advancingClock{},
		Mirror: sink, Urgent: urgent,
	})

	broken := []event.Incident{
		{},
		handbackWith(func(incident *event.Incident) { incident.IncidentID = "" }),
		handbackWith(func(incident *event.Incident) { incident.Status = "finished" }),
		handbackWith(func(incident *event.Incident) { incident.Severity = "loud" }),
		handbackWith(func(incident *event.Incident) { incident.Category = "weather" }),
		handbackWith(func(incident *event.Incident) { incident.Component = "nothing" }),
	}
	for index, incident := range broken {
		if err := journal.AppendIncident(incident); err == nil {
			t.Fatalf("incident %d was accepted and should not have been", index)
		}
	}

	records, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("the journal holds %d records after only refusals", len(records))
	}
	if len(sink.taken) != 0 {
		t.Fatalf("the mirror took %d refused records", len(sink.taken))
	}
	if urgent.marks != 0 {
		t.Fatalf("a refused incident marked the upload due %d times", urgent.marks)
	}
}

// A marker that cannot be written costs the record its promptness and nothing
// else. The interval collects it, and the count is how anyone would know the
// promptness was lost at all — the records themselves do not show it.
func TestAFailingMarkerIsCountedAndNeverFailsTheWrite(t *testing.T) {
	urgent := &markingUrgent{failing: true}
	journal := openJournalWith(t, policy.DomainRoot, Options{
		NodeID: testNodeID, Clock: &advancingClock{}, Urgent: urgent,
	})

	for index := 1; index <= 3; index++ {
		if err := journal.AppendIncident(handback(uint64(index))); err != nil {
			t.Fatalf("a failing marker failed the write: %v", err)
		}
	}
	if journal.UrgentFailures() != 3 {
		t.Fatalf("reported %d missed marks, want 3", journal.UrgentFailures())
	}
}

// Nothing is required of a journal that was given no marker: the agent's
// interval is then the only trigger, which is what a host without the agent
// installed looks like.
func TestAJournalWithNoMarkerWritesAnIncidentAnyway(t *testing.T) {
	journal := openJournal(t, policy.DomainRoot, 0)
	if err := journal.AppendIncident(handback(1)); err != nil {
		t.Fatalf("append incident: %v", err)
	}
	if journal.UrgentFailures() != 0 {
		t.Fatalf("%d marks were reported missed with no marker configured",
			journal.UrgentFailures())
	}
}

func handbackWith(change func(*event.Incident)) event.Incident {
	incident := handback(1)
	change(&incident)
	return incident
}
