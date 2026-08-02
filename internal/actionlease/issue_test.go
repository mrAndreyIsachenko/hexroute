package actionlease

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type fixedClock struct {
	wall      time.Time
	monotonic time.Duration
}

func (clock fixedClock) WallNow() time.Time          { return clock.wall }
func (clock fixedClock) MonotonicNow() time.Duration { return clock.monotonic }

type recordingStore struct {
	domain policy.Domain
	lease  policy.ActionLease
	err    error
}

func (store *recordingStore) Domain() policy.Domain { return store.domain }
func (store *recordingStore) PersistActionLease(lease policy.ActionLease) error {
	store.lease = lease
	return store.err
}

func TestIssueAndPersistBindsExactAuthorizationContext(t *testing.T) {
	store := &recordingStore{domain: policy.DomainUser}
	clock := fixedClock{
		wall:      time.Date(2030, time.January, 1, 2, 0, 0, 0, time.FixedZone("test", 3600)),
		monotonic: 12 * time.Second,
	}
	random := make([]byte, 32)
	for index := range random {
		random[index] = byte(index + 1)
	}
	input := IssueInput{
		Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume,
		BundleGeneration: 9, DomainPolicyGeneration: 5, ControlStateGeneration: 17,
		Target: "synthetic-target", PlanSHA256: policy.SHA256Hex([]byte("synthetic-plan")),
		BootID: "123e4567-e89b-42d3-a456-426614174000",
	}
	lease, err := IssueAndPersist(store, input, clock, bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	if lease.Validate() != nil || lease != store.lease ||
		lease.Domain != input.Domain || lease.Capability != input.Capability ||
		lease.BundleGeneration != input.BundleGeneration ||
		lease.DomainPolicyGeneration != input.DomainPolicyGeneration ||
		lease.ControlStateGeneration != input.ControlStateGeneration ||
		lease.Target != input.Target || lease.PlanSHA256 != input.PlanSHA256 ||
		lease.BootID != input.BootID || lease.Status != policy.LeasePending ||
		lease.IssuedAt != "2030-01-01T01:00:00Z" ||
		lease.ExpiresAt != "2030-01-01T01:00:30Z" ||
		lease.IssuedMonotonicNS != int64(12*time.Second) ||
		lease.ExpiresMonotonicNS != int64(42*time.Second) ||
		lease.ActionID == lease.Nonce {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestIssueRejectsUnboundedOrMismatchedRequests(t *testing.T) {
	clock := fixedClock{wall: time.Now(), monotonic: time.Second}
	valid := IssueInput{
		Domain: policy.DomainRoot, Capability: policy.CapabilityOperatorResume,
		BundleGeneration: 1, DomainPolicyGeneration: 1, ControlStateGeneration: 1,
		Target: "synthetic", PlanSHA256: policy.SHA256Hex([]byte("plan")),
		BootID: "123e4567-e89b-42d3-a456-426614174000", TTL: DefaultTTL,
	}
	for _, mutate := range []func(*IssueInput){
		func(value *IssueInput) { value.TTL = -time.Nanosecond },
		func(value *IssueInput) { value.TTL = MaxTTL + time.Nanosecond },
		func(value *IssueInput) { value.Domain = policy.DomainUser },
		func(value *IssueInput) { value.PlanSHA256 = "invalid" },
	} {
		candidate := valid
		mutate(&candidate)
		_, err := IssueAndPersist(
			&recordingStore{domain: policy.DomainRoot}, candidate, clock,
			bytes.NewReader(make([]byte, 32)),
		)
		if !errors.Is(err, ErrInvalidIssue) {
			t.Fatalf("IssueAndPersist() error = %v", err)
		}
	}
}
