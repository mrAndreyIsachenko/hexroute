package connectivityaccept

import (
	"errors"
	"sync"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

func mustAccept(t *testing.T, acceptor *Acceptor, fact connectivity.Fact) Acceptance {
	t.Helper()
	acceptance, err := acceptor.Accept(fact, fact.Domain)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	return acceptance
}

// seq builds the nth fact of one source's stream. The fixture varies with the
// sequence, so two different sequences are genuinely different content.
func seq(component connectivity.Component, sequence uint64) connectivity.Fact {
	fact := connectivity.FixtureBaseline(component, sequence)
	fact.Baseline = false
	fact.Reason = connectivity.ReasonProbeSucceeded
	return fact
}

func TestHostSequenceIsMonotonicAcrossSources(t *testing.T) {
	acceptor := New()
	seen := make(map[uint64]bool)
	last := uint64(0)
	for index, component := range connectivity.Components() {
		acceptance := mustAccept(t, acceptor, seq(component, uint64(index+1)))
		if !acceptance.Accepted() {
			t.Fatalf("%s: outcome %q", component, acceptance.Outcome)
		}
		if acceptance.HostSequence <= last {
			t.Fatalf("%s: host sequence %d did not advance past %d",
				component, acceptance.HostSequence, last)
		}
		if seen[acceptance.HostSequence] {
			t.Fatalf("host sequence %d reused", acceptance.HostSequence)
		}
		seen[acceptance.HostSequence] = true
		last = acceptance.HostSequence
	}
}

func TestExactRetryIsIdempotent(t *testing.T) {
	acceptor := New()
	fact := seq(connectivity.ComponentDNS, 1)
	first := mustAccept(t, acceptor, fact)
	if !first.Accepted() {
		t.Fatalf("first delivery: %q", first.Outcome)
	}
	before := acceptor.State().HostSequence

	second := mustAccept(t, acceptor, fact)
	if second.Outcome != OutcomeDuplicate || second.Reason != ReasonExactRetry {
		t.Fatalf("got %q/%q, want duplicate/exact_retry", second.Outcome, second.Reason)
	}
	if second.HostSequence != 0 {
		t.Fatalf("retry was given host sequence %d", second.HostSequence)
	}
	if after := acceptor.State().HostSequence; after != before {
		t.Fatalf("host sequence moved from %d to %d on a retry", before, after)
	}
}

func TestReusedIdentityWithDifferentContentIsAConflict(t *testing.T) {
	acceptor := New()
	fact := seq(connectivity.ComponentDNS, 1)
	mustAccept(t, acceptor, fact)

	// Same source, boot and sequence; different observation.
	altered := fact
	altered.Lifecycle = connectivity.LifecycleFailed
	altered.Reason = connectivity.ReasonProbeFailed

	acceptance := mustAccept(t, acceptor, altered)
	if acceptance.Outcome != OutcomeConflict || acceptance.Reason != ReasonIdentityReused {
		t.Fatalf("got %q/%q, want conflict/identity_reused", acceptance.Outcome, acceptance.Reason)
	}
	if acceptance.HostSequence != 0 {
		t.Fatal("a conflicting reuse entered the host order")
	}
}

func TestSequenceGapIsRecordedAndSurvivesNonBaselineFacts(t *testing.T) {
	acceptor := New()
	source, _ := connectivity.FixtureSource(connectivity.ComponentDNS)
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 1))

	jumped := mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 5))
	if jumped.OpenedGap == nil {
		t.Fatal("the skipped range was not reported")
	}
	if jumped.OpenedGap.From != 2 || jumped.OpenedGap.To != 4 {
		t.Fatalf("gap %+v, want 2..4", *jumped.OpenedGap)
	}
	if jumped.Reason != ReasonSequenceGap {
		t.Fatalf("reason %q, want sequence_gap", jumped.Reason)
	}

	// An ordinary later fact tells us the current state but says nothing
	// about what was missed, so the hole stays visible.
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 6))
	gaps, known := acceptor.Gaps(source)
	if !known || len(gaps) != 1 {
		t.Fatalf("gaps %+v, want the hole to remain", gaps)
	}
}

