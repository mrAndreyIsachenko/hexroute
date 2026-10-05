package ipc

import (
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

// The recovery quantities come from one machine, so a payload carries all four
// or none. Some of them present would describe a path maintaining half a
// budget, which no path does.
func TestRecoveryQuantitiesAreReportedTogetherOrNotAtAll(t *testing.T) {
	sound := func() Diagnostics {
		return Diagnostics{
			Status: Status{
				Role:       RoleRoot,
				Mode:       ModeObserveOnly,
				State:      control.StateHealthy,
				Generation: 1,
			},
			LastTick:   10,
			LastReason: control.ReasonProbeSucceeded,
		}
	}
	if !sound().valid() {
		t.Fatal("none of the four present was refused")
	}

	attempts := uint32(0)
	partial := sound()
	partial.Attempts = &attempts
	if partial.valid() {
		t.Fatal("one of the four present was accepted")
	}

	tick := control.Tick(0)
	whole := sound()
	whole.Attempts, whole.RecoveringSince = &attempts, &tick
	whole.NextActionAt, whole.SafeUntil = &tick, &tick
	if !whole.valid() {
		t.Fatal("all four present was refused")
	}

	negative := control.Tick(-1)
	bad := whole
	bad.SafeUntil = &negative
	if bad.valid() {
		t.Fatal("a negative tick was accepted")
	}
}

// A zero a path measured and a quantity a path does not keep are different
// claims, and the shape has to keep them apart.
func TestAMeasuredZeroIsNotAnAbsentQuantity(t *testing.T) {
	zero := uint32(0)
	measured := Diagnostics{Attempts: &zero}
	var absent Diagnostics
	if measured.Attempts == absent.Attempts {
		t.Fatal("a measured zero and an absent quantity are the same value")
	}
	if absent.Attempts != nil {
		t.Fatal("the absent quantity is not nil")
	}
	if *measured.Attempts != 0 {
		t.Fatal("the measured zero is not zero")
	}
}
