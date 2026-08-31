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
// RetryWindow is how far back a source's own digests are kept, which is what
// tells an exact retry from a reused identity from an arrival too old to
// judge.
//
// It is carried in every checkpoint, because a replay that could not see it
// would classify the same arrival differently and produce a snapshot the
// original never had. That makes its size a durability cost as well as a
// behavioural one: at one fact a minute per source it is still an hour of
// history, far beyond any retry this protocol produces.
const RetryWindow = 64

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
	// ReasonBaselinePending means this baseline settled its own component but
	// the source still owes a restatement for others it speaks about.
	ReasonBaselinePending Reason = "baseline_pending"
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

// SourceIntegrity is the source's whole stream integrity after one decision.
//
// It is returned rather than left for the reader to re-derive. Gap bounds,
// overflow and baseline coverage are decided once, here, so a consumer cannot
// hold a second opinion about them — and cannot accumulate past the bound this
// acceptor enforces, which is also the bound a restored state must satisfy.
type SourceIntegrity struct {
	BootID       string     `json:"boot_id"`
	LastSequence uint64     `json:"last_sequence"`
	Gaps         []GapRange `json:"gaps,omitempty"`
	// GapOverflow records that holes were dropped rather than forgotten.
	GapOverflow bool `json:"gap_overflow"`
	// PendingBaseline is which components still owe a complete restatement
	// before the stream can be called continuous again.
	PendingBaseline []connectivity.Component `json:"pending_baseline,omitempty"`
	Conflicts       uint32                   `json:"conflicts"`
	// Recent is the retry window as it stands after this decision. A replay
	// classifies arrivals against it, so a checkpoint that omitted it would
	// call a conflict something else and reach a different snapshot.
	Recent []RecentDigest `json:"recent,omitempty"`
}

// AwaitingBaseline reports whether any component still owes a restatement.
func (integrity SourceIntegrity) AwaitingBaseline() bool {
	return len(integrity.PendingBaseline) > 0
}

// Acceptance is the decision about one arrival.
type Acceptance struct {
	Outcome      Outcome           `json:"outcome"`
	Reason       Reason            `json:"reason"`
	Role         safety.SourceRole `json:"role,omitempty"`
	Digest       string            `json:"digest,omitempty"`
	HostSequence uint64            `json:"host_sequence,omitempty"`
	// FoldPosition is where this decision stands among every decision the
	// reduction was given, accepted or not. The host sequence orders the
	// accepted facts and has no room for the rest; this orders the rest
	// beside them, so a journal can hold everything a reduction read.
	FoldPosition uint64 `json:"fold_position,omitempty"`
	// OpenedGap is set when this arrival revealed a hole behind it.
	OpenedGap *GapRange `json:"opened_gap,omitempty"`
	// ClearedGaps is set when the last owed baseline closed prior holes.
	ClearedGaps []GapRange `json:"cleared_gaps,omitempty"`
	// Source is the stream's integrity after this decision. It is present on
	// every outcome that reached a known source, because refusing an arrival
	// changes integrity just as accepting one does.
	Source SourceIntegrity `json:"source"`
}

// Accepted reports whether the arrival entered the host order.
func (acceptance Acceptance) Accepted() bool {
	return acceptance.Outcome == OutcomeAccepted
}

// digestEntry remembers one accepted sequence inside the retry window.
// RecentDigest is one remembered arrival: the sequence it claimed and what it
// said. It is exported because a checkpoint has to carry the window, and a
// window that could not be written down could not be restored.
type RecentDigest struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

// SourceState is one source's position in its own stream.
type SourceState struct {
	BootID       string         `json:"boot_id"`
	LastSequence uint64         `json:"last_sequence"`
	Recent       []RecentDigest `json:"recent"`
	Gaps         []GapRange     `json:"gaps"`
	// GapOverflow records that holes were dropped rather than forgotten.
	GapOverflow bool `json:"gap_overflow"`
	// PendingBaseline is the set of components that still owe a complete
	// restatement. The stream is continuous again only when it is empty: a
	// hole is numbered per source, so a baseline for one component says
	// nothing about what the hole held for the others.
	PendingBaseline []connectivity.Component `json:"pending_baseline"`
	// Conflicts counts refusals to resolve a reused identity by overwriting.
	Conflicts uint32 `json:"conflicts"`
}

