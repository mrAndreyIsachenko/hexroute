package tunnelplan

import (
	"testing"
	"time"
)

func policy() Policy {
	return Policy{WakeThreshold: 90 * time.Second, PayloadFailures: 2}
}

// A cycle with a previous one behind it and nothing wrong with the machine.
func steady() (State, Observed) {
	carrier := NewSignature([]CarriedDestination{
		{Destination: "203.0.113.20", Interface: "en0"},
		{Destination: "198.51.100.20", Interface: "utun4"},
	})
	return State{Known: true, Carrier: carrier, LinkPresent: true},
		Observed{
			ProcessRunning: true,
			SincePrevious:  10 * time.Second,
			Carrier:        carrier,
			LinkPresent:    true,
			PayloadOK:      true,
		}
}

func causes(plan Plan) map[Cause]bool {
	held := map[Cause]bool{}
	for _, cause := range plan.Causes {
		held[cause] = true
	}
	return held
}

// Each of the six, one at a time. They are the causes the production supervisor
// acts on, measured from its own log: nineteen carrier changes, eleven wake
// gaps, two payload failures, and two that did not occur in sixty-one days and
// are kept because the process exiting is the only thing between this machine
// and no network at all.
func TestEachCauseIsReached(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*State, *Observed)
		cause  Cause
		action Action
	}{
		{
			name:   "the process is gone",
			change: func(_ *State, o *Observed) { o.ProcessRunning = false },
			cause:  CauseProcessGone,
			action: ActionRebuildTunnel,
		},
		{
			name:   "a wake gap",
			change: func(_ *State, o *Observed) { o.SincePrevious = 20 * time.Minute },
			cause:  CauseWakeGap,
			action: ActionRebuildTunnel,
		},
		{
			name: "the carrier changed",
			change: func(_ *State, o *Observed) {
				o.Carrier = NewSignature([]CarriedDestination{
					{Destination: "203.0.113.20", Interface: "en1"},
				})
			},
			cause:  CauseCarrierChanged,
			action: ActionRebuildTunnel,
		},
		{
			name: "connectivity returned",
			change: func(s *State, o *Observed) {
				s.LinkPresent = false
				o.LinkPresent = true
			},
			cause:  CauseLinkReturned,
			action: ActionRebuildTunnel,
		},
		{
			name: "the payload path failed past its threshold",
			change: func(s *State, o *Observed) {
				s.PayloadFailures = 1
				o.PayloadOK = false
			},
			cause:  CausePayloadFailed,
			action: ActionRebuildTunnel,
		},
		{
			name:   "the routes drifted",
			change: func(_ *State, o *Observed) { o.RoutesDrifted = true },
			cause:  CauseRoutesDrifted,
			action: ActionReapplyRoutes,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			previous, observed := steady()
			testCase.change(&previous, &observed)
			plan, _, err := Decide(policy(), previous, observed)
			if err != nil {
				t.Fatalf("Decide() error: %v", err)
			}
			if !causes(plan)[testCase.cause] {
				t.Fatalf("causes = %v, want %q among them", plan.Causes, testCase.cause)
			}
			if plan.Action != testCase.action {
				t.Fatalf("action = %q, want %q", plan.Action, testCase.action)
			}
		})
	}
}

// One failure is a network; several are a path. The supervisor counts, and a
// rule that rebuilt on the first would rebuild on every lost packet.
func TestOnePayloadFailureIsNotACause(t *testing.T) {
	previous, observed := steady()
	observed.PayloadOK = false
	plan, next, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CausePayloadFailed] {
		t.Fatal("one failure was taken as a failed path")
	}
	if next.PayloadFailures != 1 {
		t.Fatalf("failures carried = %d, want 1", next.PayloadFailures)
	}
	plan, next, err = Decide(policy(), next, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if !causes(plan)[CausePayloadFailed] {
		t.Fatalf("the threshold was reached and the cause was not: %v", plan.Causes)
	}
	if next.PayloadFailures != 0 {
		t.Fatalf("the count survived the decision it caused: %d", next.PayloadFailures)
	}
}

// The runtime this is compared against reports one reason. Recording only the
// first cause would make an honest disagreement look like a wrong decision.
func TestEveryCauseThatHeldIsNamed(t *testing.T) {
	previous, observed := steady()
	observed.ProcessRunning = false
	observed.SincePrevious = 20 * time.Minute
	observed.RoutesDrifted = true
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	held := causes(plan)
	for _, want := range []Cause{CauseProcessGone, CauseWakeGap, CauseRoutesDrifted} {
		if !held[want] {
			t.Errorf("causes = %v, want %q among them", plan.Causes, want)
		}
	}
	if plan.Action != ActionRebuildTunnel {
		t.Fatalf("action = %q; a rebuild subsumes reapplying routes", plan.Action)
	}
}

// A decision that agreed and a decision never reached look the same in an empty
// log, so doing nothing is a decision and says so.
func TestDoingNothingIsADecision(t *testing.T) {
	previous, observed := steady()
	plan, next, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if plan.Action != ActionNone {
		t.Fatalf("action = %q, want %q", plan.Action, ActionNone)
	}
	if len(plan.Causes) != 0 {
		t.Fatalf("causes = %v, want none", plan.Causes)
	}
	if !next.Known {
		t.Fatal("the next cycle was left with no previous cycle to compare against")
	}
}

// A runtime that has just started has no signature to differ from. Treating an
// absent one as a change would rebuild the tunnel on every restart.
func TestAFirstCycleDoesNotInventAChange(t *testing.T) {
	_, observed := steady()
	observed.LinkPresent = true
	plan, next, err := Decide(policy(), State{}, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	held := causes(plan)
	if held[CauseCarrierChanged] {
		t.Fatal("a first cycle invented a carrier change")
	}
	if held[CauseLinkReturned] {
		t.Fatal("a first cycle invented a returned link")
	}
	if !next.Known {
		t.Fatal("the first cycle left nothing for the second to compare against")
	}
	if next.Carrier != observed.Carrier {
		t.Fatalf("carrier carried = %q, want %q", next.Carrier, observed.Carrier)
	}
}

func TestAPolicyThatCannotDecideIsRefused(t *testing.T) {
	_, observed := steady()
	for _, broken := range []Policy{
		{WakeThreshold: 0, PayloadFailures: 2},
		{WakeThreshold: 90 * time.Second, PayloadFailures: 0},
		{WakeThreshold: -time.Second, PayloadFailures: 2},
	} {
		if _, _, err := Decide(broken, State{}, observed); err == nil {
			t.Fatalf("Decide() accepted %+v", broken)
		}
	}
}
