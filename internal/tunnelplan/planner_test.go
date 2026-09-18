package tunnelplan

import (
	"errors"
	"testing"
	"time"
)

// The values the runtime this rule reproduces runs with: a sixty-second tick and
// a wake gap at 180 seconds.
func policy() Policy {
	return Policy{
		Interval: 60 * time.Second, WakeThreshold: 180 * time.Second,
		PayloadFailures: 2, LinkFailures: 2,
	}
}

// A cycle with a previous one behind it and nothing wrong with the machine.
func steady() (State, Observed) {
	carrier := NewSignature([]CarriedDestination{
		{Destination: "203.0.113.20", Interface: "en0"},
		{Destination: "198.51.100.20", Interface: "utun4"},
	})
	return State{Known: true, Carrier: carrier, LinkPresent: true},
		Observed{
			Complete:        true,
			ProcessObserved: true,
			ProcessRunning:  true,
			TickGap:         time.Minute,
			Carrier:         carrier,
			LinkPresent:     true,
			PayloadOK:       true,
		}
}

func causes(plan Plan) map[Cause]bool {
	held := map[Cause]bool{}
	for _, cause := range plan.Causes {
		held[cause] = true
	}
	return held
}

// The three causes that act, one at a time.
func TestEachActingCauseIsReached(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*State, *Observed)
		cause  Cause
	}{
		{"the process is gone", func(_ *State, o *Observed) { o.ProcessRunning = false }, CauseProcessGone},
		{"a wake gap", func(_ *State, o *Observed) { o.TickGap = 20 * time.Minute }, CauseWakeGap},
		{"the carrier changed", func(_ *State, o *Observed) {
			o.Carrier = NewSignature([]CarriedDestination{{Destination: "203.0.113.20", Interface: "en1"}})
		}, CauseCarrierChanged},
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
			if plan.Action != ActionRebuildTunnel {
				t.Fatalf("action = %q, want a rebuild", plan.Action)
			}
		})
	}
}

// The wake gap is the wall time between two cycles, compared inclusively.
//
// The runtime this rule reproduces reads the wall clock after each sixty-second
// sleep and rebuilds when two readings are 180 seconds apart.
func TestAWakeGapIsTheTickGapInclusive(t *testing.T) {
	for _, item := range []struct {
		tickGap time.Duration
		gap     bool
	}{
		{time.Minute, false},
		{179 * time.Second, false},
		{180 * time.Second, true},
		{181 * time.Second, true},
	} {
		previous, observed := steady()
		observed.TickGap = item.tickGap
		plan, _, err := Decide(policy(), previous, observed)
		if err != nil {
			t.Fatalf("Decide() error: %v", err)
		}
		if causes(plan)[CauseWakeGap] != item.gap {
			t.Fatalf("tick gap %s: wake gap named = %v, want %v", item.tickGap, !item.gap, item.gap)
		}
		if plan.Grounds.TickGap != item.tickGap {
			t.Fatalf("tick gap %s: recorded %s", item.tickGap, plan.Grounds.TickGap)
		}
	}
}

// A sleep the steady clock measured decides nothing by itself.
//
// That clock did not stop across the idle sleeps of 2026-09-14, so what it
// measures is kept as a ground and the wall clock decides.
func TestAMeasuredSleepAloneIsNotAWakeGap(t *testing.T) {
	previous, observed := steady()
	observed.Slept = 20 * time.Minute
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CauseWakeGap] {
		t.Fatalf("a sleep with a one-minute tick gap named a wake: %v", plan.Causes)
	}
	if plan.Grounds.Slept != 20*time.Minute {
		t.Fatalf("the measured sleep was not kept as a ground: %s", plan.Grounds.Slept)
	}
}

func TestANegativeTickGapIsRefused(t *testing.T) {
	previous, observed := steady()
	observed.TickGap = -time.Second
	if _, _, err := Decide(policy(), previous, observed); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a negative tick gap gave %v, want %v", err, ErrInvalidInput)
	}
}

