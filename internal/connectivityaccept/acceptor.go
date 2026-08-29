// Package connectivityaccept assigns a durable host order to connectivity
// facts and decides what each arrival means.
//
// The aggregate deduplicates on identity plus canonical digest, so this is
// where an exact retry stops being a second event, where a reused identity
// with different content becomes a conflict rather than an update, and where a
// skipped source sequence becomes a visible gap rather than silence.
//
// Nothing here interprets a fact. Whether a component is ready, stale or
// divergent is the reducer's decision; this package only decides whether an
// arrival is part of the accepted order.
package connectivityaccept

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// RetryWindow is how many recent sequences per source keep their digest so an
// exact retry can be recognised as one.
//
// Below the window a repeat cannot be distinguished from a conflicting reuse,
// so it is reported as stale rather than guessed either way. Both outcomes are
// non-mutating: neither adds an accepted event.
const RetryWindow = 256

// MaxGapRanges bounds how many separate holes one source may accumulate before
// the overflow itself becomes the visible fact.
const MaxGapRanges = 64

var (
	ErrInvalidState = errors.New("acceptance state is invalid")
	ErrNilAcceptor  = errors.New("acceptor is not initialised")
)

// Outcome is what an arrival turned out to be.
type Outcome string

const (
	// OutcomeAccepted means the fact entered the host order under a new
	// acceptance sequence.
	OutcomeAccepted Outcome = "accepted"
	// OutcomeDuplicate means an exact retry: same identity, same digest.
	OutcomeDuplicate Outcome = "duplicate"
	// OutcomeConflict means the same identity arrived with different content.
	OutcomeConflict Outcome = "conflict"
	// OutcomeStale means the arrival is behind the accepted order and cannot
	// be proven to be either a retry or a conflict.
	OutcomeStale Outcome = "stale"
	// OutcomeRejected means the fact never entered the order at all.
	OutcomeRejected Outcome = "rejected"
)

// Reason is the bounded explanation attached to an outcome.
type Reason string

const (
	ReasonNone             Reason = "none"
	ReasonInvalidFact      Reason = "invalid_fact"
	ReasonOwnership        Reason = "ownership"
	ReasonDomainMismatch   Reason = "domain_mismatch"
	ReasonExactRetry       Reason = "exact_retry"
	ReasonIdentityReused   Reason = "identity_reused"
	ReasonBehindWatermark  Reason = "behind_watermark"
	ReasonBeyondRetry      Reason = "beyond_retry_window"
	ReasonSequenceGap      Reason = "sequence_gap"
	ReasonBootChanged      Reason = "boot_changed"
	ReasonGapOverflow      Reason = "gap_overflow"
	ReasonBaselineAccepted Reason = "baseline_accepted"
)

// Identity is what makes two arrivals the same observation.
type Identity struct {
	SourceID connectivity.SourceID `json:"source_id"`
	BootID   string                `json:"boot_id"`
	Sequence uint64                `json:"source_sequence"`
}

// GapRange is a half-open run of source sequences that never arrived.
type GapRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// Acceptance is the decision about one arrival.
type Acceptance struct {
	Outcome      Outcome           `json:"outcome"`
	Reason       Reason            `json:"reason"`
	Role         safety.SourceRole `json:"role,omitempty"`
	Digest       string            `json:"digest,omitempty"`
	HostSequence uint64            `json:"host_sequence,omitempty"`
	// OpenedGap is set when this arrival revealed a hole behind it.
	OpenedGap *GapRange `json:"opened_gap,omitempty"`
	// ClearedGaps is set when a complete baseline closed prior holes.
	ClearedGaps []GapRange `json:"cleared_gaps,omitempty"`
}

// Accepted reports whether the arrival entered the host order.
func (acceptance Acceptance) Accepted() bool {
	return acceptance.Outcome == OutcomeAccepted
}

// digestEntry remembers one accepted sequence inside the retry window.
type digestEntry struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

// SourceState is one source's position in its own stream.
type SourceState struct {
	BootID       string        `json:"boot_id"`
	LastSequence uint64        `json:"last_sequence"`
	Recent       []digestEntry `json:"recent"`
	Gaps         []GapRange    `json:"gaps"`
	// GapOverflow records that holes were dropped rather than forgotten.
	GapOverflow bool `json:"gap_overflow"`
	// AwaitingBaseline is set when the stream cannot be trusted to be
	// continuous until the owner restates its component in full.
	AwaitingBaseline bool `json:"awaiting_baseline"`
}

