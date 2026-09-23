package soakcompare

import (
	"testing"
	"time"
)

var (
	start  = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	window = 120 * time.Second
)

func at(minutes int) time.Time { return start.Add(time.Duration(minutes) * time.Minute) }

func compare(t *testing.T, decisions []Decision, rebuilds []Rebuild, inductions []Induction, days int) Report {
	t.Helper()
	report, err := Compare(decisions, rebuilds, inductions, nil, nil, nil, window, start, start.Add(time.Duration(days)*24*time.Hour))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	return report
}

func TestADecisionAndARebuildInsideTheWindowAgree(t *testing.T) {
	report := compare(t,
		[]Decision{{At: at(10), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(10).Add(90 * time.Second), Cause: WakeGap}}, nil, 1)
	if result := report.Results[WakeGap]; result.Agreements != 1 || report.Disagreements() != 0 {
		t.Fatalf("result = %+v, disagreements = %d", result, report.Disagreements())
	}
}

func TestARebuildDecidedAndNotMadeIsADisagreement(t *testing.T) {
	report := compare(t, []Decision{{At: at(10), Causes: []string{CarrierChanged}}}, nil, nil, 1)
	if len(report.Results[CarrierChanged].DecidedNotMade) != 1 {
		t.Fatalf("result = %+v", report.Results[CarrierChanged])
	}
}

func TestARebuildMadeAndNotDecidedIsADisagreement(t *testing.T) {
	report := compare(t, nil, []Rebuild{{At: at(10), Cause: ProcessGone}}, nil, 1)
	if len(report.Results[ProcessGone].MadeNotDecided) != 1 {
		t.Fatalf("result = %+v", report.Results[ProcessGone])
	}
}

// Outside the window is not the same event.
func TestARebuildOutsideTheWindowDoesNotAgree(t *testing.T) {
	report := compare(t,
		[]Decision{{At: at(10), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(10).Add(window + time.Second), Cause: WakeGap}}, nil, 1)
	if report.Results[WakeGap].Agreements != 0 || report.Disagreements() != 2 {
		t.Fatalf("result = %+v", report.Results[WakeGap])
	}
}

// A rebuild for another cause does not agree with a decision.
//
// Checked per cause, not as a total. A comparison that ignored the cause would
// pair the carrier rebuild with the wake decision and add a disagreement under a
// third cause, and the total would come out the same.
func TestCausesAreNotInterchangeable(t *testing.T) {
	report := compare(t,
		[]Decision{{At: at(10), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(10), Cause: CarrierChanged}}, nil, 1)
	wake, carrier, process := report.Results[WakeGap], report.Results[CarrierChanged], report.Results[ProcessGone]
	if wake.Agreements != 0 || len(wake.DecidedNotMade) != 1 {
		t.Fatalf("wake = %+v; the carrier rebuild was taken for it", wake)
	}
	if carrier.Agreements != 0 || len(carrier.MadeNotDecided) != 1 {
		t.Fatalf("carrier = %+v", carrier)
	}
	if len(process.MadeNotDecided) != 0 || len(process.DecidedNotMade) != 0 {
		t.Fatalf("process = %+v; a cause nobody named has a disagreement", process)
	}
}

// One condition held over several cycles is one episode and one agreement.
//
// This runtime decides again on every cycle until the condition passes; the
// owner rebuilds once. Counting decisions would call every cycle after the first
// a disagreement.
func TestConsecutiveDecisionsAreOneEpisode(t *testing.T) {
	report := compare(t,
		[]Decision{
			{At: at(10), Causes: []string{ProcessGone}},
			{At: at(11), Causes: []string{ProcessGone}},
			{At: at(12), Causes: []string{ProcessGone}},
		},
		[]Rebuild{{At: at(11), Cause: ProcessGone}}, nil, 1)
	if result := report.Results[ProcessGone]; result.Agreements != 1 || report.Disagreements() != 0 {
		t.Fatalf("result = %+v", result)
	}
}

