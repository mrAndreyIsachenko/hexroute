package eventarchive

import (
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

func transition(component control.Component, from, to control.State, generation uint64) []byte {
	encoded, err := event.Encode(event.SchemaTransition, event.Transition{
		Component: component, From: from, To: to,
		Reason: control.ReasonProbeFailed, Generation: generation,
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

func fill(t *testing.T, archive *Archive) {
	t.Helper()
	for index := 0; index < 6; index++ {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if _, err := archive.Append(
		transition(control.ComponentTunnel, control.StateHealthy,
			control.StateDegraded, 1)); err != nil {
		t.Fatalf("append transition: %v", err)
	}
	if _, err := archive.Append(diagnostic(3)); err != nil {
		t.Fatalf("append diagnostic: %v", err)
	}
}

func readAll(t *testing.T, archive *Archive) Reading {
	t.Helper()
	reading, err := archive.Read(Query{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return reading
}

func summarize(t *testing.T, reading Reading) Report {
	t.Helper()
	report, err := Summarize(reading)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	return report
}

// 2.3, 2.4 — two archives holding the same thing produce the same report. A
// report that varied run to run could not be compared with last week's, which
// is the only thing a weekly report is for.
func TestEqualArchivesProduceEqualReports(t *testing.T) {
	one := openArchive(t, t.TempDir(), newClock(), Options{})
	other := openArchive(t, t.TempDir(), newClock(), Options{})
	fill(t, one)
	fill(t, other)

	first := summarize(t, readAll(t, one))
	second := summarize(t, readAll(t, other))
	if first.Digest == "" {
		t.Fatal("a report carried no digest")
	}
	if first.Digest != second.Digest {
		t.Fatalf("two archives holding the same records digested to %s and %s",
			first.Digest, second.Digest)
	}
}

// 2.2 — the aggregation does not depend on map iteration. Repeating it has to
// land on the same bytes, or the ordering is a property of the run.
func TestSummarizingTwiceIsIdentical(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	fill(t, archive)
	reading := readAll(t, archive)

	baseline := summarize(t, reading)
	for attempt := 0; attempt < 16; attempt++ {
		again := summarize(t, reading)
		if again.Digest != baseline.Digest {
			t.Fatalf("summarizing the same reading gave %s then %s",
				baseline.Digest, again.Digest)
		}
	}
	recomputed, err := baseline.Digested()
	if err != nil {
		t.Fatalf("digested: %v", err)
	}
	if recomputed != baseline.Digest {
		t.Fatal("a report does not match its own digest")
	}
}

// 2.2 — the tie-break is on recorded values, so equal counts still order the
// same way every time.
func TestTheRarityTieBreakIsTotal(t *testing.T) {
	tied := []RareFinding{
		{Schema: "b", Component: "x", Count: 1, FirstSequence: 5},
		{Schema: "a", Component: "x", Count: 1, FirstSequence: 5},
		{Schema: "a", Component: "x", Count: 1, FirstSequence: 2},
		{Schema: "a", Component: "w", Count: 1, FirstSequence: 2},
		{Schema: "a", Component: "w", Count: 2, FirstSequence: 1},
	}
	for one := range tied {
		for other := range tied {
			if one == other {
				if lessRare(tied[one], tied[other]) {
					t.Fatal("a finding sorts before itself")
				}
				continue
			}
			if lessRare(tied[one], tied[other]) == lessRare(tied[other], tied[one]) {
				t.Fatalf("%+v and %+v have no order between them",
					tied[one], tied[other])
			}
		}
	}
}

// 2.1, 2.4 — an empty window says it is empty. Rendering it as a period in
// which nothing happened is the one reading it must never receive.
func TestAnEmptyWindowIsNeverAQuietOne(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	fill(t, archive)

	// A window entirely before anything the archive holds.
	distant := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	reading, err := archive.Read(Query{From: distant, To: distant.Add(time.Hour)})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(reading.Records) != 0 {
		t.Fatalf("a window before the archive returned %d records",
			len(reading.Records))
	}
	if !reading.Covered.Empty {
		t.Fatal("an empty answer did not say it was empty")
	}
	if !reading.Shortened() {
		t.Fatal("a window the archive cannot answer for was not reported as shortened")
	}
	report := summarize(t, reading)
	if report.Records != 0 || !report.Covered.Empty || !report.Shortened {
		t.Fatalf("an empty window summarized as %+v", report.Covered)
	}
}

// 2.1, 2.4 — a request reaching back further than the archive holds is
// reported as shortened, so an eviction is not read as a quiet week.
func TestAWindowLongerThanTheArchiveIsReportedShortened(t *testing.T) {
	clock := newClock()
	archive := openArchive(t, t.TempDir(), clock, Options{})
	fill(t, archive)

	window, err := archive.Window()
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	reading, err := archive.Read(Query{
		From: window.Oldest.Add(-24 * time.Hour),
		To:   window.Newest,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(reading.Records) == 0 {
		t.Fatal("the request covered the archive and returned nothing")
	}
	if !reading.Shortened() {
		t.Fatal("a request reaching before the archive was not reported as shortened")
	}
	if summarize(t, reading).Shortened != true {
		t.Fatal("the report did not carry that the window was shortened")
	}
}

// 2.1 — a request the limit cut short says so, rather than presenting a
// partial window as the whole one.
func TestATruncatedReadSaysSo(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	fill(t, archive)

	reading, err := archive.Read(Query{Limit: 3})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(reading.Records) != 3 {
		t.Fatalf("limit 3 returned %d records", len(reading.Records))
	}
	if !reading.Truncated || !reading.Shortened() {
		t.Fatal("a truncated read did not report itself truncated")
	}
}

// 2.2 — transitions are reported in the order they occurred, with what they
// were between.
func TestTransitionsAreReportedInOrder(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	if _, err := archive.Append(transition(control.ComponentTunnel,
		control.StateHealthy, control.StateDegraded, 1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := archive.Append(transition(control.ComponentTunnel,
		control.StateDegraded, control.StateHealthy, 2)); err != nil {
		t.Fatalf("append: %v", err)
	}
	report := summarize(t, readAll(t, archive))
	if len(report.Transitions) != 2 {
		t.Fatalf("reported %d transitions, wrote 2", len(report.Transitions))
	}
	if report.Transitions[0].To != string(control.StateDegraded) ||
		report.Transitions[1].To != string(control.StateHealthy) {
		t.Fatalf("transitions out of order: %+v", report.Transitions)
	}
	if report.Transitions[0].Sequence >= report.Transitions[1].Sequence {
		t.Fatal("transition sequences are not increasing")
	}
}

// 2.3 — commentary is outside the digest, so a report produced with a model
// present stays comparable with one produced without.
func TestCommentaryDoesNotMoveTheDigest(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	fill(t, archive)
	report := summarize(t, readAll(t, archive))
	if len(report.Rare) == 0 {
		t.Fatal("nothing was ranked")
	}
	report.Rare[0].Commentary = "this one is worth a look"
	recomputed, err := report.Digested()
	if err != nil {
		t.Fatalf("digested: %v", err)
	}
	if recomputed != report.Digest {
		t.Fatal("adding commentary changed what the report says happened")
	}
}
