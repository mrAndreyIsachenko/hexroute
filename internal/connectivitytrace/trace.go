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
// A trace carries its own policy as well as its facts. What a reduction
// concludes depends on both, so a catalogue that named only the facts would
// let two runs under different policy claim to be the same trace.
//
// Traces are canonically digestible because the qualification chain binds
// results to the trace that produced them. A result that names a trace whose
// content has since changed is a result about something that no longer exists.
package connectivitytrace

import (
	"errors"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
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

// Assertion is the structural form of an expectation.
//
// Expectation.Visible is prose for a person; this is what a runner checks.
// Both are stated here, in the catalogue, before any run: a runner that
// decided for itself what counted as surviving a fault would be confirming the
// implementation against itself, which is the one thing a qualification may
// not do.
//
// A nil field makes no claim. A trace asserts what its own fault is about and
// stays quiet about the rest, so a later change to some unrelated part of the
// model does not fail a trace that was never about it.
type Assertion struct {
	// Acceptance counts are about the injected steps only. The baseline that
	// had to exist first is not part of what the fault did.
	Accepted   *uint16 `json:"accepted,omitempty"`
	Duplicates *uint16 `json:"duplicates,omitempty"`
	Conflicts  *uint16 `json:"conflicts,omitempty"`
	Stale      *uint16 `json:"stale,omitempty"`
	Rejected   *uint16 `json:"rejected,omitempty"`

	// OpenGaps, AwaitingBaseline and SourceConflicts are read off the final
	// snapshot's summary, which is what an operator would be looking at.
	OpenGaps         *uint16 `json:"open_gaps,omitempty"`
	AwaitingBaseline *uint16 `json:"awaiting_baseline,omitempty"`
	SourceConflicts  *uint16 `json:"source_conflicts,omitempty"`
	StaleComponents  *uint16 `json:"stale_components,omitempty"`

	// Aggregate is the operator-facing summary state. Empty makes no claim.
	Aggregate connectivityreduce.AggregateState `json:"aggregate,omitempty"`
	// Authorization and AuthorizationReason are claimed by traces about
	// policy. Empty makes no claim.
	Authorization       connectivityreduce.Authorization       `json:"authorization,omitempty"`
	AuthorizationReason connectivityreduce.AuthorizationReason `json:"authorization_reason,omitempty"`

	// Resume and ResumeReason are the verdict a restart must reach on the
	// stored lineage. Store-layer traces claim both; empty makes no claim.
	Resume       connectivitycheckpoint.ResumeStatus `json:"resume,omitempty"`
	ResumeReason connectivitycheckpoint.ResumeReason `json:"resume_reason,omitempty"`
	// ResumeDepth is how far back the search had to go. It is what separates
	// two faults the store refuses for the same reason: a substituted parent
	// costs one step, a deleted ancestor costs two.
	ResumeDepth *uint16 `json:"resume_depth,omitempty"`
}

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
	// Damaging says this fault must leave a mark. It is stated rather than
	// inferred from the assertion, because it decides whether an untroubled
	// read model is the correct outcome or the one result that voids the run,
	// and that decision must not depend on reading other fields correctly.
	//
	// A duplicate is not damaging: the model is supposed to shrug it off. A
	// hole is, and a model that shrugged one off guessed healthy.
	Damaging bool `json:"damaging"`
	// Assert is the structural form checked by a runner.
	Assert Assertion `json:"assert"`
}

// Step is one thing a trace does.
type Step struct {
	// Fact is delivered when set.
	Fact *connectivity.Fact `json:"fact,omitempty"`
	// Wake marks a full wake before the following facts.
	Wake bool `json:"wake,omitempty"`
	// Restart stops the read model and brings it back up against the same
	// store, under BootID. It is how a reboot is injected: a source claiming
	// a new boot is a source that changed its mind, while a reboot is the
	// host losing every deadline it had issued.
	Restart bool `json:"restart,omitempty"`
	// BootID is the identity the restarted model runs under. It is only read
	// when Restart is set.
	BootID string `json:"boot_id,omitempty"`
	// Policy replaces the active policy from this step onward. A trace that
	// never sets one runs under the trace's opening policy throughout.
	Policy *connectivityreduce.PolicyDescriptor `json:"policy,omitempty"`
	// Tick is the evaluation tick this step reduces at.
	Tick control.Tick `json:"tick"`
	// Injected marks a step as part of the fault rather than of the baseline
	// that had to exist before the fault could mean anything. Acceptance
	// counts are reported for these steps alone.
	Injected bool `json:"injected,omitempty"`
}

