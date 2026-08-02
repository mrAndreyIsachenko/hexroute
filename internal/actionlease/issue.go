package actionlease

import (
	"errors"
	"io"
	"math"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	DefaultTTL = 30 * time.Second
	MaxTTL     = policy.MaxActionLeaseTTL
)

type Store interface {
	Domain() policy.Domain
	PersistActionLease(policy.ActionLease) error
}

type IssueInput struct {
	Domain                 policy.Domain
	Capability             policy.Capability
	BundleGeneration       uint64
	DomainPolicyGeneration uint64
	ControlStateGeneration uint64
	Target                 string
	PlanSHA256             string
	BootID                 metadata.UUID
	TTL                    time.Duration
}

var ErrInvalidIssue = errors.New("invalid action lease issue request")

func IssueAndPersist(
	store Store,
	input IssueInput,
	clock metadata.Clock,
	random io.Reader,
) (policy.ActionLease, error) {
	ttl := input.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if store == nil || clock == nil || !input.Domain.Valid() ||
		store.Domain() != input.Domain || ttl < 0 || ttl > MaxTTL {
		return policy.ActionLease{}, ErrInvalidIssue
	}
	now := clock.WallNow().UTC()
	monotonic := clock.MonotonicNow()
	if now.IsZero() || monotonic < 0 || ttl.Nanoseconds() <= 0 ||
		monotonic.Nanoseconds() > math.MaxInt64-ttl.Nanoseconds() {
		return policy.ActionLease{}, ErrInvalidIssue
	}
	actionID, err := metadata.NewUUID(random)
	if err != nil {
		return policy.ActionLease{}, ErrInvalidIssue
	}
	nonce, err := metadata.NewUUID(random)
	if err != nil {
		return policy.ActionLease{}, ErrInvalidIssue
	}
	lease := policy.ActionLease{
		Schema: policy.ActionLeaseSchema, ActionID: actionID,
		Domain: input.Domain, Capability: input.Capability,
		BundleGeneration:       input.BundleGeneration,
		DomainPolicyGeneration: input.DomainPolicyGeneration,
		ControlStateGeneration: input.ControlStateGeneration,
		Target:                 input.Target, PlanSHA256: input.PlanSHA256,
		IssuedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(ttl).Format(time.RFC3339Nano),
		IssuedMonotonicNS:  monotonic.Nanoseconds(),
		ExpiresMonotonicNS: monotonic.Nanoseconds() + ttl.Nanoseconds(),
		BootID:             input.BootID, Nonce: nonce, Status: policy.LeasePending,
	}
	if lease.Validate() != nil {
		return policy.ActionLease{}, ErrInvalidIssue
	}
	if err := store.PersistActionLease(lease); err != nil {
		return policy.ActionLease{}, err
	}
	return lease, nil
}