// AwaitingBaseline reports whether the stream still owes a restatement.
func (source *SourceState) AwaitingBaseline() bool {
	return source != nil && len(source.PendingBaseline) > 0
}

// State is the whole durable position of the acceptor.
type State struct {
	HostSequence uint64 `json:"host_sequence"`
	// FoldPosition is how many decisions this acceptor has handed out, of
	// every kind, so a resumed acceptor continues one order as well as the
	// other.
	FoldPosition uint64                                 `json:"fold_position"`
	Sources      map[connectivity.SourceID]*SourceState `json:"sources"`
}

// Clone returns a deep copy so a caller can checkpoint a stable value.
func (state State) Clone() State {
	out := State{HostSequence: state.HostSequence,
		FoldPosition: state.FoldPosition,
		Sources:      make(map[connectivity.SourceID]*SourceState, len(state.Sources))}
	for id, source := range state.Sources {
		copied := *source
		copied.Recent = append([]RecentDigest(nil), source.Recent...)
		copied.Gaps = append([]GapRange(nil), source.Gaps...)
		copied.PendingBaseline = append(
			[]connectivity.Component(nil), source.PendingBaseline...)
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
		// A restored state that owes nothing cannot also be holding holes:
		// that pairing describes a stream both interrupted and continuous,
		// which this acceptor never produces.
		if len(source.Gaps) > 0 && len(source.PendingBaseline) == 0 {
			return fmt.Errorf("%w: %q holds gaps while owing no baseline",
				ErrInvalidState, id)
		}
		declared := safety.ConnectivitySourceComponents(id)
		for _, component := range source.PendingBaseline {
			if !containsComponent(declared, component) {
				return fmt.Errorf("%w: %q owes a baseline for %q, which it does not speak about",
					ErrInvalidState, id, component)
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

	// Every decision from here on is one the reduction will be given, so every
	// one of them takes a place in the folded order. Rejections returned
	// above never reach a reduction and take none.
	acceptor.state.FoldPosition++
	folded := acceptor.state.FoldPosition

	if source.BootID != fact.BootID {
		// A new boot is a new stream. Nothing from the prior boot orders
		// against it, and its freshness deadlines do not carry over. Every
		// component the source speaks about owes a restatement, because none
		// of what it said in the prior boot describes this one.
		*source = SourceState{
			BootID:          fact.BootID,
			PendingBaseline: safety.ConnectivitySourceComponents(fact.SourceID),
		}
		acceptance := acceptor.admit(source, fact, digest, role)
		if acceptance.Reason == ReasonNone || acceptance.Reason == ReasonSequenceGap {
			acceptance.Reason = ReasonBootChanged
		}
		acceptance.FoldPosition = folded
		return acceptance, nil
	}

	switch {
	case fact.SourceSequence > source.LastSequence:
		acceptance := acceptor.admit(source, fact, digest, role)
		acceptance.FoldPosition = folded
		return acceptance, nil
	default:
		acceptance := acceptor.replay(source, fact, digest, role)
		acceptance.FoldPosition = folded
		return acceptance, nil
	}
}

// integrityOf snapshots a source's stream integrity for the caller.
func integrityOf(source *SourceState) SourceIntegrity {
	if source == nil {
		return SourceIntegrity{}
	}
	return SourceIntegrity{
		BootID:          source.BootID,
		LastSequence:    source.LastSequence,
		Gaps:            append([]GapRange(nil), source.Gaps...),
		GapOverflow:     source.GapOverflow,
		PendingBaseline: append([]connectivity.Component(nil), source.PendingBaseline...),
		Conflicts:       source.Conflicts,
		Recent:          append([]RecentDigest(nil), source.Recent...),
	}
}

func containsComponent(list []connectivity.Component, value connectivity.Component) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// owesBaseline puts every component the source speaks about back on the hook.
//
// It is called wherever the stream stopped being provably continuous — a hole,
// a conflicting reuse, a new boot — because all three lose the same thing: the
// guarantee that what survived accounts for what did not.
func owesBaseline(source *SourceState, id connectivity.SourceID) {
	declared := safety.ConnectivitySourceComponents(id)
	for _, component := range declared {
		if !containsComponent(source.PendingBaseline, component) {
			source.PendingBaseline = append(source.PendingBaseline, component)
		}
	}
	sort.Slice(source.PendingBaseline, func(i, j int) bool {
		return source.PendingBaseline[i] < source.PendingBaseline[j]
	})
}

// settleBaseline records that one component restated itself in full.
//
// The debt is only discharged when the last component pays it. A baseline for
// physical_network says what physical_network is now; it does not say what the
// hole held for default_path, which the same source also speaks about.
func settleBaseline(source *SourceState, component connectivity.Component) bool {
	remaining := source.PendingBaseline[:0]
	for _, pending := range source.PendingBaseline {
		if pending != component {
			remaining = append(remaining, pending)
		}
	}
	source.PendingBaseline = remaining
	if len(source.PendingBaseline) > 0 {
		return false
	}
	source.PendingBaseline = nil
	return true
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
		acceptor.recordGap(source, gap, fact.SourceID)
		acceptance.OpenedGap = &gap
		acceptance.Reason = ReasonSequenceGap
	}

	// Only a complete restatement can close holes: the later state is known,
	// but what happened inside the hole is not, and a non-baseline fact does
	// not claim to describe it. One component restating itself settles its own
	// share of the debt; the holes close when the last share is settled.
	if fact.Baseline && source.AwaitingBaseline() {
		if settleBaseline(source, fact.Component) {
			acceptance.ClearedGaps = append([]GapRange(nil), source.Gaps...)
			source.Gaps = nil
			source.GapOverflow = false
			source.Conflicts = 0
			acceptance.Reason = ReasonBaselineAccepted
		} else if acceptance.Reason == ReasonNone {
			acceptance.Reason = ReasonBaselinePending
		}
	}

	source.LastSequence = fact.SourceSequence
	source.Recent = append(source.Recent, RecentDigest{
		Sequence: fact.SourceSequence, Digest: digest,
	})
	if len(source.Recent) > RetryWindow {
		source.Recent = append([]RecentDigest(nil), source.Recent[len(source.Recent)-RetryWindow:]...)
	}

	acceptor.state.HostSequence++
	acceptance.HostSequence = acceptor.state.HostSequence
	acceptance.Source = integrityOf(source)
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
				Role: role, Digest: digest, Source: integrityOf(source),
			}
		}
		// A reused identity carrying different content means the stream can no
		// longer be trusted to be continuous, so it owes the same complete
		// restatement a hole owes.
		source.Conflicts++
		owesBaseline(source, fact.SourceID)
		return Acceptance{
			Outcome: OutcomeConflict, Reason: ReasonIdentityReused,
			Role: role, Digest: digest, Source: integrityOf(source),
		}
	}
	// Inside a recorded hole the sequence was never accepted, so a late
	// arrival there is behind the watermark rather than a reuse.
	reason := ReasonBeyondRetry
	if inGap(source.Gaps, fact.SourceSequence) {
		reason = ReasonBehindWatermark
	}
	return Acceptance{
		Outcome: OutcomeStale, Reason: reason,
		Role: role, Digest: digest, Source: integrityOf(source),
	}
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
func (acceptor *Acceptor) recordGap(
	source *SourceState,
	gap GapRange,
	id connectivity.SourceID,
) {
	source.Gaps = append(source.Gaps, gap)
	sort.Slice(source.Gaps, func(i, j int) bool {
		return source.Gaps[i].From < source.Gaps[j].From
	})
	if len(source.Gaps) > MaxGapRanges {
		source.Gaps = append(
			[]GapRange(nil), source.Gaps[len(source.Gaps)-MaxGapRanges:]...)
		source.GapOverflow = true
	}
	owesBaseline(source, id)
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