// Trace is one named fault and the sequence that injects it.
type Trace struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	Fault Fault `json:"fault"`
	Layer Layer `json:"layer"`
	// BootID is the identity the model opens under.
	BootID string `json:"boot_id"`
	// Policy is the descriptor every reduction runs under until a step
	// replaces it.
	Policy connectivityreduce.PolicyDescriptor `json:"policy"`
	// Components is the per-component policy the desired state derives from.
	Components  []connectivityreduce.ComponentPolicy `json:"components"`
	Steps       []Step                               `json:"steps,omitempty"`
	Expectation Expectation                          `json:"expectation"`
}

const (
	// TraceSchema names the wire contract for a fault trace.
	TraceSchema = "hexroute.connectivity-fault-trace.v1"
	// TraceSchemaVersion is bumped only for an incompatible change.
	//
	// Version 2 made a trace runnable: it carries the policy its reductions
	// run under, marks which of its steps inject the fault, can restart the
	// model, and states its expectation structurally as well as in prose.
	TraceSchemaVersion uint16 = 2
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

func count(value uint16) *uint16 { return &value }

// For returns the canonical trace for one fault.
func For(fault Fault) (Trace, error) {
	trace := Trace{
		Schema: TraceSchema, Version: TraceSchemaVersion, Fault: fault,
		BootID: connectivity.FixtureBootID,
		Policy: authorizedPolicy(baseGeneration), Components: managedComponents(),
	}
	switch fault {
	case FaultDuplicate:
		trace.Layer = LayerFacts
		trace.Steps = duplicateSteps()
		trace.Expectation = Expectation{
			Visible:   "retry recognized as a duplicate, nothing else disturbed",
			Resumable: true,
			Assert: Assertion{
				Accepted: count(1), Duplicates: count(1), Conflicts: count(0),
				Stale: count(0), OpenGaps: count(0), AwaitingBaseline: count(0),
				SourceConflicts: count(0), StaleComponents: count(0),
				Aggregate: connectivityreduce.AggregateReady,
			},
		}
	case FaultReorder:
		trace.Layer = LayerFacts
		trace.Steps = reorderSteps()
		trace.Expectation = Expectation{
			Visible:   "late arrival refused as stale, the hole it belonged to still open",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Accepted: count(1), Stale: count(1), Duplicates: count(0),
				Conflicts: count(0), OpenGaps: count(1),
				AwaitingBaseline: count(1), SourceConflicts: count(0),
				Aggregate: connectivityreduce.AggregateDegraded,
			},
		}
	case FaultGap:
		trace.Layer = LayerFacts
		trace.Steps = gapSteps()
		trace.Expectation = Expectation{
			Visible:   "hole recorded, source awaiting a complete restatement",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Accepted: count(1), Stale: count(0), Duplicates: count(0),
				Conflicts: count(0), OpenGaps: count(1),
				AwaitingBaseline: count(1), SourceConflicts: count(0),
				Aggregate: connectivityreduce.AggregateDegraded,
			},
		}
	case FaultCollectorLoss:
		trace.Layer = LayerFacts
		trace.Steps = collectorLossSteps()
		trace.Expectation = Expectation{
			Visible:   "every component stale on its own deadline, none guessed forward",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				StaleComponents: count(uint16(len(connectivity.Components()))),
				OpenGaps:        count(0), SourceConflicts: count(0),
				Aggregate: connectivityreduce.AggregateDegraded,
			},
		}
	case FaultConflict:
		trace.Layer = LayerFacts
		trace.Steps = conflictSteps()
		trace.Expectation = Expectation{
			Visible:   "conflict recorded, accepted fact kept, restatement owed",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Accepted: count(1), Conflicts: count(1), Duplicates: count(0),
				Stale: count(0), SourceConflicts: count(1),
				AwaitingBaseline: count(1), OpenGaps: count(0),
				Aggregate: connectivityreduce.AggregateDegraded,
			},
		}
	case FaultPolicyChange:
		trace.Layer = LayerFacts
		trace.Steps = policyChangeSteps()
		trace.Expectation = Expectation{
			Visible:   "authorization withdrawn on a rolled-back generation, no desire derived",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Authorization:       connectivityreduce.AuthorizationUnauthorized,
				AuthorizationReason: connectivityreduce.AuthorizationReasonGenerationGap,
			},
		}
	case FaultSleepWake:
		trace.Layer = LayerFacts
		trace.Steps = sleepWakeSteps()
		trace.Expectation = Expectation{
			Visible:   "time-sensitive components stale until restated, route table not invented stale",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				StaleComponents: count(timeSensitiveCount()),
				OpenGaps:        count(0), SourceConflicts: count(0),
				Aggregate: connectivityreduce.AggregateDegraded,
			},
		}
	case FaultReboot:
		trace.Layer = LayerFacts
		trace.Steps = rebootSteps()
		trace.Expectation = Expectation{
			// A reboot invalidates more than a wake does. A wake spares the
			// route table, because a table does not stop being installed
			// because the machine slept. A reboot tears it down with
			// everything else, and every deadline the prior boot issued was
			// measured against a clock that no longer exists.
			Visible:   "every component stale, no deadline from the prior boot honoured",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				StaleComponents: count(uint16(len(connectivity.Components()))),
				Aggregate:       connectivityreduce.AggregateDegraded,
			},
		}
	case FaultParentTamper:
		trace.Layer = LayerStore
		trace.Steps = lineageSteps()
		trace.Expectation = Expectation{
			Visible:   "substituted ancestry refused, an older provable checkpoint resumed",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Resume:       connectivitycheckpoint.ResumeAncestor,
				ResumeReason: connectivitycheckpoint.ResumeReasonParentBroken,
				ResumeDepth:  count(1),
			},
		}
	case FaultOutputTamper:
		trace.Layer = LayerStore
		trace.Steps = lineageSteps()
		trace.Expectation = Expectation{
			// The record is resealed around the rewritten digest, so it
			// verifies on its own terms and only the lineage's record of its
			// address disagrees. A store that checked records alone would
			// resume this one.
			Visible:   "rewritten output digest caught by the lineage, an older provable checkpoint resumed",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Resume:       connectivitycheckpoint.ResumeAncestor,
				ResumeReason: connectivitycheckpoint.ResumeReasonDigestMismatch,
				ResumeDepth:  count(1),
			},
		}
	case FaultMissingAncestor:
		trace.Layer = LayerStore
		trace.Steps = lineageSteps()
		trace.Expectation = Expectation{
			Visible:   "broken ancestry refused, resume falling back past the missing record",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Resume:       connectivitycheckpoint.ResumeAncestor,
				ResumeReason: connectivitycheckpoint.ResumeReasonParentBroken,
				ResumeDepth:  count(2),
			},
		}
	case FaultCheckpointCorruption:
		trace.Layer = LayerStore
		trace.Steps = lineageSteps()
		trace.Expectation = Expectation{
			Visible:   "unreadable checkpoint refused, an older provable checkpoint resumed",
			Resumable: true, Damaging: true,
			Assert: Assertion{
				Resume:       connectivitycheckpoint.ResumeAncestor,
				ResumeReason: connectivitycheckpoint.ResumeReasonRecordInvalid,
				ResumeDepth:  count(1),
			},
		}
	case FaultDepthExhaustion:
		trace.Layer = LayerStore
		trace.Steps = lineageSteps()
		trace.Expectation = Expectation{
			Visible:   "recovery bound exhausted, state published as unknown rather than guessed",
			Damaging:  true,
			Resumable: false,
			Assert: Assertion{
				Resume:       connectivitycheckpoint.ResumeUnrecoverable,
				ResumeReason: connectivitycheckpoint.ResumeReasonDepthExhausted,
			},
		}
	default:
		return Trace{}, ErrUnknownFault
	}
	return trace, nil
}