// State is the whole durable position of the acceptor.
type State struct {
	HostSequence uint64                                 `json:"host_sequence"`
	Sources      map[connectivity.SourceID]*SourceState `json:"sources"`
}

// Clone returns a deep copy so a caller can checkpoint a stable value.
func (state State) Clone() State {
	out := State{HostSequence: state.HostSequence,
		Sources: make(map[connectivity.SourceID]*SourceState, len(state.Sources))}
	for id, source := range state.Sources {
		copied := *source
		copied.Recent = append([]digestEntry(nil), source.Recent...)
		copied.Gaps = append([]GapRange(nil), source.Gaps...)
		out.Sources[id] = &copied
	}
	return out
}

// Validate rejects a restored state that could not have been produced by this
// acceptor, so a tampered checkpoint fails loudly instead of resuming.
func (state State) Validate() error {
	for id, source := range state.Sources {
		if source == nil {
			return fmt.Errorf("%w: nil source %q", ErrInvalidState, id)
		}
		if len(source.Recent) > RetryWindow {
			return fmt.Errorf("%w: retry window overflow for %q", ErrInvalidState, id)
		}
		if len(source.Gaps) > MaxGapRanges {
			return fmt.Errorf("%w: gap overflow for %q", ErrInvalidState, id)
		}
		for index, entry := range source.Recent {
			if entry.Sequence > source.LastSequence {
				return fmt.Errorf("%w: %q remembers a sequence beyond its watermark",
					ErrInvalidState, id)
			}
			if index > 0 && entry.Sequence <= source.Recent[index-1].Sequence {
				return fmt.Errorf("%w: %q retry window is not ordered", ErrInvalidState, id)
			}
		}
		for _, gap := range source.Gaps {
			if gap.From == 0 || gap.To < gap.From {
				return fmt.Errorf("%w: %q has an inverted gap", ErrInvalidState, id)
			}
		}
	}
	return nil
}

// Acceptor assigns the durable host acceptance order.
//
// It is safe for concurrent use: independent sources publish from independent
// goroutines, and the host sequence must be a single monotonic order across
// all of them.
type Acceptor struct {
	mu    sync.Mutex
	state State
}

// New returns an acceptor at the beginning of the host order.
func New() *Acceptor {
	return &Acceptor{state: State{Sources: make(map[connectivity.SourceID]*SourceState)}}
}

// Restore returns an acceptor resuming from a validated durable state.
func Restore(state State) (*Acceptor, error) {
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return &Acceptor{state: state.Clone()}, nil
}

// State returns a copy of the durable position.
func (acceptor *Acceptor) State() State {
	acceptor.mu.Lock()
	defer acceptor.mu.Unlock()
	return acceptor.state.Clone()
}

// Accept decides what one arrival means and, when it is new, gives it the next
// host acceptance sequence.
//
// authenticatedDomain is the domain the transport proved. It is passed in
// rather than read from the fact so a sender cannot describe itself.
func (acceptor *Acceptor) Accept(
	fact connectivity.Fact,
	authenticatedDomain policy.Domain,
) (Acceptance, error) {
	if acceptor == nil {
		return Acceptance{}, ErrNilAcceptor
	}
	if err := connectivity.Validate(fact); err != nil {
		return Acceptance{Outcome: OutcomeRejected, Reason: ReasonInvalidFact}, err
	}
	role, err := safety.ClassifyConnectivityFact(fact, authenticatedDomain)
	if err != nil {
		reason := ReasonOwnership
		if errors.Is(err, safety.ErrDomainMismatch) {
			reason = ReasonDomainMismatch
		}
		return Acceptance{Outcome: OutcomeRejected, Reason: reason}, err
	}
	digest, err := connectivity.Digest(fact)
	if err != nil {
		return Acceptance{Outcome: OutcomeRejected, Reason: ReasonInvalidFact}, err
	}

	acceptor.mu.Lock()
	defer acceptor.mu.Unlock()

	source, known := acceptor.state.Sources[fact.SourceID]
	if !known {
		source = &SourceState{BootID: fact.BootID}
		acceptor.state.Sources[fact.SourceID] = source
	}

	if source.BootID != fact.BootID {
		// A new boot is a new stream. Nothing from the prior boot orders
		// against it, and its freshness deadlines do not carry over.
		*source = SourceState{BootID: fact.BootID, AwaitingBaseline: true}
		acceptance := acceptor.admit(source, fact, digest, role)
		if acceptance.Reason == ReasonNone || acceptance.Reason == ReasonSequenceGap {
			acceptance.Reason = ReasonBootChanged
		}
		return acceptance, nil
	}

	switch {
	case fact.SourceSequence > source.LastSequence:
		return acceptor.admit(source, fact, digest, role), nil
	default:
		return acceptor.replay(source, fact, digest, role), nil
	}
}