func TestOnlyABaselineClearsAGap(t *testing.T) {
	acceptor := New()
	source, _ := connectivity.FixtureSource(connectivity.ComponentDNS)
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 1))
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 4))

	baseline := connectivity.FixtureBaseline(connectivity.ComponentDNS, 5)
	acceptance := mustAccept(t, acceptor, baseline)
	if len(acceptance.ClearedGaps) != 1 {
		t.Fatalf("cleared %+v, want one range", acceptance.ClearedGaps)
	}
	if acceptance.Reason != ReasonBaselineAccepted {
		t.Fatalf("reason %q, want baseline_accepted", acceptance.Reason)
	}
	gaps, _ := acceptor.Gaps(source)
	if len(gaps) != 0 {
		t.Fatalf("gaps %+v remain after a baseline", gaps)
	}
}

func TestLateArrivalInsideAGapDoesNotAdvanceTheOrder(t *testing.T) {
	acceptor := New()
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 1))
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 5))
	before := acceptor.State().HostSequence

	delayed := mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 3))
	if delayed.Outcome != OutcomeStale || delayed.Reason != ReasonBehindWatermark {
		t.Fatalf("got %q/%q, want stale/behind_watermark", delayed.Outcome, delayed.Reason)
	}
	if after := acceptor.State().HostSequence; after != before {
		t.Fatalf("a delayed fact advanced the order from %d to %d", before, after)
	}
}

// A retry older than the remembered window cannot be proven to be either a
// repeat or a reuse, so it is reported as stale rather than guessed.
func TestRetryBeyondTheWindowIsStaleNotConflict(t *testing.T) {
	acceptor := New()
	first := seq(connectivity.ComponentTransports, 1)
	mustAccept(t, acceptor, first)
	for sequence := uint64(2); sequence <= RetryWindow+2; sequence++ {
		mustAccept(t, acceptor, seq(connectivity.ComponentTransports, sequence))
	}
	acceptance := mustAccept(t, acceptor, first)
	if acceptance.Outcome != OutcomeStale || acceptance.Reason != ReasonBeyondRetry {
		t.Fatalf("got %q/%q, want stale/beyond_retry_window",
			acceptance.Outcome, acceptance.Reason)
	}
}

func TestBootChangeStartsANewStreamAwaitingBaseline(t *testing.T) {
	acceptor := New()
	source, _ := connectivity.FixtureSource(connectivity.ComponentRelays)
	mustAccept(t, acceptor, seq(connectivity.ComponentRelays, 9))

	rebooted := seq(connectivity.ComponentRelays, 1)
	rebooted.BootID = "boot-1111111111111111"
	acceptance := mustAccept(t, acceptor, rebooted)
	if !acceptance.Accepted() {
		t.Fatalf("outcome %q, want accepted", acceptance.Outcome)
	}
	if acceptance.Reason != ReasonBootChanged {
		t.Fatalf("reason %q, want boot_changed", acceptance.Reason)
	}
	state := acceptor.State()
	if state.Sources[source].BootID != rebooted.BootID {
		t.Fatal("the source did not move to the new boot")
	}
	if !state.Sources[source].AwaitingBaseline() {
		t.Fatal("a new boot did not require a baseline")
	}
}

func TestCorroboratingFactsAreAcceptedAsEvidenceOnly(t *testing.T) {
	acceptor := New()
	fact := seq(connectivity.ComponentDNS, 1)
	fact.SourceID = "root.probe"

	acceptance := mustAccept(t, acceptor, fact)
	if !acceptance.Accepted() {
		t.Fatalf("corroborating fact was not recorded: %q", acceptance.Outcome)
	}
	if acceptance.Role != safety.RoleCorroborating {
		t.Fatalf("role %q, want corroborating", acceptance.Role)
	}
	// It keeps its own stream, so it can never occupy the owner's sequence.
	owner, _ := connectivity.FixtureSource(connectivity.ComponentDNS)
	if _, tracked := acceptor.Gaps(owner); tracked {
		t.Fatal("a corroborating fact advanced the owner's stream")
	}
}

func TestFactsOutsideOwnershipAreRejected(t *testing.T) {
	acceptor := New()
	tests := []struct {
		name   string
		fact   connectivity.Fact
		domain policy.Domain
		want   error
	}{
		{
			name:   "user fact on the root channel",
			fact:   seq(connectivity.ComponentUserAccess, 1),
			domain: policy.DomainRoot,
			want:   safety.ErrDomainMismatch,
		},
		{
			name: "source claiming another component",
			fact: func() connectivity.Fact {
				fact := seq(connectivity.ComponentDNS, 1)
				fact.SourceID = "root.routes"
				return fact
			}(),
			domain: policy.DomainRoot,
			want:   safety.ErrUnknownSource,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			acceptance, err := acceptor.Accept(test.fact, test.domain)
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if acceptance.Outcome != OutcomeRejected {
				t.Fatalf("outcome %q, want rejected", acceptance.Outcome)
			}
			if acceptor.State().HostSequence != 0 {
				t.Fatal("a rejected fact entered the host order")
			}
		})
	}
}