const (
	baseTick = control.Tick(1000)
	// baseGeneration is the policy generation the model opens under. The
	// rollback trace needs a generation to move away from.
	baseGeneration = uint64(7)
	// RebootBootID is the identity a reboot trace restarts under.
	RebootBootID = "boot-1111111111111111"
)

// injectionSource is the stream every fact-layer fault is injected into. It
// speaks for one component only, so a hole in it owes exactly one restatement
// and the trace stays about the fault rather than about how many components a
// source happens to cover.
const injectionComponent = connectivity.ComponentRelays

// nextSequence is where the injection source's stream continues.
//
// It is read off the opening baseline rather than written down, because a
// number written down beside the fixtures goes stale the first time a
// component is added and turns every fact-layer trace into an accidental gap.
func nextSequence() uint64 {
	high := uint64(0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Component == injectionComponent && fact.SourceSequence > high {
			high = fact.SourceSequence
		}
	}
	return high + 1
}

// authorizedPolicy is a policy that permits a desired state to be derived.
func authorizedPolicy(generation uint64) connectivityreduce.PolicyDescriptor {
	return connectivityreduce.PolicyDescriptor{
		Present: true, Valid: true, Suspended: false,
		BundleGeneration: generation,
		RootGeneration:   generation,
		UserGeneration:   generation,
		ManifestDigest:   "0000000000000000000000000000000000000000000000000000000000000001",
	}
}

