package userdaemon

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/notification"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyexpiry"
)

type stubAnnouncer struct {
	stage      policyexpiry.Stage
	generation uint64
	ok         bool
	calls      int
}

func (announcer *stubAnnouncer) ExpiryAnnouncement(time.Time) (policyexpiry.Stage, uint64, bool) {
	announcer.calls++
	return announcer.stage, announcer.generation, announcer.ok
}

func expiryTestLogger(t *testing.T) *logging.Logger {
	t.Helper()
	logger, err := logging.New(&bytes.Buffer{}, logging.ComponentUser)
	if err != nil {
		t.Fatal(err)
	}
	return logger
}

func TestPolicyExpiryAnnouncesEachStageWithItsOwnIdentity(t *testing.T) {
	for _, testCase := range []struct {
		stage policyexpiry.Stage
		want  string
	}{
		{policyexpiry.StageWarning, "policy-expiry-warning"},
		{policyexpiry.StageUrgent, "policy-expiry-urgent"},
		{policyexpiry.StageLapsed, "policy-expiry-lapsed"},
	} {
		t.Run(string(testCase.stage), func(t *testing.T) {
			notifier := &fakeIncidentNotifier{}
			dispatchPolicyExpiryNotification(
				context.Background(), notifier,
				&stubAnnouncer{stage: testCase.stage, generation: 3, ok: true},
				time.Now(), expiryTestLogger(t),
			)
			if len(notifier.calls) != 1 {
				t.Fatalf("dispatched %d notifications, want 1", len(notifier.calls))
			}
			incident := notifier.calls[0].Incident
			if incident.IncidentID != testCase.want {
				t.Fatalf("incident id = %q, want %q", incident.IncidentID, testCase.want)
			}
			if incident.Category != event.IncidentPolicyExpiry {
				t.Fatalf("category = %q; borrowing another one tells the operator the wrong thing",
					incident.Category)
			}
			if incident.Component != control.ComponentRuntime {
				t.Fatalf("component = %q, want runtime", incident.Component)
			}
			if incident.Severity == event.SeverityCritical {
				t.Fatal("a deadline was raised as critical; critical bypasses the night window")
			}
			if incident.Generation != 3 {
				t.Fatalf("generation = %d, want the active bundle", incident.Generation)
			}
		})
	}
}

func TestPolicyExpiryAnnouncesNothingWhileThereIsTime(t *testing.T) {
	for _, announcer := range []*stubAnnouncer{
		{stage: policyexpiry.StageNone, generation: 3, ok: true},
		{stage: policyexpiry.StageWarning, generation: 3, ok: false},
		{stage: policyexpiry.StageWarning, generation: 0, ok: true},
	} {
		notifier := &fakeIncidentNotifier{}
		dispatchPolicyExpiryNotification(
			context.Background(), notifier, announcer, time.Now(), expiryTestLogger(t),
		)
		if len(notifier.calls) != 0 {
			t.Fatalf("announced %d times with nothing to announce", len(notifier.calls))
		}
	}
}

// TestPolicyExpiryDefersToTheMorningInsideTheNightWindow is why the incident is
// warning rather than critical: severity is the only thing that decides whether
// the night window is respected.
func TestPolicyExpiryDefersToTheMorningInsideTheNightWindow(t *testing.T) {
	policy := notification.Policy{NightStartHour: 23, NightEndHour: 8}
	incident := event.Incident{
		IncidentID: "policy-expiry-warning",
		Status:     event.IncidentOpened,
		Severity:   event.SeverityWarning,
		Category:   event.IncidentPolicyExpiry,
		Component:  control.ComponentRuntime,
		Generation: 3,
	}
	decision, err := policy.Decide(
		notification.Input{Incident: incident, External: notification.ExternalNotRequired},
		time.Date(2026, time.September, 10, 3, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.LocalImmediate {
		t.Fatal("a deadline woke the operator at three in the morning")
	}

	daytime, err := policy.Decide(
		notification.Input{Incident: incident, External: notification.ExternalNotRequired},
		time.Date(2026, time.September, 10, 14, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !daytime.LocalImmediate || daytime.Template != notification.TemplatePolicyExpiry {
		t.Fatalf("daytime decision = %+v, want an immediate policy-expiry notification", daytime)
	}
}