// An agreement on something the operator did is counted apart.
func TestAnInducedAgreementIsCountedApart(t *testing.T) {
	report := compare(t,
		[]Decision{{At: at(10), Causes: []string{ProcessGone}}},
		[]Rebuild{{At: at(10), Cause: ProcessGone}},
		[]Induction{{At: at(10).Add(-30 * time.Second), Cause: ProcessGone}}, 1)
	if result := report.Results[ProcessGone]; result.Induced != 1 || result.Natural() != 0 {
		t.Fatalf("result = %+v", result)
	}
}

// An induction for another cause does not make an agreement induced.
func TestAnInductionOfAnotherCauseIsNotThisOne(t *testing.T) {
	report := compare(t,
		[]Decision{{At: at(10), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(10), Cause: WakeGap}},
		[]Induction{{At: at(10), Cause: ProcessGone}}, 1)
	if report.Results[WakeGap].Natural() != 1 {
		t.Fatalf("result = %+v", report.Results[WakeGap])
	}
}

func passing() ([]Decision, []Rebuild, []Induction) {
	var decisions []Decision
	var rebuilds []Rebuild
	for index, minute := range []int{600, 3000, 6000} {
		_ = index
		decisions = append(decisions, Decision{At: at(minute), Causes: []string{WakeGap}})
		rebuilds = append(rebuilds, Rebuild{At: at(minute), Cause: WakeGap})
	}
	for _, minute := range []int{1000, 2000} {
		decisions = append(decisions, Decision{At: at(minute), Causes: []string{CarrierChanged}})
		rebuilds = append(rebuilds, Rebuild{At: at(minute), Cause: CarrierChanged})
	}
	decisions = append(decisions, Decision{At: at(4000), Causes: []string{ProcessGone}})
	rebuilds = append(rebuilds, Rebuild{At: at(4000), Cause: ProcessGone})
	return decisions, rebuilds, []Induction{{At: at(4000), Cause: ProcessGone}}
}

func TestASoakThatMeetsEveryConditionPasses(t *testing.T) {
	decisions, rebuilds, inductions := passing()
	report := compare(t, decisions, rebuilds, inductions, 7)
	if ok, missing := report.Passes(Pass); !ok {
		t.Fatalf("a passing soak did not pass: %v", missing)
	}
}

func TestEachConditionIsRequired(t *testing.T) {
	for _, item := range []struct {
		name  string
		days  int
		alter func(*[]Decision, *[]Rebuild, *[]Induction)
	}{
		{"six days", 6, func(*[]Decision, *[]Rebuild, *[]Induction) {}},
		{"one disagreement", 7, func(d *[]Decision, _ *[]Rebuild, _ *[]Induction) {
			*d = append(*d, Decision{At: at(8000), Causes: []string{CarrierChanged}})
		}},
		{"two natural wake gaps", 7, func(d *[]Decision, r *[]Rebuild, _ *[]Induction) {
			*d, *r = (*d)[1:], (*r)[1:]
		}},
		{"a wake gap induced does not count as natural", 7, func(_ *[]Decision, _ *[]Rebuild, i *[]Induction) {
			*i = append(*i, Induction{At: at(600), Cause: WakeGap})
		}},
		{"one carrier change", 7, func(d *[]Decision, r *[]Rebuild, _ *[]Induction) {
			*d = append((*d)[:3], (*d)[4:]...)
			*r = append((*r)[:3], (*r)[4:]...)
		}},
		{"the process loss was not induced", 7, func(_ *[]Decision, _ *[]Rebuild, i *[]Induction) {
			*i = nil
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			decisions, rebuilds, inductions := passing()
			item.alter(&decisions, &rebuilds, &inductions)
			report := compare(t, decisions, rebuilds, inductions, item.days)
			if ok, _ := report.Passes(Pass); ok {
				t.Fatal("the soak passed without it")
			}
		})
	}
}

