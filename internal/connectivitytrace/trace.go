// Package connectivitytrace holds the fault traces a shadow qualification is
// run against.
//
// A soak that only sees a healthy host proves that a healthy host produces no
// surprises. The gate wants more than that: it wants each way the read model
// can be lied to, starved or interrupted, exercised deliberately, with the
// outcome named in advance so a run either matches it or does not.
//
// Every trace is synthetic. Nothing here reaches a network, a process or a
// route: a trace is a sequence of facts and interruptions, and the strongest
// statement about it is that replaying one cannot change the host.
//
// Traces are canonically digestible because the qualification chain binds
// results to the trace that produced them. A result that names a trace whose
// content has since changed is a result about something that no longer exists.
package connectivitytrace

import (
	"errors"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// Fault names the condition a trace injects.
type Fault string

const (
	// FaultDuplicate replays an accepted fact byte for byte.
	FaultDuplicate Fault = "duplicate"
	// FaultReorder delivers a source's facts out of their own order.
	FaultReorder Fault = "reorder"
	// FaultGap skips a source sequence, leaving a hole behind.
	FaultGap Fault = "gap"
	// FaultCollectorLoss stops a source entirely and lets its components
	// pass their freshness deadlines.
	FaultCollectorLoss Fault = "collector_loss"
	// FaultConflict reuses an accepted identity with different content.
	FaultConflict Fault = "conflict"
	// FaultParentTamper rewrites a checkpoint's parent link.
	FaultParentTamper Fault = "parent_tamper"
	// FaultOutputTamper rewrites a checkpoint's recorded output digest.
	FaultOutputTamper Fault = "output_tamper"
	// FaultMissingAncestor removes a checkpoint the lineage depends on.
	FaultMissingAncestor Fault = "missing_ancestor"
	// FaultDepthExhaustion breaks more links than recovery may search back.
	FaultDepthExhaustion Fault = "recovery_depth_exhaustion"
	// FaultCheckpointCorruption leaves a checkpoint unreadable.
	FaultCheckpointCorruption Fault = "checkpoint_corruption"
	// FaultPolicyChange moves the active policy generation under the model.
	FaultPolicyChange Fault = "policy_change"
	// FaultSleepWake withdraws the assumption that observations survived.
	FaultSleepWake Fault = "sleep_wake"
	// FaultReboot changes the boot identity, invalidating every monotonic
	// deadline issued under the old one.
	FaultReboot Fault = "reboot"
)

// Faults returns every fault a qualification must inject, in fixed order.
//
// The gate names these; a run that skipped one would be a run that qualified
// something narrower than the gate describes.
func Faults() []Fault {
	return []Fault{
		FaultDuplicate, FaultReorder, FaultGap, FaultCollectorLoss,
		FaultConflict, FaultParentTamper, FaultOutputTamper,
		FaultMissingAncestor, FaultDepthExhaustion, FaultCheckpointCorruption,
		FaultPolicyChange, FaultSleepWake, FaultReboot,
	}
}

// Layer is where a fault is injected.
//
// It matters because the two layers fail differently: a fault in the fact
// stream is something the acceptor and reducer must describe, while a fault in
// the store is something startup must refuse to resume from.
type Layer string

const (
	// LayerFacts injects into the accepted-fact stream.
	LayerFacts Layer = "facts"
	// LayerStore injects into the durable lineage.
	LayerStore Layer = "store"
)

// Expectation is what a trace must produce.
//
// It is stated before the run rather than read off it. A qualification that
// recorded whatever happened would confirm the implementation against itself.
type Expectation struct {
	// Visible is the condition the read model must report. A fault that
	// produces a correct-looking snapshot has not been survived; it has been
	// hidden.
	Visible string `json:"visible"`
	// Resumable says whether a daemon restarting after this fault may resume
	// the lineage at all.
	Resumable bool `json:"resumable"`
	// GuessedHealthy must always be false. It is stated explicitly because it
	// is the one outcome that would make every other number meaningless.
	GuessedHealthy bool `json:"guessed_healthy"`
}

// Step is one thing a trace does.
type Step struct {
	// Fact is delivered when set.
	Fact *connectivity.Fact `json:"fact,omitempty"`
	// Wake marks a full wake before the following facts.
	Wake bool `json:"wake,omitempty"`
	// Tick is the evaluation tick this step reduces at.
	Tick control.Tick `json:"tick"`
}

// Trace is one named fault and the sequence that injects it.
type Trace struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	Fault       Fault       `json:"fault"`
	Layer       Layer       `json:"layer"`
	Steps       []Step      `json:"steps,omitempty"`
	Expectation Expectation `json:"expectation"`
}

const (
	// TraceSchema names the wire contract for a fault trace.
	TraceSchema = "hexroute.connectivity-fault-trace.v1"
	// TraceSchemaVersion is bumped only for an incompatible change.
	TraceSchemaVersion uint16 = 1
)

var ErrUnknownFault = errors.New("no canonical trace for this fault")