// What no longer acts is still seen, and decides nothing.
//
// The runtime this rule reproduces does not rebuild on a returned link, rebuilds
// on the payload path only after a failover this runtime does not perform, and
// keeps its own routes. Acting on them decided 125 rebuilds where it made 2.
func TestWhatNoLongerActsIsRecordedAndDecidesNothing(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*State, *Observed)
		ground func(Grounds) bool
	}{
		{"connectivity returned",
			func(s *State, o *Observed) { s.LinkPresent = false; o.LinkPresent = true },
			func(g Grounds) bool { return g.LinkPresent }},
		{"the payload path failed past its threshold",
			func(s *State, o *Observed) { s.PayloadFailures = 1; o.PayloadOK = false },
			func(g Grounds) bool { return !g.PayloadOK && g.PayloadFailures == 2 }},
		{"the routes drifted",
			func(_ *State, o *Observed) { o.RoutesDrifted = true },
			func(g Grounds) bool { return g.RoutesDrifted }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			previous, observed := steady()
			testCase.change(&previous, &observed)
			plan, _, err := Decide(policy(), previous, observed)
			if err != nil {
				t.Fatalf("Decide() error: %v", err)
			}
			if plan.Action != ActionNone || len(plan.Causes) != 0 {
				t.Fatalf("it decided %q for %v, want nothing", plan.Action, plan.Causes)
			}
			if !testCase.ground(plan.Grounds) {
				t.Fatalf("it is not in the grounds: %+v", plan.Grounds)
			}
		})
	}
}

// The payload count is consecutive and stands until the path answers.
func TestPayloadFailuresAreCountedUntilThePathAnswers(t *testing.T) {
	previous, observed := steady()
	failing := observed
	failing.PayloadOK = false

	plans, state := run(t, previous, []Observed{failing, failing, failing})
	for index, want := range []uint32{1, 2, 3} {
		if plans[index].Grounds.PayloadFailures != want {
			t.Fatalf("cycle %d reported %d failures, want %d", index, plans[index].Grounds.PayloadFailures, want)
		}
	}
	plans, _ = run(t, state, []Observed{observed})
	if plans[0].Grounds.PayloadFailures != 0 {
		t.Fatalf("a path that answered still counts %d failures", plans[0].Grounds.PayloadFailures)
	}
}

