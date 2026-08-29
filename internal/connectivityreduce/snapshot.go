// Package connectivityreduce turns ordered accepted facts into one normalized
// snapshot of host connectivity.
//
// The snapshot keeps every configured component separately inspectable and
// derives a summary beside them, never instead of them. A host with a working
// tunnel and broken DNS has to stay legible as exactly that.
//
// Reduction is pure. It reads no clock, no environment and no file: the
// evaluation tick, the boot identity and the policy descriptor are inputs, so
// the same inputs always produce the same canonical output.
package connectivityreduce

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

const (
	// SnapshotSchema names the wire contract for a connectivity snapshot.
	SnapshotSchema = "hexroute.connectivity-snapshot.v1"
	// SnapshotSchemaVersion is bumped only for an incompatible change.
	SnapshotSchemaVersion uint16 = 1

	// ReducerID identifies the reduction rules that produced a snapshot. A
	// checkpoint binds it, so output from different rules is never mistaken
	// for a replay of the same ones.
	ReducerID = "hexroute.connectivity-reducer"
	// ReducerVersion changes whenever reduction could produce a different
	// snapshot from identical inputs.
	ReducerVersion uint16 = 1

	// MaxCorroborations bounds retained evidence per component.
	MaxCorroborations = 8
)

var (
	ErrInvalidInput    = errors.New("reduction input is invalid")
	ErrInvalidSnapshot = errors.New("connectivity snapshot is invalid")
	ErrOutOfOrder      = errors.New("accepted facts are not in host order")
)

// ComponentState is what the aggregate concludes about a component.
//
// It is a superset of what a source may assert: stale and conflict are
// conclusions drawn from freshness and ownership, and no collector can claim
// them about itself.
type ComponentState string

const (
	StateUnknown       ComponentState = "unknown"
	StateReady         ComponentState = "ready"
	StateDegraded      ComponentState = "degraded"
	StateFailed        ComponentState = "failed"
	StateStale         ComponentState = "stale"
	StateConflict      ComponentState = "conflict"
	StateNotApplicable ComponentState = "not_applicable"
)

// Corroboration is one non-owning source's opinion about a component.
type Corroboration struct {
	Source    connectivity.SourceID  `json:"source"`
	Lifecycle connectivity.Lifecycle `json:"lifecycle"`
	Reason    connectivity.Reason    `json:"reason"`
	// Agrees compares the corroborating lifecycle with the owner's. A
	// disagreement is retained as evidence and never promoted to state.
	Agrees        bool         `json:"agrees"`
	MonotonicTick control.Tick `json:"monotonic_tick"`
}

// ComponentRecord is the aggregate's normalized view of one component.
type ComponentRecord struct {
	Component connectivity.Component `json:"component"`
	State     ComponentState         `json:"state"`
	Domain    policy.Domain          `json:"domain"`
	Source    connectivity.SourceID  `json:"source"`

	// Observed is what the owner last asserted. It is retained even when the
	// derived state is stale, so an operator can see what went quiet.
	Observed          connectivity.Lifecycle `json:"observed"`
	Reason            connectivity.Reason    `json:"reason"`
	Payload           connectivity.Payload   `json:"payload"`
	BootID            string                 `json:"boot_id,omitempty"`
	MonotonicTick     control.Tick           `json:"monotonic_tick,omitempty"`
	FreshnessDeadline control.Tick           `json:"freshness_deadline,omitempty"`
	HostSequence      uint64                 `json:"host_sequence,omitempty"`

	// HasBaseline records whether the owner has ever restated this component
	// in full. Without one, a derived ready state rests on a partial stream.
	HasBaseline bool `json:"has_baseline"`
	// RebaselineRequired is set when the host woke or rebooted. Sleep is not
	// evidence of health, so what was true before it is held stale until the
	// owner restates the component in full — a fresh non-baseline fact is not
	// enough, because it describes now without accounting for the gap.
	RebaselineRequired bool            `json:"rebaseline_required,omitempty"`
	Conflicts          uint32          `json:"conflicts"`
	Corroborations     []Corroboration `json:"corroborations,omitempty"`
}

// SourceWatermark is one source's position and integrity as the snapshot sees
// it. This is where a gap or a conflict stops being a return value.
type SourceWatermark struct {
	Source           connectivity.SourceID         `json:"source"`
	Domain           policy.Domain                 `json:"domain"`
	Role             safety.SourceRole             `json:"role"`
	BootID           string                        `json:"boot_id"`
	LastSequence     uint64                        `json:"last_sequence"`
	Gaps             []connectivityaccept.GapRange `json:"gaps,omitempty"`
	GapOverflow      bool                          `json:"gap_overflow"`
	AwaitingBaseline bool                          `json:"awaiting_baseline"`
	Conflicts        uint32                        `json:"conflicts"`
}

// ConflictRecord names one refusal to resolve a disagreement by overwriting.
type ConflictRecord struct {
	Source    connectivity.SourceID     `json:"source"`
	Component connectivity.Component    `json:"component"`
	Sequence  uint64                    `json:"source_sequence"`
	Reason    connectivityaccept.Reason `json:"reason"`
	Count     uint32                    `json:"count"`
}