// Digest addresses a trace by its content, so a result can name the trace it
// came from and a changed trace cannot masquerade as the one that ran.
func (trace Trace) Digest() (string, error) {
	digest, _, err := policy.CanonicalSHA256(trace)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// Canonical returns every fault trace, in the fixed order of Faults.
func Canonical() ([]Trace, error) {
	traces := make([]Trace, 0, len(Faults()))
	for _, fault := range Faults() {
		trace, err := For(fault)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, nil
}

// For returns the canonical trace for one fault.
func For(fault Fault) (Trace, error) {
	trace := Trace{Schema: TraceSchema, Version: TraceSchemaVersion, Fault: fault}
	switch fault {
	case FaultDuplicate:
		trace.Layer = LayerFacts
		trace.Steps = duplicateSteps()
		trace.Expectation = Expectation{
			Visible: "retry accepted as duplicate, snapshot generation unchanged", Resumable: true,
		}
	case FaultReorder:
		trace.Layer = LayerFacts
		trace.Steps = reorderSteps()
		trace.Expectation = Expectation{
			Visible: "late arrival stale, accepted order unchanged", Resumable: true,
		}
	case FaultGap:
		trace.Layer = LayerFacts
		trace.Steps = gapSteps()
		trace.Expectation = Expectation{
			Visible: "hole recorded, source awaiting a complete restatement", Resumable: true,
		}
	case FaultCollectorLoss:
		trace.Layer = LayerFacts
		trace.Steps = collectorLossSteps()
		trace.Expectation = Expectation{
			Visible: "components stale on their own deadlines", Resumable: true,
		}
	case FaultConflict:
		trace.Layer = LayerFacts
		trace.Steps = conflictSteps()
		trace.Expectation = Expectation{
			Visible: "conflict recorded, accepted fact kept", Resumable: true,
		}
	case FaultPolicyChange:
		trace.Layer = LayerFacts
		trace.Steps = baselineSteps()
		trace.Expectation = Expectation{
			Visible: "proposals bound to the prior generation are stale", Resumable: true,
		}
	case FaultSleepWake:
		trace.Layer = LayerFacts
		trace.Steps = sleepWakeSteps()
		trace.Expectation = Expectation{
			Visible: "time-sensitive components stale until restated", Resumable: true,
		}
	case FaultReboot:
		trace.Layer = LayerFacts
		trace.Steps = rebootSteps()
		trace.Expectation = Expectation{
			Visible: "prior boot freshness invalidated", Resumable: true,
		}
	case FaultParentTamper, FaultOutputTamper, FaultMissingAncestor,
		FaultCheckpointCorruption:
		trace.Layer = LayerStore
		trace.Steps = baselineSteps()
		trace.Expectation = Expectation{
			Visible: "lineage refused, an older provable ancestor resumed", Resumable: true,
		}
	case FaultDepthExhaustion:
		trace.Layer = LayerStore
		trace.Steps = baselineSteps()
		trace.Expectation = Expectation{
			Visible: "recovery bound exhausted, state unknown", Resumable: false,
		}
	default:
		return Trace{}, ErrUnknownFault
	}
	return trace, nil
}

// baseline restates every component in full, which is where every trace
// starts: a fault injected into a model that never had a whole picture would
// be indistinguishable from the missing picture.
func baselineSteps() []Step {
	facts := connectivity.FixtureBaselineSet()
	steps := make([]Step, 0, len(facts))
	for index := range facts {
		fact := facts[index]
		steps = append(steps, Step{Fact: &fact, Tick: baseTick})
	}
	return steps
}

const baseTick = control.Tick(1000)

func observation(
	component connectivity.Component,
	sequence uint64,
	tick control.Tick,
) connectivity.Fact {
	fact := connectivity.FixtureBaseline(component, sequence)
	fact.Baseline = false
	fact.Reason = connectivity.ReasonProbeSucceeded
	fact.MonotonicTick = tick
	fact.FreshnessDeadline = tick + 300
	return fact
}

func duplicateSteps() []Step {
	steps := baselineSteps()
	repeated := observation(connectivity.ComponentRelays, 20, baseTick)
	steps = append(steps,
		Step{Fact: &repeated, Tick: baseTick},
		Step{Fact: &repeated, Tick: baseTick},
	)
	return steps
}

func reorderSteps() []Step {
	steps := baselineSteps()
	later := observation(connectivity.ComponentRelays, 25, baseTick)
	earlier := observation(connectivity.ComponentRelays, 24, baseTick)
	steps = append(steps,
		Step{Fact: &later, Tick: baseTick},
		Step{Fact: &earlier, Tick: baseTick},
	)
	return steps
}

func gapSteps() []Step {
	steps := baselineSteps()
	skipped := observation(connectivity.ComponentRelays, 30, baseTick)
	steps = append(steps, Step{Fact: &skipped, Tick: baseTick})
	return steps
}

func collectorLossSteps() []Step {
	// Nothing more arrives; the later tick is past every deadline the
	// baseline issued, so the components go stale on their own evidence.
	return append(baselineSteps(), Step{Tick: baseTick + 5000})
}

func conflictSteps() []Step {
	steps := baselineSteps()
	first := observation(connectivity.ComponentRelays, 40, baseTick)
	clash := first
	clash.Lifecycle = connectivity.LifecycleFailed
	clash.Reason = connectivity.ReasonProbeFailed
	steps = append(steps,
		Step{Fact: &first, Tick: baseTick},
		Step{Fact: &clash, Tick: baseTick},
	)
	return steps
}

func sleepWakeSteps() []Step {
	return append(baselineSteps(), Step{Wake: true, Tick: baseTick + 10})
}

func rebootSteps() []Step {
	steps := baselineSteps()
	rebooted := connectivity.FixtureBaseline(connectivity.ComponentRelays, 1)
	rebooted.BootID = "boot-1111111111111111"
	steps = append(steps, Step{Fact: &rebooted, Tick: baseTick + 20})
	return steps
}

// Digests returns each canonical trace's digest, keyed by fault.
func Digests() (map[Fault]string, error) {
	traces, err := Canonical()
	if err != nil {
		return nil, err
	}
	digests := make(map[Fault]string, len(traces))
	for _, trace := range traces {
		digest, err := trace.Digest()
		if err != nil {
			return nil, err
		}
		digests[trace.Fault] = digest
	}
	return digests, nil
}

// Sorted returns the faults a map covers, in fixed order, so a report reads
// the same way twice.
func Sorted(faults []Fault) []Fault {
	out := append([]Fault(nil), faults...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
