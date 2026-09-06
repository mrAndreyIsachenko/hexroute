// Package configprove records what became of a published configuration
// version.
//
// It records and never instructs. Nothing here can tell a host to change, to
// return, or to do anything at all: the host has already decided, and this is
// the ledger catching up with what the host's own heartbeat said.
package configprove

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// Lifecycle is where a version stands. The values are the ledger's own.
type Lifecycle string

const (
	LifecycleStaged   Lifecycle = "staged"
	LifecycleActive   Lifecycle = "active"
	LifecycleProven   Lifecycle = "proven"
	LifecycleRetired  Lifecycle = "retired"
	LifecycleRejected Lifecycle = "rejected"
)

// Outcome is what one pass concluded.
type Outcome string

const (
	// OutcomeActivated is the first time a host was seen running a version.
	OutcomeActivated Outcome = "activated"
	// OutcomeProven is a version that reported healthy for its whole window.
	OutcomeProven Outcome = "proven"
	// OutcomeRejected is a version that never did, before its deadline.
	OutcomeRejected Outcome = "rejected"
	// OutcomePending is a version still inside its window.
	OutcomePending Outcome = "pending"
	// OutcomeSettled is a version already proven or rejected.
	OutcomeSettled Outcome = "settled"
)

var (
	// ErrProve is a failure to record.
	ErrProve = errors.New("configuration version outcome was not recorded")
	// ErrNoVersion is a target with nothing published for it.
	ErrNoVersion = errors.New("no configuration version for that target")
)

// Version is the ledger row this pass acts on.
type Version struct {
	ConfigVersionID metadata.UUID
	TargetKind      string
	TargetKey       string
	VersionLabel    string
	ContentSHA256   [32]byte
	Lifecycle       Lifecycle
	CreatedAt       time.Time
	ActivatedAt     time.Time
}

// Observation is what the host's signed heartbeat reported, as read by
// whatever could reach it.
type Observation struct {
	Generation string
	Healthy    bool
}

// Ledger is the configuration ledger, in the one direction this needs it.
type Ledger interface {
	LatestVersion(ctx context.Context, kind string, key string) (Version, error)
	MarkActive(ctx context.Context, version Version, deploymentID metadata.UUID, at time.Time) error
	MarkProven(ctx context.Context, version Version, at time.Time) error
	MarkRejected(ctx context.Context, version Version, at time.Time, reason string) error
}

// Prover moves one version through its lifecycle from evidence.
type Prover struct {
	ledger Ledger
	// window is how long a version must report healthy before it is proven.
	window time.Duration
	// deadline is how long it may take to get there. Past it, a version that
	// has not proved is rejected, and the reason is kept.
	deadline time.Duration
}

// New builds a prover. The window must fit inside the deadline: a version that
// could not prove in the time it is given would be rejected while still
// healthy, which would make the record wrong in the one direction that
// matters.
func New(ledger Ledger, window time.Duration, deadline time.Duration) (*Prover, error) {
	if ledger == nil || window <= 0 || deadline <= 0 || deadline <= window {
		return nil, fmt.Errorf("%w: incomplete prover", ErrProve)
	}
	return &Prover{ledger: ledger, window: window, deadline: deadline}, nil
}

// Result is one pass over one target.
type Result struct {
	Outcome      Outcome `json:"outcome"`
	VersionLabel string  `json:"version_label,omitempty"`
	Lifecycle    string  `json:"lifecycle,omitempty"`
	Reason       string  `json:"reason,omitempty"`
}

// Prove records what the observation establishes about the latest version.
func (prover *Prover) Prove(
	ctx context.Context,
	kind string,
	key string,
	observation Observation,
	deploymentID metadata.UUID,
	now time.Time,
) (Result, error) {
	if prover == nil || ctx == nil || now.IsZero() {
		return Result{}, fmt.Errorf("%w: no prover", ErrProve)
	}
	version, err := prover.ledger.LatestVersion(ctx, kind, key)
	if err != nil {
		return Result{}, err
	}
	now = now.UTC()
	if version.Lifecycle == LifecycleProven || version.Lifecycle == LifecycleRejected ||
		version.Lifecycle == LifecycleRetired {
		return Result{
			Outcome:      OutcomeSettled,
			VersionLabel: version.VersionLabel,
			Lifecycle:    string(version.Lifecycle),
		}, nil
	}

	reason := unprovenReason(observation, version.VersionLabel)
	if reason != "" {
		// The version is not serving. Until the deadline that is only news
		// about now, because a host reboots and a transport recovers.
		if now.Sub(version.CreatedAt) < prover.deadline {
			return Result{
				Outcome:      OutcomePending,
				VersionLabel: version.VersionLabel,
				Lifecycle:    string(version.Lifecycle),
				Reason:       reason,
			}, nil
		}
		if err := prover.ledger.MarkRejected(ctx, version, now, reason); err != nil {
			return Result{}, err
		}
		return Result{
			Outcome:      OutcomeRejected,
			VersionLabel: version.VersionLabel,
			Lifecycle:    string(LifecycleRejected),
			Reason:       reason,
		}, nil
	}

	if version.Lifecycle == LifecycleStaged {
		// The first sight of a host running this version. It is deployed and
		// it is not proven: one healthy answer is not a window.
		if err := prover.ledger.MarkActive(ctx, version, deploymentID, now); err != nil {
			return Result{}, err
		}
		return Result{
			Outcome:      OutcomeActivated,
			VersionLabel: version.VersionLabel,
			Lifecycle:    string(LifecycleActive),
		}, nil
	}
	if version.ActivatedAt.IsZero() || now.Sub(version.ActivatedAt) < prover.window {
		return Result{
			Outcome:      OutcomePending,
			VersionLabel: version.VersionLabel,
			Lifecycle:    string(version.Lifecycle),
		}, nil
	}
	if err := prover.ledger.MarkProven(ctx, version, now); err != nil {
		return Result{}, err
	}
	return Result{
		Outcome:      OutcomeProven,
		VersionLabel: version.VersionLabel,
		Lifecycle:    string(LifecycleProven),
	}, nil
}

// unprovenReason names what the heartbeat was missing, or is empty when it was
// missing nothing.
func unprovenReason(observation Observation, label string) string {
	switch {
	case observation.Generation == "":
		return "heartbeat_unavailable"
	case observation.Generation != label:
		return "generation_not_running"
	case !observation.Healthy:
		return "transport_unhealthy"
	default:
		return ""
	}
}