func TestAnInvalidComparisonIsRefused(t *testing.T) {
	if _, err := Compare(nil, nil, nil, nil, nil, nil, 0, start, start.Add(time.Hour)); err == nil {
		t.Fatal("a zero window was accepted")
	}
	if _, err := Compare(nil, nil, nil, nil, nil, nil, window, start, start); err == nil {
		t.Fatal("an empty soak was accepted")
	}
}

// A process-gone decision the owner's own restart explains is not a
// disagreement: watching from outside, a restart replaces the process too.
func TestAProcessGoneTheOwnersRestartExplainsIsNotADisagreement(t *testing.T) {
	restart := at(10).Add(-30 * time.Second)
	report, err := Compare(
		[]Decision{{At: at(10), Causes: []string{ProcessGone, CarrierChanged}}},
		[]Rebuild{{At: restart, Cause: CarrierChanged}}, nil, []time.Time{restart}, nil, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	gone := report.Results[ProcessGone]
	if len(gone.Explained) != 1 || len(gone.DecidedNotMade) != 0 || report.Disagreements() != 0 {
		t.Fatalf("process_gone = %+v, disagreements = %d", gone, report.Disagreements())
	}
	if report.Results[CarrierChanged].Agreements != 1 {
		t.Fatalf("carrier = %+v", report.Results[CarrierChanged])
	}
}

// A restart outside the window explains nothing.
func TestAProcessGoneNoRestartExplainsIsADisagreement(t *testing.T) {
	report, err := Compare(
		[]Decision{{At: at(10), Causes: []string{ProcessGone}}},
		nil, nil, []time.Time{at(10).Add(window + time.Second)}, nil, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	gone := report.Results[ProcessGone]
	if len(gone.DecidedNotMade) != 1 || len(gone.Explained) != 0 {
		t.Fatalf("process_gone = %+v", gone)
	}
}

// Only a process loss is explained by a restart; another cause still disagrees.
func TestARestartExplainsOnlyAProcessGone(t *testing.T) {
	report, err := Compare(
		[]Decision{{At: at(10), Causes: []string{CarrierChanged}}},
		nil, nil, []time.Time{at(10)}, nil, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if carrier := report.Results[CarrierChanged]; len(carrier.DecidedNotMade) != 1 || len(carrier.Explained) != 0 {
		t.Fatalf("carrier = %+v", carrier)
	}
}

// A process loss the owner rebuilt for agrees, even with a restart beside it.
func TestAnAgreementComesBeforeAnExplanation(t *testing.T) {
	report, err := Compare(
		[]Decision{{At: at(10), Causes: []string{ProcessGone}}},
		[]Rebuild{{At: at(10), Cause: ProcessGone}}, nil, []time.Time{at(10)}, nil, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gone := report.Results[ProcessGone]; gone.Agreements != 1 || len(gone.Explained) != 0 {
		t.Fatalf("process_gone = %+v", gone)
	}
}

// Nothing inside a dozing stretch is compared, in either direction.
//
// A machine dozing on battery wakes for seconds at a time and each runtime
// decides in different wakes: measured on the night of 2026-09-20, six rebuilds
// by the owning runtime against fourteen decided here, only six the same event.
func TestNothingInsideADozingStretchIsCompared(t *testing.T) {
	doze := []Dozing{{From: at(5), To: at(60)}}
	report, err := Compare(
		[]Decision{{At: at(10), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(30), Cause: WakeGap}}, nil, nil, doze, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results[WakeGap]
	if result.Agreements != 0 || report.Disagreements() != 0 {
		t.Fatalf("a dozing stretch was judged: %+v, disagreements %d", result, report.Disagreements())
	}
	// The same pair outside the stretch is a disagreement in both directions.
	report, err = Compare(
		[]Decision{{At: at(100), Causes: []string{WakeGap}}},
		[]Rebuild{{At: at(130), Cause: WakeGap}}, nil, nil, doze, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Disagreements() != 2 {
		t.Fatalf("outside the stretch: %+v", report.Results[WakeGap])
	}
}

// An event whose gap began while the machine dozed is about that doze.
//
// Measured 2026-09-20: the owning runtime named a wake at 10:21:47Z on a gap of
// 1,625 seconds that began at 09:54:42Z, inside a stretch this runtime's cycles
// never finished. This runtime, whose own cycles had resumed by then, named no
// wake, and the pair read as a disagreement about a machine that was awake.
func TestAnEventWhoseGapBeganWhileDozingIsNotJudged(t *testing.T) {
	doze := []Dozing{{From: at(5), To: at(60)}}
	report, err := Compare(nil,
		[]Rebuild{{At: at(70), Cause: WakeGap, Previous: at(40)}}, nil, nil, doze, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Disagreements() != 0 {
		t.Fatalf("a rebuild whose gap began while dozing was judged: %+v", report.Results[WakeGap])
	}
	// The same rebuild with its previous activity after the doze is judged.
	report, err = Compare(nil,
		[]Rebuild{{At: at(70), Cause: WakeGap, Previous: at(65)}}, nil, nil, doze, nil,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Disagreements() != 1 {
		t.Fatalf("a rebuild after the doze was not judged: %+v", report.Results[WakeGap])
	}
	// And the same for a decision of this runtime's own.
	report, err = Compare(
		[]Decision{{At: at(70), Causes: []string{WakeGap}, Previous: at(40)}},
		nil, nil, nil, doze, nil, window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Disagreements() != 0 {
		t.Fatalf("a decision whose gap began while dozing was judged: %+v", report.Results[WakeGap])
	}
}

// A carrier change the owner made and this runtime could not have seen is not a
// disagreement. Measured 2026-09-23: an ingress target moved to the physical
// interface at 11:00:52Z and was back by 11:01:09Z, between two cycles of this
// runtime that read one and the same signature.
func TestACarrierChangeShorterThanACycleIsNotADisagreement(t *testing.T) {
	marks := []Carrier{{At: at(0), Digest: "7b60440cfa7b"}}
	report, err := Compare(nil,
		[]Rebuild{{At: at(60), Cause: CarrierChanged}}, nil, nil, nil, marks,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results[CarrierChanged]
	if len(result.Unobservable) != 1 || report.Disagreements() != 0 {
		t.Fatalf("carrier = %+v, disagreements = %d", result, report.Disagreements())
	}
	// A change this runtime did read around that moment is a disagreement: it
	// saw the carrier move and decided nothing.
	report, err = Compare(nil,
		[]Rebuild{{At: at(60), Cause: CarrierChanged}}, nil, nil, nil,
		append(marks, Carrier{At: at(60).Add(30 * time.Second), Digest: "0f0f0f0f0f0f"}),
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results[CarrierChanged].MadeNotDecided) != 1 || len(report.Results[CarrierChanged].Unobservable) != 0 {
		t.Fatalf("a carrier this runtime saw move was excused: %+v", report.Results[CarrierChanged])
	}
	// A runtime that had read no carrier at all excuses nothing.
	report, err = Compare(nil,
		[]Rebuild{{At: at(60), Cause: CarrierChanged}}, nil, nil, nil,
		[]Carrier{{At: at(600), Digest: "7b60440cfa7b"}},
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results[CarrierChanged].MadeNotDecided) != 1 {
		t.Fatalf("a rebuild before this runtime read any carrier was excused: %+v", report.Results[CarrierChanged])
	}
	// The excuse is the carrier's alone: a wake or a process loss the owner made
	// stays a disagreement.
	report, err = Compare(nil,
		[]Rebuild{{At: at(60), Cause: WakeGap}}, nil, nil, nil, marks,
		window, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results[WakeGap].MadeNotDecided) != 1 {
		t.Fatalf("a wake was excused by the carrier: %+v", report.Results[WakeGap])
	}
}
