package rootdaemon

import (
	"testing"
	"time"
)

// The loop aims at a period rather than waiting one out after the work.
//
// Waiting a whole period after the work adds that work to the interval between
// observations, and that interval is what the tunnel decision compares against a
// wake threshold. On 2026-09-12 a fold costing 32.4 seconds turned a sixty-one
// second period into a ninety-three second gap, which crossed a ninety second
// threshold on a machine that had not slept.
func TestTheNextObservationIsScheduledFromWhenTheLastBegan(t *testing.T) {
	const period = time.Minute
	for _, item := range []struct {
		name  string
		began time.Duration
		ended time.Duration
		want  time.Duration
	}{
		{"a quick cycle waits out the rest of its period",
			0, 2 * time.Second, 58 * time.Second},
		{"the slow fold that started this",
			0, 32400 * time.Millisecond, 27600 * time.Millisecond},
		{"a cycle that spent its period waits not at all",
			0, period, 0},
		{"a cycle that overran does not wait to make up for it",
			0, 5 * period, 0},
		{"the period is measured from the start, not from zero",
			10 * time.Minute, 10*time.Minute + time.Second, 59 * time.Second},
	} {
		t.Run(item.name, func(t *testing.T) {
			got := remainingPeriod(item.began, item.ended, period)
			if got != item.want {
				t.Fatalf("remainingPeriod(%v, %v, %v) = %v, want %v",
					item.began, item.ended, period, got, item.want)
			}
		})
	}
}

// An overrun yields one immediate cycle, never a backlog of them.
//
// Every cycle observes the present, so there is nothing to catch up on, and a
// loop that tried would spend a recovered machine's first minute re-observing
// moments that have passed.
func TestAnOverrunDoesNotAccumulate(t *testing.T) {
	const period = 30 * time.Second
	if waited := remainingPeriod(0, time.Hour, period); waited != 0 {
		t.Fatalf("after an hour-long cycle the loop waits %v, want 0", waited)
	}
	// The cycle after it begins on time again rather than inheriting the debt.
	if waited := remainingPeriod(time.Hour, time.Hour+time.Second, period); waited != 29*time.Second {
		t.Fatalf("the cycle after an overrun waits %v, want 29s", waited)
	}
}