func TestInvalidFactNeverEntersTheOrder(t *testing.T) {
	acceptor := New()
	fact := seq(connectivity.ComponentDNS, 1)
	fact.EventID = "not-a-uuid"
	acceptance, err := acceptor.Accept(fact, fact.Domain)
	if err == nil {
		t.Fatal("an invalid fact was accepted")
	}
	if acceptance.Outcome != OutcomeRejected || acceptance.Reason != ReasonInvalidFact {
		t.Fatalf("got %q/%q, want rejected/invalid_fact", acceptance.Outcome, acceptance.Reason)
	}
}

// Independent sources publish concurrently; the host order must still be a
// single monotonic sequence with no value used twice.
func TestConcurrentSourcesShareOneOrder(t *testing.T) {
	acceptor := New()
	components := connectivity.Components()
	const perSource = 50

	var group sync.WaitGroup
	results := make([][]uint64, len(components))
	for index, component := range components {
		group.Add(1)
		go func(index int, component connectivity.Component) {
			defer group.Done()
			assigned := make([]uint64, 0, perSource)
			for sequence := uint64(1); sequence <= perSource; sequence++ {
				acceptance, err := acceptor.Accept(seq(component, sequence),
					mustDomain(component))
				if err != nil {
					t.Errorf("%s: %v", component, err)
					return
				}
				if acceptance.Accepted() {
					assigned = append(assigned, acceptance.HostSequence)
				}
			}
			results[index] = assigned
		}(index, component)
	}
	group.Wait()

	seen := make(map[uint64]bool)
	total := 0
	for _, assigned := range results {
		for pair := 1; pair < len(assigned); pair++ {
			if assigned[pair] <= assigned[pair-1] {
				t.Fatalf("a source saw its own order go backwards: %v", assigned)
			}
		}
		for _, value := range assigned {
			if seen[value] {
				t.Fatalf("host sequence %d was assigned twice", value)
			}
			seen[value] = true
			total++
		}
	}
	if state := acceptor.State(); state.HostSequence != uint64(total) {
		t.Fatalf("host sequence %d does not match %d accepted facts",
			state.HostSequence, total)
	}
}

func mustDomain(component connectivity.Component) policy.Domain {
	_, domain := connectivity.FixtureSource(component)
	return domain
}

func TestRestoreRejectsTamperedState(t *testing.T) {
	acceptor := New()
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 1))
	source, _ := connectivity.FixtureSource(connectivity.ComponentDNS)

	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{"nil source", func(s *State) { s.Sources[source] = nil }},
		{"remembered beyond watermark", func(s *State) {
			s.Sources[source].Recent[0].Sequence = s.Sources[source].LastSequence + 1
		}},
		{"inverted gap", func(s *State) {
			s.Sources[source].Gaps = []GapRange{{From: 9, To: 2}}
		}},
		{"zero gap origin", func(s *State) {
			s.Sources[source].Gaps = []GapRange{{From: 0, To: 2}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := acceptor.State()
			test.mutate(&state)
			if _, err := Restore(state); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("got %v, want %v", err, ErrInvalidState)
			}
		})
	}
}

func TestRestoreResumesTheHostOrder(t *testing.T) {
	acceptor := New()
	mustAccept(t, acceptor, seq(connectivity.ComponentDNS, 1))
	mustAccept(t, acceptor, seq(connectivity.ComponentRelays, 1))
	saved := acceptor.State()

	resumed, err := Restore(saved)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	acceptance := mustAccept(t, resumed, seq(connectivity.ComponentDNS, 2))
	if acceptance.HostSequence != saved.HostSequence+1 {
		t.Fatalf("host sequence %d, want %d", acceptance.HostSequence, saved.HostSequence+1)
	}
	// The retry window survived the restore, so an exact retry is still one.
	retry := mustAccept(t, resumed, seq(connectivity.ComponentRelays, 1))
	if retry.Outcome != OutcomeDuplicate {
		t.Fatalf("outcome %q after restore, want duplicate", retry.Outcome)
	}
}