// Every acting cause that held is named, and nothing that no longer acts.
func TestEveryActingCauseThatHeldIsNamed(t *testing.T) {
	previous, observed := steady()
	observed.ProcessRunning = false
	observed.TickGap = 20 * time.Minute
	observed.RoutesDrifted = true
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	held := causes(plan)
	if !held[CauseProcessGone] || !held[CauseWakeGap] {
		t.Fatalf("causes = %v, want the process and the gap", plan.Causes)
	}
	if held[CauseRoutesDrifted] {
		t.Fatalf("drifted routes were named: %v", plan.Causes)
	}
	if plan.Action != ActionRebuildTunnel {
		t.Fatalf("action = %q", plan.Action)
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
	if plan.Action != ActionNone || len(plan.Causes) != 0 {
		t.Fatalf("plan = %+v, want nothing", plan)
	}
	if !next.Known {
		t.Fatal("the next cycle was left with no previous cycle to compare against")
	}
}

// A runtime that has just started has no signature to differ from.
func TestAFirstCycleDoesNotInventAChange(t *testing.T) {
	_, observed := steady()
	plan, next, err := Decide(policy(), State{}, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CauseCarrierChanged] {
		t.Fatal("a first cycle invented a carrier change")
	}
	if !next.Known || next.Carrier != observed.Carrier {
		t.Fatalf("the first cycle left %+v for the second", next)
	}
}

func TestAPolicyThatCannotDecideIsRefused(t *testing.T) {
	_, observed := steady()
	for _, broken := range []Policy{
		{Interval: 60 * time.Second, WakeThreshold: 0, PayloadFailures: 2, LinkFailures: 2},
		{Interval: 60 * time.Second, WakeThreshold: 180 * time.Second, PayloadFailures: 0, LinkFailures: 2},
		{Interval: 60 * time.Second, WakeThreshold: 180 * time.Second, PayloadFailures: 2, LinkFailures: 0},
		{Interval: 0, WakeThreshold: 180 * time.Second, PayloadFailures: 2, LinkFailures: 2},
		// A threshold at or below one interval names a gap on every cycle.
		{Interval: 60 * time.Second, WakeThreshold: 60 * time.Second, PayloadFailures: 2, LinkFailures: 2},
		{Interval: 60 * time.Second, WakeThreshold: 30 * time.Second, PayloadFailures: 2, LinkFailures: 2},
	} {
		if _, _, err := Decide(broken, State{}, observed); err == nil {
			t.Fatalf("Decide() accepted %+v", broken)
		}
	}
}

// A cycle that stopped before it saw the routes has no carrier signature, and an
// empty one is not a changed one.
func TestAnIncompleteCycleDecidesOnlyFromWhatItSaw(t *testing.T) {
	previous, observed := steady()
	observed.Complete = false
	observed.Carrier = ""
	observed.LinkPresent = false
	observed.ProcessRunning = false

	plan, next, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	held := causes(plan)
	if !held[CauseProcessGone] {
		t.Fatalf("an incomplete cycle lost the one cause it could see: %v", plan.Causes)
	}
	if held[CauseCarrierChanged] {
		t.Fatal("an incomplete cycle read a carrier change out of what it did not see")
	}
	if next.Carrier != previous.Carrier || next.LinkPresent != previous.LinkPresent {
		t.Fatalf("what it could not see was overwritten: %+v", next)
	}
}

// A complete cycle after an incomplete one compares against the last cycle that
// actually saw something.
func TestACompleteCycleComparesAgainstTheLastOneThatSaw(t *testing.T) {
	previous, observed := steady()
	blind := observed
	blind.Complete = false
	blind.Carrier = ""
	_, afterBlind, err := Decide(policy(), previous, blind)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	plan, _, err := Decide(policy(), afterBlind, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CauseCarrierChanged] {
		t.Fatal("a blind cycle in between manufactured a carrier change")
	}
}

// The belief about the link is still kept, because it is recorded: one failed
// probe does not change it, two do, and the first answer brings it back.
func TestTheLinkBeliefNeedsTwoFailures(t *testing.T) {
	previous, observed := steady()
	gone := observed
	gone.LinkPresent = false

	_, afterOne := run(t, previous, []Observed{gone})
	if !afterOne.LinkPresent {
		t.Fatal("one failed probe changed the belief")
	}
	_, afterTwo := run(t, previous, []Observed{gone, gone})
	if afterTwo.LinkPresent {
		t.Fatal("two failed probes did not")
	}
	plans, back := run(t, afterTwo, []Observed{observed})
	if !back.LinkPresent || back.LinkFailures != 0 {
		t.Fatalf("the first answer did not bring the link back: %+v", back)
	}
	if plans[0].Action != ActionNone {
		t.Fatalf("the link coming back decided %q", plans[0].Action)
	}
}

// A cycle that never reached the probes did not fail them.
func TestAnIncompleteCycleDoesNotCountAgainstTheLink(t *testing.T) {
	previous, observed := steady()
	gone := observed
	gone.LinkPresent = false
	blind := observed
	blind.Complete = false
	blind.LinkPresent = false

	_, state := run(t, previous, []Observed{gone, blind, blind, blind})
	if state.LinkFailures != 1 || !state.LinkPresent {
		t.Fatalf("blind cycles counted against the link: %+v", state)
	}
}

// A start that has seen nothing believes the link present.
func TestAStartBelievesTheLinkPresent(t *testing.T) {
	_, observed := steady()
	gone := observed
	gone.LinkPresent = false
	_, state := run(t, State{}, []Observed{gone})
	if !state.LinkPresent {
		t.Fatal("a fresh start took its first failed probe as an absent link")
	}
}

// run drives a sequence of cycles, carrying the state the way a runtime does.
func run(t *testing.T, previous State, cycles []Observed) ([]Plan, State) {
	t.Helper()
	plans := make([]Plan, 0, len(cycles))
	for index, observed := range cycles {
		plan, next, err := Decide(policy(), previous, observed)
		if err != nil {
			t.Fatalf("cycle %d: %v", index, err)
		}
		plans = append(plans, plan)
		previous = next
	}
	return plans, previous
}

// A process the owner replaced between two cycles is a process that went.
func TestAReplacedProcessIsGone(t *testing.T) {
	previous, observed := steady()
	observed.ProcessReplaced = true
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if !causes(plan)[CauseProcessGone] || plan.Action != ActionRebuildTunnel || !plan.Grounds.ProcessReplaced {
		t.Fatalf("a replaced process decided %v, %q, grounds %+v", plan.Causes, plan.Action, plan.Grounds)
	}
	previous, observed = steady()
	plan, _, err = Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CauseProcessGone] {
		t.Fatalf("the same process running decided a loss: %v", plan.Causes)
	}
}

// A cycle that could not look at the tunnel has not seen it gone.
//
// Reading "not running" out of an observation never taken named a loss on every
// cycle a machine spent in dark wake: measured on the night of 2026-09-18,
// eight losses reported and none of them real.
func TestAProcessNobodyLookedAtIsNotGone(t *testing.T) {
	previous, observed := steady()
	observed.ProcessObserved, observed.ProcessRunning = false, false
	plan, _, err := Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if causes(plan)[CauseProcessGone] {
		t.Fatalf("a process nobody looked at was named gone: %v", plan.Causes)
	}
	observed.ProcessObserved = true
	plan, _, err = Decide(policy(), previous, observed)
	if err != nil {
		t.Fatalf("Decide() error: %v", err)
	}
	if !causes(plan)[CauseProcessGone] || !plan.Grounds.ProcessObserved {
		t.Fatalf("a process seen to be gone decided %v, grounds %+v", plan.Causes, plan.Grounds)
	}
}