// PolicyDescriptor is the exact active policy a reduction was bound to.
//
// It is an input rather than something the reducer reads, both to keep
// reduction pure and so that a reduction can be replayed under the policy it
// actually ran under.
type PolicyDescriptor struct {
	Present          bool   `json:"present"`
	Valid            bool   `json:"valid"`
	Suspended        bool   `json:"suspended"`
	BundleGeneration uint64 `json:"bundle_generation"`
	RootGeneration   uint64 `json:"root_generation"`
	UserGeneration   uint64 `json:"user_generation"`
	ManifestDigest   string `json:"manifest_digest"`
}

// Authorized reports whether this policy may back a desired state at all.
func (descriptor PolicyDescriptor) Authorized() bool {
	return descriptor.Present && descriptor.Valid && !descriptor.Suspended &&
		descriptor.BundleGeneration > 0 && descriptor.ManifestDigest != ""
}

// Authorization says whether desired state and proposals may be derived.
type Authorization string

const (
	AuthorizationAuthorized   Authorization = "authorized"
	AuthorizationUnauthorized Authorization = "unauthorized"
)

// AuthorizationReason is the bounded explanation for an unauthorized snapshot.
type AuthorizationReason string

const (
	AuthorizationReasonNone          AuthorizationReason = "none"
	AuthorizationReasonAbsent        AuthorizationReason = "policy_absent"
	AuthorizationReasonInvalid       AuthorizationReason = "policy_invalid"
	AuthorizationReasonSuspended     AuthorizationReason = "policy_suspended"
	AuthorizationReasonGenerationGap AuthorizationReason = "policy_generation_mismatch"
)

// AggregateState is the operator-facing summary of the whole host.
type AggregateState string

const (
	AggregateUnknown  AggregateState = "unknown"
	AggregateReady    AggregateState = "ready"
	AggregateDegraded AggregateState = "degraded"
	AggregateFailed   AggregateState = "failed"
)

// Summary is a derived projection. It never removes a component record and is
// deliberately pessimistic: it cannot report better than its worst component.
type Summary struct {
	State         AggregateState      `json:"state"`
	Authorization Authorization       `json:"authorization"`
	Reason        AuthorizationReason `json:"authorization_reason"`

	Ready         uint16 `json:"ready"`
	Degraded      uint16 `json:"degraded"`
	Failed        uint16 `json:"failed"`
	Stale         uint16 `json:"stale"`
	Unknown       uint16 `json:"unknown"`
	Conflicted    uint16 `json:"conflicted"`
	NotApplicable uint16 `json:"not_applicable"`

	OpenGaps        uint16 `json:"open_gaps"`
	GapOverflow     bool   `json:"gap_overflow"`
	SourceConflicts uint16 `json:"source_conflicts"`
}

// Snapshot is the whole normalized read model at one generation.
type Snapshot struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	Generation     uint64 `json:"generation"`
	ReducerID      string `json:"reducer_id"`
	ReducerVersion uint16 `json:"reducer_version"`

	// BootID and EvaluationTick are the time context the reduction ran in.
	// They are recorded so a replay can be shown to have used the same one.
	BootID         string       `json:"boot_id"`
	EvaluationTick control.Tick `json:"evaluation_tick"`

	Policy        PolicyDescriptor    `json:"policy"`
	Authorization Authorization       `json:"authorization"`
	Reason        AuthorizationReason `json:"authorization_reason"`

	ConsumedHostSequence uint64 `json:"consumed_host_sequence"`

	Components []ComponentRecord `json:"components"`
	Sources    []SourceWatermark `json:"sources"`
	Conflicts  []ConflictRecord  `json:"conflicts,omitempty"`
	Summary    Summary           `json:"summary"`
}

// Digest returns the canonical digest of the whole snapshot.
func (snapshot Snapshot) Digest() (string, error) {
	digest, _, err := policy.CanonicalSHA256(snapshot)
	if err != nil {
		return "", ErrInvalidSnapshot
	}
	return digest, nil
}

// Validate rejects a snapshot that this reducer could not have produced.
func (snapshot Snapshot) Validate() error {
	if snapshot.Schema != SnapshotSchema || snapshot.Version != SnapshotSchemaVersion {
		return ErrInvalidSnapshot
	}
	if snapshot.ReducerID != ReducerID || snapshot.ReducerVersion != ReducerVersion {
		return ErrInvalidSnapshot
	}
	if snapshot.Generation == 0 {
		return ErrInvalidSnapshot
	}
	// Every configured component is present in every snapshot. An absent
	// component would be indistinguishable from one that is fine.
	if len(snapshot.Components) != len(connectivity.Components()) {
		return ErrInvalidSnapshot
	}
	expected := connectivity.Components()
	for index, record := range snapshot.Components {
		if record.Component != expected[index] {
			return ErrInvalidSnapshot
		}
		if !record.State.valid() {
			return ErrInvalidSnapshot
		}
	}
	if snapshot.Authorization == AuthorizationAuthorized &&
		snapshot.Reason != AuthorizationReasonNone {
		return ErrInvalidSnapshot
	}
	if snapshot.Authorization == AuthorizationUnauthorized &&
		snapshot.Reason == AuthorizationReasonNone {
		return ErrInvalidSnapshot
	}
	return nil
}

func (state ComponentState) valid() bool {
	switch state {
	case StateUnknown, StateReady, StateDegraded, StateFailed,
		StateStale, StateConflict, StateNotApplicable:
		return true
	default:
		return false
	}
}