// admit records a fact that advances its source's stream.
func (acceptor *Acceptor) admit(
	source *SourceState,
	fact connectivity.Fact,
	digest string,
	role safety.SourceRole,
) Acceptance {
	acceptance := Acceptance{
		Outcome: OutcomeAccepted,
		Reason:  ReasonNone,
		Role:    role,
		Digest:  digest,
	}
	expected := source.LastSequence + 1
	if source.LastSequence > 0 && fact.SourceSequence > expected {
		gap := GapRange{From: expected, To: fact.SourceSequence - 1}
		acceptor.recordGap(source, gap)
		acceptance.OpenedGap = &gap
		acceptance.Reason = ReasonSequenceGap
	}

	// Only a complete restatement can close holes: the later state is known,
	// but what happened inside the hole is not, and a non-baseline fact does
	// not claim to describe it.
	if fact.Baseline && (len(source.Gaps) > 0 || source.AwaitingBaseline) {
		acceptance.ClearedGaps = append([]GapRange(nil), source.Gaps...)
		source.Gaps = nil
		source.GapOverflow = false
		source.AwaitingBaseline = false
		acceptance.Reason = ReasonBaselineAccepted
	}

	source.LastSequence = fact.SourceSequence
	source.Recent = append(source.Recent, digestEntry{
		Sequence: fact.SourceSequence, Digest: digest,
	})
	if len(source.Recent) > RetryWindow {
		source.Recent = append([]digestEntry(nil), source.Recent[len(source.Recent)-RetryWindow:]...)
	}

	acceptor.state.HostSequence++
	acceptance.HostSequence = acceptor.state.HostSequence
	return acceptance
}

// replay classifies an arrival that does not advance its source's stream.
func (acceptor *Acceptor) replay(
	source *SourceState,
	fact connectivity.Fact,
	digest string,
	role safety.SourceRole,
) Acceptance {
	for _, entry := range source.Recent {
		if entry.Sequence != fact.SourceSequence {
			continue
		}
		if entry.Digest == digest {
			return Acceptance{
				Outcome: OutcomeDuplicate, Reason: ReasonExactRetry,
				Role: role, Digest: digest,
			}
		}
		return Acceptance{
			Outcome: OutcomeConflict, Reason: ReasonIdentityReused,
			Role: role, Digest: digest,
		}
	}
	// Inside a recorded hole the sequence was never accepted, so a late
	// arrival there is behind the watermark rather than a reuse.
	reason := ReasonBeyondRetry
	if inGap(source.Gaps, fact.SourceSequence) {
		reason = ReasonBehindWatermark
	}
	return Acceptance{Outcome: OutcomeStale, Reason: reason, Role: role, Digest: digest}
}

func inGap(gaps []GapRange, sequence uint64) bool {
	for _, gap := range gaps {
		if sequence >= gap.From && sequence <= gap.To {
			return true
		}
	}
	return false
}

// recordGap keeps holes ordered and bounded, and makes the loss of a hole
// itself observable rather than silently forgetting it.
func (acceptor *Acceptor) recordGap(source *SourceState, gap GapRange) {
	source.Gaps = append(source.Gaps, gap)
	sort.Slice(source.Gaps, func(i, j int) bool {
		return source.Gaps[i].From < source.Gaps[j].From
	})
	if len(source.Gaps) > MaxGapRanges {
		source.Gaps = source.Gaps[len(source.Gaps)-MaxGapRanges:]
		source.GapOverflow = true
	}
	source.AwaitingBaseline = true
}

// Gaps returns the holes currently recorded for a source.
func (acceptor *Acceptor) Gaps(id connectivity.SourceID) ([]GapRange, bool) {
	acceptor.mu.Lock()
	defer acceptor.mu.Unlock()
	source, known := acceptor.state.Sources[id]
	if !known {
		return nil, false
	}
	return append([]GapRange(nil), source.Gaps...), true
}
