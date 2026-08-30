package connectivityreduce

import (
	"fmt"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// Event pairs one arrival with what the acceptor decided about it.
//
// Rejected arrivals never reach the reducer: they were not part of the host
// order and have nothing to contribute. Conflicts and stale arrivals do reach
// it, because refusing an arrival is itself something the snapshot must show.
type Event struct {
	Acceptance connectivityaccept.Acceptance
	Fact       connectivity.Fact
}

// Input is everything a reduction is allowed to see.
type Input struct {
	// Prior is nil only for the first reduction on a host.
	Prior  *Snapshot
	Events []Event
	Policy PolicyDescriptor
	// PolicyComponents is what the active policy asks of each component. It
	// is empty when policy manages nothing, which is not the same as policy
	// being absent.
	PolicyComponents []ComponentPolicy
	// BootID and EvaluationTick are the time context. They are supplied
	// rather than read so that reduction stays pure and replayable.
	BootID string
	// PriorConsumed is the host sequence already accounted for when there is
	// no prior snapshot to read it from.
	//
	// A read model that lost its lineage starts from an empty snapshot, but
	// not from an empty order: the journals still hold the sequences this
	// host issued, and the acceptor continues above them rather than minting
	// a second fact for a position already taken. Without this the first
	// reduction after such a restart would demand sequence one and refuse the
	// stream for ever.
	//
	// It is ignored when Prior is set, and contradicting a prior snapshot is
	// refused: a caller may not move a watermark the snapshot already states.
	PriorConsumed  uint64
	EvaluationTick control.Tick
	// Wake, when set, reports that the host resumed from sleep since the
	// prior reduction. It carries no state of its own: what it does is
	// withdraw the assumption that anything observed before it still holds.
	Wake *Wake
}

// Wake is a full-wake transition.
type Wake struct {
	// Tick is the monotonic tick the wake was observed at, in the same boot
	// as the reduction that carries it.
	Tick control.Tick
}

// Output is the result of one reduction.
type Output struct {
	Snapshot Snapshot
	Desired  DesiredState
	Diff     Diff
	// Proposals are descriptions of divergence. Nothing in this build can
	// execute one.
	Proposals []Proposal
	// Changed reports whether the reduction was semantically effective. A
	// no-op leaves the generation where it was.
	Changed bool
}

// Reduce folds an ordered batch of arrivals into a new snapshot.
//
// It performs no I/O, reads no clock and calls no network, process, route,
// DNS or credential API. Equal inputs produce equal canonical outputs.
func Reduce(input Input) (Output, error) {
	if err := validateInput(input); err != nil {
		return Output{}, err
	}

	components := newComponentTable(input.Prior)
	sources := newSourceTable(input.Prior)
	markRebaseline(components, input)
	conflicts := newConflictTable(input.Prior)
	consumed := input.PriorConsumed
	generation := uint64(0)
	if input.Prior != nil {
		consumed = input.Prior.ConsumedHostSequence
		generation = input.Prior.Generation
	}

	for _, event := range input.Events {
		switch event.Acceptance.Outcome {
		case connectivityaccept.OutcomeAccepted:
			if event.Acceptance.HostSequence != consumed+1 {
				return Output{}, fmt.Errorf("%w: expected %d, got %d",
					ErrOutOfOrder, consumed+1, event.Acceptance.HostSequence)
			}
			consumed = event.Acceptance.HostSequence
			applyAccepted(components, sources, conflicts, event)
		case connectivityaccept.OutcomeConflict:
			applyConflict(sources, conflicts, event)
		case connectivityaccept.OutcomeDuplicate, connectivityaccept.OutcomeStale:
			// Neither adds information nor removes any: the accepted state
			// already covers a retry, and a late arrival is behind it.
		default:
			return Output{}, fmt.Errorf("%w: outcome %q",
				ErrInvalidInput, event.Acceptance.Outcome)
		}
	}

	snapshot := Snapshot{
		Schema:               SnapshotSchema,
		Version:              SnapshotSchemaVersion,
		ReducerID:            ReducerID,
		ReducerVersion:       ReducerVersion,
		BootID:               input.BootID,
		EvaluationTick:       input.EvaluationTick,
		Policy:               input.Policy,
		ConsumedHostSequence: consumed,
		Components:           renderComponents(components, sources, input),
		Sources:              renderSources(sources),
	}
	records, conflictOverflow := renderConflicts(conflicts)
	snapshot.Conflicts = records
	snapshot.Authorization, snapshot.Reason = authorize(input.Policy, input.Prior)
	snapshot.Summary = summarize(snapshot, conflictOverflow)

	changed, err := effective(input.Prior, snapshot)
	if err != nil {
		return Output{}, err
	}
	if changed || input.Prior == nil {
		generation++
	}
	snapshot.Generation = generation

	if err := snapshot.Validate(); err != nil {
		return Output{}, err
	}

	desired, err := Desire(EffectivePolicy{
		Descriptor: input.Policy, Components: input.PolicyComponents,
	})
	if err != nil {
		return Output{}, err
	}
	diff := Compare(snapshot, desired, input.Prior)
	proposals, err := Propose(snapshot, diff)
	if err != nil {
		return Output{}, err
	}

	return Output{
		Snapshot:  snapshot,
		Desired:   desired,
		Diff:      diff,
		Proposals: proposals,
		Changed:   changed || input.Prior == nil,
	}, nil
}

func validateInput(input Input) error {
	// A snapshot already states what it consumed. A caller that supplied a
	// different watermark beside it would be asking the reduction to fold from
	// somewhere the snapshot does not describe.
	if input.Prior != nil && input.PriorConsumed != 0 &&
		input.PriorConsumed != input.Prior.ConsumedHostSequence {
		return fmt.Errorf("%w: prior consumed watermark contradicts the snapshot",
			ErrInvalidInput)
	}
	if input.BootID == "" || input.EvaluationTick <= 0 {
		return fmt.Errorf("%w: time context", ErrInvalidInput)
	}
	if input.Wake != nil && input.Wake.Tick <= 0 {
		return fmt.Errorf("%w: wake tick", ErrInvalidInput)
	}
	if input.Prior != nil {
		if err := input.Prior.Validate(); err != nil {
			return fmt.Errorf("%w: prior snapshot", ErrInvalidInput)
		}
		// Monotonic time is only comparable inside one boot. Within a boot it
		// may not run backwards: a deadline measured against a tick that
		// regressed would make a stale component look fresh again.
		if input.Prior.BootID == input.BootID &&
			input.EvaluationTick < input.Prior.EvaluationTick {
			return fmt.Errorf("%w: monotonic tick moved backwards within one boot",
				ErrInvalidInput)
		}
	}
	for _, event := range input.Events {
		if err := connectivity.Validate(event.Fact); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if event.Acceptance.Outcome == connectivityaccept.OutcomeRejected {
			return fmt.Errorf("%w: a rejected arrival was offered for reduction",
				ErrInvalidInput)
		}
	}
	return nil
}

// componentTable carries mutable per-component accumulation.
type componentTable map[connectivity.Component]*ComponentRecord

func newComponentTable(prior *Snapshot) componentTable {
	table := make(componentTable, len(connectivity.Components()))
	for _, component := range connectivity.Components() {
		record := ComponentRecord{Component: component, State: StateUnknown}
		if declaration, ok := safety.ConnectivityAuthority(component); ok {
			record.Domain = declaration.Domain
			record.Source = declaration.Source
		}
		record.Observed = connectivity.LifecycleUnknown
		record.Reason = connectivity.ReasonNone
		table[component] = &record
	}
	if prior == nil {
		return table
	}
	for _, record := range prior.Components {
		carried := record
		carried.Corroborations = append([]Corroboration(nil), record.Corroborations...)
		table[record.Component] = &carried
	}
	return table
}

type sourceTable map[connectivity.SourceID]*SourceWatermark

func newSourceTable(prior *Snapshot) sourceTable {
	table := make(sourceTable)
	if prior == nil {
		return table
	}
	for _, watermark := range prior.Sources {
		carried := watermark
		carried.Gaps = append([]connectivityaccept.GapRange(nil), watermark.Gaps...)
		carried.PendingBaseline = append(
			[]connectivity.Component(nil), watermark.PendingBaseline...)
		table[watermark.Source] = &carried
	}
	return table
}

type conflictKey struct {
	source    connectivity.SourceID
	component connectivity.Component
	sequence  uint64
}

type conflictTable map[conflictKey]*ConflictRecord

func newConflictTable(prior *Snapshot) conflictTable {
	table := make(conflictTable)
	if prior == nil {
		return table
	}
	for _, record := range prior.Conflicts {
		carried := record
		table[conflictKey{record.Source, record.Component, record.Sequence}] = &carried
	}
	return table
}

func watermarkFor(sources sourceTable, event Event) *SourceWatermark {
	watermark, known := sources[event.Fact.SourceID]
	if !known {
		watermark = &SourceWatermark{
			Source: event.Fact.SourceID,
			Domain: event.Fact.Domain,
			Role:   event.Acceptance.Role,
			BootID: event.Fact.BootID,
		}
		sources[event.Fact.SourceID] = watermark
	}
	return watermark
}

// adoptIntegrity takes the acceptor's decision about a stream verbatim.
//
// Nothing here recomputes gaps, overflow, baseline debt or the conflict count.
// They were decided once, under the acceptor's bounds, and a second derivation
// here is exactly how the snapshot and the acceptor would come to disagree —
// which matters because a restart rebuilds the acceptor from this watermark.
func adoptIntegrity(watermark *SourceWatermark, event Event) {
	integrity := event.Acceptance.Source
	watermark.BootID = integrity.BootID
	watermark.LastSequence = integrity.LastSequence
	watermark.Gaps = append([]connectivityaccept.GapRange(nil), integrity.Gaps...)
	watermark.GapOverflow = integrity.GapOverflow
	watermark.PendingBaseline = append(
		[]connectivity.Component(nil), integrity.PendingBaseline...)
	watermark.Conflicts = integrity.Conflicts
}

func applyAccepted(
	components componentTable,
	sources sourceTable,
	conflicts conflictTable,
	event Event,
) {
	watermark := watermarkFor(sources, event)
	watermark.Role = event.Acceptance.Role
	adoptIntegrity(watermark, event)
	if event.Fact.Baseline {
		// The owner restated this component in full, so the refusals recorded
		// against it describe a position that has been superseded. Retaining
		// them past that point would show an operator a conflict the state no
		// longer has.
		clearComponentConflicts(conflicts, event.Fact.SourceID, event.Fact.Component)
	}

	record := components[event.Fact.Component]
	if event.Acceptance.Role != safety.RoleAuthoritative {
		addCorroboration(record, event)
		return
	}
	record.Observed = event.Fact.Lifecycle
	record.Reason = event.Fact.Reason
	record.Payload = event.Fact.Payload
	record.BootID = event.Fact.BootID
	record.MonotonicTick = event.Fact.MonotonicTick
	record.FreshnessDeadline = event.Fact.FreshnessDeadline
	record.HostSequence = event.Acceptance.HostSequence
	if event.Fact.Baseline {
		record.HasBaseline = true
		record.RebaselineRequired = false
	}
}

// addCorroboration keeps at most one opinion per corroborating source, so the
// evidence list is bounded by the compiled source table rather than by traffic.
func addCorroboration(record *ComponentRecord, event Event) {
	entry := Corroboration{
		Source:        event.Fact.SourceID,
		Lifecycle:     event.Fact.Lifecycle,
		Reason:        event.Fact.Reason,
		Agrees:        event.Fact.Lifecycle == record.Observed,
		MonotonicTick: event.Fact.MonotonicTick,
	}
	for index := range record.Corroborations {
		if record.Corroborations[index].Source == entry.Source {
			record.Corroborations[index] = entry
			return
		}
	}
	if len(record.Corroborations) >= MaxCorroborations {
		return
	}
	record.Corroborations = append(record.Corroborations, entry)
}

func applyConflict(sources sourceTable, conflicts conflictTable, event Event) {
	watermark := watermarkFor(sources, event)
	watermark.Role = event.Acceptance.Role
	adoptIntegrity(watermark, event)

	key := conflictKey{event.Fact.SourceID, event.Fact.Component, event.Fact.SourceSequence}
	if existing, seen := conflicts[key]; seen {
		existing.Count++
		return
	}
	conflicts[key] = &ConflictRecord{
		Source:    event.Fact.SourceID,
		Component: event.Fact.Component,
		Sequence:  event.Fact.SourceSequence,
		Reason:    event.Acceptance.Reason,
		Count:     1,
	}
}

func clearComponentConflicts(
	conflicts conflictTable,
	source connectivity.SourceID,
	component connectivity.Component,
) {
	for key := range conflicts {
		if key.source == source && key.component == component {
			delete(conflicts, key)
		}
	}
}

// renderComponents derives each component's state and returns the records in
// the fixed component order, always complete.
func renderComponents(components componentTable, sources sourceTable, input Input) []ComponentRecord {
	ordered := connectivity.Components()
	out := make([]ComponentRecord, 0, len(ordered))
	for _, component := range ordered {
		record := *components[component]
		watermark := sources[record.Source]
		record.Conflicts = 0
		if watermark != nil {
			record.Conflicts = watermark.Conflicts
		}
		record.State = deriveState(record, input)
		record.Corroborations = sortedCorroborations(record.Corroborations)
		out = append(out, record)
	}
	return out
}

// deriveState is where the aggregate draws the conclusions a source may not
// draw about itself.
func deriveState(record ComponentRecord, input Input) ComponentState {
	if record.Observed == connectivity.LifecycleUnknown && record.HostSequence == 0 {
		return StateUnknown
	}
	// An unresolved conflict means two different contents claimed the same
	// position. The accepted fact is kept and shown, but it is no longer
	// evidence of a settled state.
	if record.Conflicts > 0 {
		return StateConflict
	}
	// A deadline is only meaningful inside the boot that issued it.
	if record.BootID != input.BootID {
		return StateStale
	}
	// Sleep is not evidence of health. Until the owner restates the component
	// in full, what was observed before the wake describes a host that was
	// not running.
	if record.RebaselineRequired {
		return StateStale
	}
	if input.EvaluationTick > record.FreshnessDeadline {
		return StateStale
	}
	switch record.Observed {
	case connectivity.LifecycleReady:
		return StateReady
	case connectivity.LifecycleDegraded:
		return StateDegraded
	case connectivity.LifecycleFailed:
		return StateFailed
	case connectivity.LifecycleNotApplicable:
		return StateNotApplicable
	default:
		return StateUnknown
	}
}

func sortedCorroborations(entries []Corroboration) []Corroboration {
	if len(entries) == 0 {
		return nil
	}
	out := append([]Corroboration(nil), entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

func renderSources(sources sourceTable) []SourceWatermark {
	out := make([]SourceWatermark, 0, len(sources))
	for _, watermark := range sources {
		copied := *watermark
		copied.Gaps = append([]connectivityaccept.GapRange(nil), watermark.Gaps...)
		sort.Slice(copied.Gaps, func(i, j int) bool {
			return copied.Gaps[i].From < copied.Gaps[j].From
		})
		if len(copied.Gaps) == 0 {
			copied.Gaps = nil
		}
		copied.PendingBaseline = append(
			[]connectivity.Component(nil), watermark.PendingBaseline...)
		sort.Slice(copied.PendingBaseline, func(i, j int) bool {
			return copied.PendingBaseline[i] < copied.PendingBaseline[j]
		})
		if len(copied.PendingBaseline) == 0 {
			copied.PendingBaseline = nil
		}
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// renderConflicts orders retained refusals and holds each source inside the
// bound.
//
// Within a source the lowest sequences are dropped first: those describe
// positions the stream has already moved past, while the most recent refusals
// are the ones an operator can still do something about. The eviction is
// reported rather than left to look like a shorter list.
func renderConflicts(conflicts conflictTable) ([]ConflictRecord, bool) {
	if len(conflicts) == 0 {
		return nil, false
	}
	bySource := make(map[connectivity.SourceID][]ConflictRecord)
	for _, record := range conflicts {
		bySource[record.Source] = append(bySource[record.Source], *record)
	}
	overflow := false
	out := make([]ConflictRecord, 0, len(conflicts))
	for _, records := range bySource {
		// Sequence orders age inside one stream, so the tail is the most
		// recent evidence this source produced.
		sort.Slice(records, func(i, j int) bool {
			if records[i].Sequence != records[j].Sequence {
				return records[i].Sequence < records[j].Sequence
			}
			return records[i].Component < records[j].Component
		})
		if len(records) > MaxConflictRecords {
			records = records[len(records)-MaxConflictRecords:]
			overflow = true
		}
		out = append(out, records...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		return out[i].Sequence < out[j].Sequence
	})
	return out, overflow
}

// authorize decides whether desired state may be derived at all. Observations
// are never withheld: only the authority to say what should be is.
func authorize(descriptor PolicyDescriptor, prior *Snapshot) (Authorization, AuthorizationReason) {
	switch {
	case !descriptor.Present:
		return AuthorizationUnauthorized, AuthorizationReasonAbsent
	case !descriptor.Valid:
		return AuthorizationUnauthorized, AuthorizationReasonInvalid
	case descriptor.Suspended:
		return AuthorizationUnauthorized, AuthorizationReasonSuspended
	case !descriptor.Authorized():
		return AuthorizationUnauthorized, AuthorizationReasonInvalid
	}
	// A policy that moved backwards is not the policy this lineage was bound
	// to, and resuming under it would authorize an older decision.
	if prior != nil && prior.Policy.Present &&
		(descriptor.BundleGeneration < prior.Policy.BundleGeneration ||
			descriptor.RootGeneration < prior.Policy.RootGeneration ||
			descriptor.UserGeneration < prior.Policy.UserGeneration) {
		return AuthorizationUnauthorized, AuthorizationReasonGenerationGap
	}
	return AuthorizationAuthorized, AuthorizationReasonNone
}

// summarize derives the operator projection. It cannot report better than its
// worst component, and it never replaces the component records.
func summarize(snapshot Snapshot, conflictOverflow bool) Summary {
	summary := Summary{
		Authorization:    snapshot.Authorization,
		Reason:           snapshot.Reason,
		ConflictOverflow: conflictOverflow,
	}
	for _, record := range snapshot.Components {
		switch record.State {
		case StateReady:
			summary.Ready++
		case StateDegraded:
			summary.Degraded++
		case StateFailed:
			summary.Failed++
		case StateStale:
			summary.Stale++
		case StateConflict:
			summary.Conflicted++
		case StateNotApplicable:
			summary.NotApplicable++
		default:
			summary.Unknown++
		}
	}
	for _, watermark := range snapshot.Sources {
		// Both counts are bounded by the compiled source and gap limits, so
		// the widening cannot lose a digit.
		summary.OpenGaps += uint16(len(watermark.Gaps))
		if watermark.GapOverflow {
			summary.GapOverflow = true
		}
		if watermark.Conflicts > 0 {
			summary.SourceConflicts++
		}
		if watermark.AwaitingBaseline() {
			summary.AwaitingBaseline++
		}
	}
	// A hole in a source's stream is a missing observation, and the spec is
	// explicit that healthy aggregate output must not be inferred across one.
	// Every surviving component can be fresh and ready precisely because the
	// facts that would have said otherwise are the ones that went missing, so
	// integrity degrades the summary on its own rather than only through the
	// components that happen to have been observed.
	integrity := summary.OpenGaps > 0 || summary.GapOverflow ||
		summary.AwaitingBaseline > 0 || summary.SourceConflicts > 0
	switch {
	case summary.Failed > 0:
		summary.State = AggregateFailed
	case summary.Degraded > 0 || summary.Stale > 0 || summary.Conflicted > 0 ||
		integrity:
		summary.State = AggregateDegraded
	case summary.Unknown > 0:
		summary.State = AggregateUnknown
	default:
		summary.State = AggregateReady
	}
	return summary
}

// semantic is the projection that decides whether a reduction changed
// anything. It omits the bookkeeping that moves on every batch — sequences,
// ticks and deadlines — so re-observing the same state is not a change, while
// the staleness that a deadline eventually causes is.
type semantic struct {
	BootID        string              `json:"boot_id"`
	Policy        PolicyDescriptor    `json:"policy"`
	Authorization Authorization       `json:"authorization"`
	Reason        AuthorizationReason `json:"authorization_reason"`
	Components    []semanticComponent `json:"components"`
	Sources       []semanticSource    `json:"sources"`
	Conflicts     []ConflictRecord    `json:"conflicts,omitempty"`
	Summary       Summary             `json:"summary"`
}

type semanticComponent struct {
	Component          connectivity.Component  `json:"component"`
	State              ComponentState          `json:"state"`
	Observed           connectivity.Lifecycle  `json:"observed"`
	Reason             connectivity.Reason     `json:"reason"`
	Payload            connectivity.Payload    `json:"payload"`
	HasBaseline        bool                    `json:"has_baseline"`
	RebaselineRequired bool                    `json:"rebaseline_required"`
	Conflicts          uint32                  `json:"conflicts"`
	Corroborations     []semanticCorroboration `json:"corroborations,omitempty"`
}

type semanticCorroboration struct {
	Source    connectivity.SourceID  `json:"source"`
	Lifecycle connectivity.Lifecycle `json:"lifecycle"`
	Agrees    bool                   `json:"agrees"`
}

type semanticSource struct {
	Source          connectivity.SourceID         `json:"source"`
	BootID          string                        `json:"boot_id"`
	Gaps            []connectivityaccept.GapRange `json:"gaps,omitempty"`
	GapOverflow     bool                          `json:"gap_overflow"`
	PendingBaseline []connectivity.Component      `json:"pending_baseline,omitempty"`
	Conflicts       uint32                        `json:"conflicts"`
}

func project(snapshot Snapshot) semantic {
	out := semantic{
		BootID:        snapshot.BootID,
		Policy:        snapshot.Policy,
		Authorization: snapshot.Authorization,
		Reason:        snapshot.Reason,
		Conflicts:     snapshot.Conflicts,
		Summary:       snapshot.Summary,
	}
	for _, record := range snapshot.Components {
		component := semanticComponent{
			Component:          record.Component,
			State:              record.State,
			Observed:           record.Observed,
			Reason:             record.Reason,
			Payload:            record.Payload,
			HasBaseline:        record.HasBaseline,
			RebaselineRequired: record.RebaselineRequired,
			Conflicts:          record.Conflicts,
		}
		for _, entry := range record.Corroborations {
			component.Corroborations = append(component.Corroborations, semanticCorroboration{
				Source: entry.Source, Lifecycle: entry.Lifecycle, Agrees: entry.Agrees,
			})
		}
		out.Components = append(out.Components, component)
	}
	for _, watermark := range snapshot.Sources {
		out.Sources = append(out.Sources, semanticSource{
			Source:          watermark.Source,
			BootID:          watermark.BootID,
			Gaps:            watermark.Gaps,
			GapOverflow:     watermark.GapOverflow,
			PendingBaseline: watermark.PendingBaseline,
			Conflicts:       watermark.Conflicts,
		})
	}
	return out
}

// SemanticDigest returns the digest reduction compares to decide whether a
// generation should advance.
func SemanticDigest(snapshot Snapshot) (string, error) {
	digest, _, err := policy.CanonicalSHA256(project(snapshot))
	if err != nil {
		return "", ErrInvalidSnapshot
	}
	return digest, nil
}

func effective(prior *Snapshot, next Snapshot) (bool, error) {
	if prior == nil {
		return true, nil
	}
	before, err := SemanticDigest(*prior)
	if err != nil {
		return false, err
	}
	after, err := SemanticDigest(next)
	if err != nil {
		return false, err
	}
	return before != after, nil
}

// markRebaseline withdraws the assumption that time-sensitive observations
// survived a sleep or a reboot.
//
// It runs before the batch is applied, so a baseline that arrives in the same
// batch as the wake clears the requirement it just raised. That is the point:
// a component whose owner has already restated it needs nothing further.
func markRebaseline(components componentTable, input Input) {
	rebooted := input.Prior != nil && input.Prior.BootID != input.BootID
	if input.Wake == nil && !rebooted {
		return
	}
	for component, record := range components {
		if !component.TimeSensitive() {
			continue
		}
		if record.HostSequence == 0 {
			// Never observed: already unknown, and nothing to withdraw.
			continue
		}
		record.RebaselineRequired = true
	}
}
