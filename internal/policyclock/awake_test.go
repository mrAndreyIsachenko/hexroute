package policyclock

import (
	"runtime"
	"testing"
	"time"
)

// The two clocks are only useful as a pair: the sleep a host had is the
// difference between them. A build where they are the same clock would report
// every sleep as eligible time and quietly qualify a host that was not there.
func TestTheAwakeClockIsNotTheContinuousClock(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no continuous clock on this platform")
	}
	continuous, err := ContinuousNow()
	if err != nil {
		t.Fatalf("continuous: %v", err)
	}
	awake, err := AwakeNow()
	if err != nil {
		t.Fatalf("awake: %v", err)
	}
	if continuous <= 0 || awake <= 0 {
		t.Fatalf("clocks read %v and %v", continuous, awake)
	}
	// Sleep never runs backwards, so the clock that counts through it can
	// never be behind the one that does not.
	if awake > continuous+time.Second {
		t.Fatalf("the awake clock (%v) is ahead of the continuous clock (%v); "+
			"they are not the pair this depends on", awake, continuous)
	}
	if awake == continuous {
		t.Fatalf("both clocks read %v; a sleep would be indistinguishable from "+
			"observing time", awake)
	}
}

// Both must advance while the process runs, or an eligible window measured
// from them would be zero for ever.
func TestBothClocksAdvance(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no continuous clock on this platform")
	}
	firstContinuous, _ := ContinuousNow()
	firstAwake, _ := AwakeNow()
	time.Sleep(2 * time.Millisecond)
	secondContinuous, _ := ContinuousNow()
	secondAwake, _ := AwakeNow()
	if secondContinuous <= firstContinuous {
		t.Fatalf("the continuous clock did not advance: %v then %v",
			firstContinuous, secondContinuous)
	}
	if secondAwake <= firstAwake {
		t.Fatalf("the awake clock did not advance: %v then %v",
			firstAwake, secondAwake)
	}
}
