package policyclock

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	bootOne metadata.UUID = "123e4567-e89b-42d3-a456-426614174000"
	bootTwo metadata.UUID = "223e4567-e89b-42d3-a456-426614174000"
)

func TestGuardRejectsWallRollbackAndExcessiveForwardSkew(t *testing.T) {
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		next Sample
	}{
		{name: "rollback", next: Sample{WallUTC: base.Add(-3 * time.Minute), MonotonicNS: int64(11 * time.Second)}},
		{name: "forward", next: Sample{WallUTC: base.Add(4 * time.Minute), MonotonicNS: int64(11 * time.Second)}},
		{name: "monotonic rollback", next: Sample{WallUTC: base, MonotonicNS: int64(9 * time.Second)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			guard, err := NewGuard(DefaultMaxWallSkew)
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Observe(Sample{WallUTC: base, MonotonicNS: int64(10 * time.Second)}); err != nil {
				t.Fatal(err)
			}
			if err := guard.Observe(test.next); !errors.Is(err, ErrClockAnomaly) {
				t.Fatalf("Observe() error = %v", err)
			}
		})
	}
}

func TestGuardAcceptsContinuousTimeAcrossSleep(t *testing.T) {
	guard, err := NewGuard(DefaultMaxWallSkew)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := guard.Observe(Sample{WallUTC: base, MonotonicNS: 0}); err != nil {
		t.Fatal(err)
	}
	slept := 8 * time.Hour
	if err := guard.Observe(Sample{WallUTC: base.Add(slept), MonotonicNS: int64(slept)}); err != nil {
		t.Fatalf("continuous sleep sample: %v", err)
	}
}

func TestPendingLeaseUsesMonotonicExpiryAndBootIdentity(t *testing.T) {
	lease := syntheticLease()
	if err := ValidatePendingLease(lease, LeaseSample{MonotonicNS: int64(15 * time.Second), BootID: bootOne}); err != nil {
		t.Fatalf("valid lease: %v", err)
	}
	if err := ValidatePendingLease(lease, LeaseSample{MonotonicNS: int64(20 * time.Second), BootID: bootOne}); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("sleep-counted expiry error = %v", err)
	}
	if err := ValidatePendingLease(lease, LeaseSample{MonotonicNS: int64(15 * time.Second), BootID: bootTwo}); !errors.Is(err, ErrLeaseBootMismatch) {
		t.Fatalf("boot mismatch error = %v", err)
	}
	if err := ValidatePendingLease(lease, LeaseSample{MonotonicNS: int64(9 * time.Second), BootID: bootOne}); !errors.Is(err, ErrClockAnomaly) {
		t.Fatalf("monotonic rollback error = %v", err)
	}
}

func TestUnfinishedPreRebootLeaseCannotResume(t *testing.T) {
	lease := syntheticLease()
	if err := ValidatePendingLease(
		lease,
		LeaseSample{MonotonicNS: int64(15 * time.Second), BootID: bootOne},
	); err != nil {
		t.Fatalf("lease before reboot: %v", err)
	}
	if err := ValidatePendingLease(
		lease,
		LeaseSample{MonotonicNS: int64(time.Second), BootID: bootTwo},
	); !errors.Is(err, ErrLeaseBootMismatch) {
		t.Fatalf("pre-reboot lease error = %v", err)
	}
}

func syntheticLease() policy.ActionLease {
	return policy.ActionLease{
		Schema:   policy.ActionLeaseSchema,
		ActionID: bootOne, Domain: policy.DomainUser,
		Capability:       policy.CapabilityOperatorResume,
		BundleGeneration: 3, DomainPolicyGeneration: 2,
		ControlStateGeneration: 8, Target: "pritunl",
		PlanSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IssuedAt:   "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-01T00:00:10Z",
		IssuedMonotonicNS: int64(10 * time.Second), ExpiresMonotonicNS: int64(20 * time.Second),
		BootID: bootOne, Nonce: bootTwo, Status: policy.LeasePending,
	}
}
