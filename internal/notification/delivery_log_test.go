package notification

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

// An announcement already made must stay made across a restart.
//
// The specification says a threshold crossing "is not announced again for the
// same generation". Suppression lived in a map built empty by NewService, so it
// lasted the life of the process instead. On 2026-09-07 the user daemon
// restarted twelve times and Notification Center collected five identical
// announcements for one generation — each within a second of a daemon_started.
//
// Five copies of one message is how a person learns to skip it, which is what
// happened: the crossing on 2026-09-09 was delivered, and went unread.
func TestAnAnnouncementIsNotRepeatedAfterARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.json")
	at := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	input := expiryAnnouncement(3)

	first := &fakeNotifier{}
	service := mustServiceWithLog(t, first, path)
	outcome, err := service.Dispatch(context.Background(), input, at)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if outcome.LocalDelivery != LocalDelivered {
		t.Fatalf("first delivery = %q, want %q", outcome.LocalDelivery, LocalDelivered)
	}

	// A new process, the same generation, the same threshold.
	second := &fakeNotifier{}
	restarted := mustServiceWithLog(t, second, path)
	outcome, err = restarted.Dispatch(context.Background(), input, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("dispatch after restart: %v", err)
	}
	if outcome.LocalDelivery != LocalDuplicate {
		t.Fatalf("after a restart the same announcement was %q, want %q",
			outcome.LocalDelivery, LocalDuplicate)
	}
	if len(second.calls) != 0 {
		t.Fatalf("the restarted process announced it again %d times", len(second.calls))
	}
}

// A successor generation is a different announcement and must still arrive.
//
// Remembering across restarts would be worthless if it also silenced the next
// generation's crossing — that is the announcement the operator actually needs.
func TestTheNextGenerationIsStillAnnounced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.json")
	at := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

	service := mustServiceWithLog(t, &fakeNotifier{}, path)
	if _, err := service.Dispatch(context.Background(), expiryAnnouncement(3), at); err != nil {
		t.Fatalf("generation 3: %v", err)
	}

	notifier := &fakeNotifier{}
	restarted := mustServiceWithLog(t, notifier, path)
	outcome, err := restarted.Dispatch(
		context.Background(), expiryAnnouncement(4), at.Add(time.Hour))
	if err != nil {
		t.Fatalf("generation 4: %v", err)
	}
	if outcome.LocalDelivery != LocalDelivered {
		t.Fatalf("generation 4 was %q, want %q", outcome.LocalDelivery, LocalDelivered)
	}
}

// An identity that does not outlive the process must not be remembered.
//
// The Pritunl safe-mode notification is keyed by the daemon's state generation,
// which counts from zero when the daemon starts. Remembering a delivery against
// it would let a restart's counter reach the same number and suppress a genuine
// safe-mode entry — the daemon would stop telling the operator that automatic
// recovery had paused, and nothing would say so.
func TestAnIdentityThatDoesNotOutliveTheProcessIsNotRemembered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.json")
	at := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	input := Input{
		Incident: event.Incident{
			IncidentID: "pritunl-safe-mode",
			Status:     event.IncidentOpened,
			Severity:   event.SeverityWarning,
			Category:   event.IncidentRecoveryBudget,
			Component:  control.ComponentPritunl,
			Generation: 3,
		},
		External: ExternalNotRequired,
	}

	service := mustServiceWithLog(t, &fakeNotifier{}, path)
	if _, err := service.Dispatch(context.Background(), input, at); err != nil {
		t.Fatalf("first: %v", err)
	}

	notifier := &fakeNotifier{}
	restarted := mustServiceWithLog(t, notifier, path)
	outcome, err := restarted.Dispatch(context.Background(), input, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("after restart: %v", err)
	}
	if outcome.LocalDelivery != LocalDelivered {
		t.Fatalf("a safe-mode entry after a restart was %q, want %q — "+
			"its generation counts from zero and cannot identify a delivery",
			outcome.LocalDelivery, LocalDelivered)
	}
}

// The record is bounded, and a damaged one costs the announcement rather than
// the daemon.
func TestTheDeliveryRecordIsBoundedAndSurvivesDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.json")
	at := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

	service := mustServiceWithLog(t, &fakeNotifier{}, path)
	for generation := 1; generation <= maxRememberedDeliveries+20; generation++ {
		if _, err := service.Dispatch(
			context.Background(), expiryAnnouncement(uint64(generation)), at,
		); err != nil {
			t.Fatalf("generation %d: %v", generation, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("nothing was written down")
	}
	remembered := mustServiceWithLog(t, &fakeNotifier{}, path)
	if len(remembered.entries) > maxRememberedDeliveries {
		t.Fatalf("remembered %d deliveries, want at most %d",
			len(remembered.entries), maxRememberedDeliveries)
	}

	// A record that cannot be read is not a reason to stop announcing.
	if err := os.WriteFile(path, []byte("{ this is not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	notifier := &fakeNotifier{}
	recovered := mustServiceWithLog(t, notifier, path)
	outcome, err := recovered.Dispatch(
		context.Background(), expiryAnnouncement(9001), at)
	if err != nil {
		t.Fatalf("dispatch over a damaged record: %v", err)
	}
	if outcome.LocalDelivery != LocalDelivered {
		t.Fatalf("a damaged record cost the announcement: %q", outcome.LocalDelivery)
	}
}

func expiryAnnouncement(generation uint64) Input {
	return Input{
		Incident: event.Incident{
			IncidentID: "policy-expiry-warning",
			Status:     event.IncidentOpened,
			Severity:   event.SeverityWarning,
			Category:   event.IncidentPolicyExpiry,
			Component:  control.ComponentRuntime,
			Generation: generation,
		},
		External: ExternalNotRequired,
		// A policy generation is the same number in the next process.
		DurableGeneration: true,
	}
}

func mustServiceWithLog(t *testing.T, notifier Notifier, path string) *Service {
	t.Helper()
	service, err := NewService(
		Policy{NightStartHour: 23, NightEndHour: 8},
		notifier,
		DeliveryRecordAt(path),
	)
	if err != nil {
		t.Fatalf("NewService() error: %v", err)
	}
	return service
}
