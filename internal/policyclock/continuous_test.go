//go:build darwin || linux

package policyclock

import (
	"testing"
	"time"
)

func TestContinuousNowAdvances(t *testing.T) {
	before, err := ContinuousNow()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	after, err := ContinuousNow()
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("continuous clock did not advance: before=%s after=%s", before, after)
	}
}