// managedComponents asks for every component to be ready and pins nothing.
// A pin would make the trace about whether a fixture satisfied it, when the
// trace is about what a fault does to the model.
func managedComponents() []connectivityreduce.ComponentPolicy {
	components := connectivity.Components()
	policies := make([]connectivityreduce.ComponentPolicy, 0, len(components))
	for _, component := range components {
		policies = append(policies, connectivityreduce.ComponentPolicy{
			Component: component, Managed: true,
			Expect: connectivityreduce.Expectation{
				Lifecycle: connectivity.LifecycleReady,
			},
		})
	}
	return policies
}

func timeSensitiveCount() uint16 {
	total := uint16(0)
	for _, component := range connectivity.Components() {
		if component.TimeSensitive() {
			total++
		}
	}
	return total
}

// baselineSteps restates every component in full, which is where every trace
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
	repeated := observation(injectionComponent, nextSequence(), baseTick)
	steps = append(steps,
		Step{Fact: &repeated, Tick: baseTick, Injected: true},
		Step{Fact: &repeated, Tick: baseTick, Injected: true},
	)
	return steps
}

func reorderSteps() []Step {
	steps := baselineSteps()
	later := observation(injectionComponent, nextSequence()+1, baseTick)
	earlier := observation(injectionComponent, nextSequence(), baseTick)
	steps = append(steps,
		Step{Fact: &later, Tick: baseTick, Injected: true},
		Step{Fact: &earlier, Tick: baseTick, Injected: true},
	)
	return steps
}

func gapSteps() []Step {
	steps := baselineSteps()
	skipped := observation(injectionComponent, nextSequence()+1, baseTick)
	return append(steps, Step{Fact: &skipped, Tick: baseTick, Injected: true})
}

func collectorLossSteps() []Step {
	// Nothing more arrives; the later tick is past every deadline the
	// baseline issued, so the components go stale on their own evidence.
	return append(baselineSteps(), Step{Tick: baseTick + 5000, Injected: true})
}

func conflictSteps() []Step {
	steps := baselineSteps()
	first := observation(injectionComponent, nextSequence(), baseTick)
	clash := first
	clash.Lifecycle = connectivity.LifecycleFailed
	clash.Reason = connectivity.ReasonProbeFailed
	steps = append(steps,
		Step{Fact: &first, Tick: baseTick, Injected: true},
		Step{Fact: &clash, Tick: baseTick, Injected: true},
	)
	return steps
}

// policyChangeSteps rolls the generation backwards under a model that has
// already reduced under the newer one. A generation that moves forward is an
// ordinary update; one that moves back means the compiled policy the model was
// authorized by is no longer the one in force.
func policyChangeSteps() []Step {
	rolledBack := authorizedPolicy(baseGeneration - 1)
	return append(baselineSteps(),
		Step{Policy: &rolledBack, Tick: baseTick + 1, Injected: true})
}

func sleepWakeSteps() []Step {
	return append(baselineSteps(),
		Step{Wake: true, Tick: baseTick + 10, Injected: true})
}

// rebootSteps restarts the model under a new boot identity. Every deadline the
// prior boot issued was measured against a clock that no longer exists.
func rebootSteps() []Step {
	return append(baselineSteps(),
		Step{Restart: true, BootID: RebootBootID, Tick: baseTick + 20, Injected: true})
}

// lineageSteps produces a lineage long enough to damage.
//
// Store faults need ancestors: removing one, or breaking more links than
// recovery may walk back, means nothing on a store holding a single
// checkpoint. Each observation advances one component, which is an effective
// change, which is what the store records.
func lineageSteps() []Step {
	steps := baselineSteps()
	for index := uint64(0); index < lineageDepth; index++ {
		fact := observation(
			injectionComponent, nextSequence()+index,
			baseTick+control.Tick(index))
		if index%2 == 0 {
			fact.Lifecycle = connectivity.LifecycleDegraded
			fact.Reason = connectivity.ReasonProbeFailed
		}
		steps = append(steps, Step{
			Fact: &fact, Tick: baseTick + control.Tick(index), Injected: true,
		})
	}
	return steps
}

// lineageDepth is one more than the recovery bound, so a trace that breaks
// every candidate the search may consider still has an intact ancestor
// underneath it — except the one trace that is about running out.
const lineageDepth = uint64(connectivitycheckpoint.DefaultMaxRecoveryDepth + 2)

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
