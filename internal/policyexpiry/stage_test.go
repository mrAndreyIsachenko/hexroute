package policyexpiry

import (
	"testing"
	"time"
)

func TestStageAtCrossesEachThresholdOnce(t *testing.T) {
	expiry := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := expiry.Format(time.RFC3339Nano)

	cases := []struct {
		name      string
		remaining time.Duration
		want      Stage
	}{
		{"a month out", 30 * 24 * time.Hour, StageNone},
		{"eight days out", 8 * 24 * time.Hour, StageNone},
		{"exactly seven days out", 7 * 24 * time.Hour, StageWarning},
		{"three days out", 72 * time.Hour, StageWarning},
		{"exactly forty-eight hours out", 48 * time.Hour, StageUrgent},
		{"an hour out", time.Hour, StageUrgent},
		{"at the expiry instant", 0, StageLapsed},
		{"a week after", -7 * 24 * time.Hour, StageLapsed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stage, ok := StageAt(expiresAt, expiry.Add(-testCase.remaining))
			if !ok {
				t.Fatal("StageAt() refused a well-formed expiry")
			}
			if stage != testCase.want {
				t.Fatalf("StageAt() = %q, want %q", stage, testCase.want)
			}
		})
	}
}

func TestStageAtRefusesWhatItCannotRead(t *testing.T) {
	for _, value := range []string{"", "not a timestamp", "2026-09-30"} {
		if _, ok := StageAt(value, time.Now()); ok {
			t.Fatalf("StageAt(%q) claimed to know a deadline it cannot read", value)
		}
	}
	if _, ok := StageAt(time.Now().Format(time.RFC3339Nano), time.Time{}); ok {
		t.Fatal("StageAt() answered without a clock")
	}
}

// TestEachAnnouncedStageHasItsOwnIdentity is what keeps one deadline from
// collapsing into one notification: delivery deduplicates by incident and
// generation, so two stages sharing an identity would announce only once.
func TestEachAnnouncedStageHasItsOwnIdentity(t *testing.T) {
	seen := make(map[string]Stage)
	for _, stage := range []Stage{StageNone, StageWarning, StageUrgent, StageLapsed} {
		id := stage.IncidentID()
		if stage == StageNone {
			if id != "" || stage.Announces() {
				t.Fatal("the ordinary case announces something")
			}
			continue
		}
		if !stage.Announces() {
			t.Fatalf("stage %q announces nothing", stage)
		}
		if previous, exists := seen[id]; exists {
			t.Fatalf("stages %q and %q share the identity %q", previous, stage, id)
		}
		seen[id] = stage
	}
}
