// Package cloudconnectivity stores the redacted connectivity projection a host
// uploaded, and reads it back for the dashboard.
//
// It is a read model in the strict sense: it derives nothing, decides nothing
// and is never consulted by a host. There is no path from anything written
// here back to a local reduction, a policy generation or a proposal — the
// package exports no writer a local runtime could call, and the local read
// model is forbidden from importing it at all.
//
// What it does have to get right is order. A projection that arrives late must
// not overwrite a newer one, because a stale cloud row rendered as current is
// the one way telemetry could mislead an operator about a host it cannot
// reach.
package cloudconnectivity

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

var (
	ErrInvalidProjection = errors.New("connectivity projection is invalid")
	ErrProjectionStore   = errors.New("connectivity projection store unavailable")
)

// Component is one component's state as the cloud holds it.
type Component struct {
	Component  string
	State      string
	Freshness  string
	DiffReason string
}

// ProposalClass counts proposals of one class without naming any.
type ProposalClass struct {
	Class string
	Count uint16
}

// Snapshot is one node's latest stored projection.
type Snapshot struct {
	NodeID     metadata.UUID
	EventID    metadata.UUID
	SessionID  metadata.UUID
	Sequence   uint64
	ObservedAt time.Time

	SnapshotGeneration uint64
	ReducerVersion     uint16
	BundleGeneration   uint64
	RootGeneration     uint64
	UserGeneration     uint64

	Aggregate           string
	Authorization       string
	AuthorizationReason string

	OpenGaps         uint16
	GapOverflow      bool
	SourceConflicts  uint16
	AwaitingBaseline uint16
	ConflictOverflow bool
	// LineageReset records that a later projection carried a lower snapshot
	// generation, which is what a host that could not recover its read-model
	// lineage looks like from here.
	LineageReset bool
	UpdatedAt    time.Time

	Components      []Component
	ProposalClasses []ProposalClass
}

// Stale reports whether the stored projection is older than the freshness the
// caller expects. The cloud says a row is old; it never repairs one.
func (snapshot Snapshot) Stale(at time.Time, after time.Duration) bool {
	if after <= 0 || snapshot.ObservedAt.IsZero() {
		return true
	}
	return snapshot.ObservedAt.Add(after).Before(at.UTC())
}
