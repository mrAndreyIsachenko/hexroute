package notification

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

type fakeNotifier struct {
	calls []Template
	err   error
}

func (notifier *fakeNotifier) Deliver(
	_ context.Context,
	template Template,
) error {
	notifier.calls = append(notifier.calls, template)
	return notifier.err
}

func TestServiceDeliversOnceAndRetainsExternalPending(t *testing.T) {
	notifier := &fakeNotifier{}
	service := mustService(t, notifier)
	at := time.Date(2026, 7, 25, 2, 0, 0, 0, time.UTC)
	input := Input{
		Incident: incident(
			event.IncidentOpened,
			event.SeverityCritical,
			event.IncidentAvailability,
			control.ComponentTelegram,
		),
		External: ExternalPending,
	}

	first, err := service.Dispatch(context.Background(), input, at)
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if first.LocalDelivery != LocalDelivered ||
		!first.ExternalPending ||
		len(notifier.calls) != 1 {
		t.Fatalf("first=%+v calls=%v", first, notifier.calls)
	}
	second, err := service.Dispatch(context.Background(), input, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("duplicate Dispatch() error: %v", err)
	}
	if second.LocalDelivery != LocalDuplicate ||
		!second.ExternalPending ||
		len(notifier.calls) != 1 {
		t.Fatalf("second=%+v calls=%v", second, notifier.calls)
	}
}

func TestServiceRetriesFailureOnlyAfterCooldown(t *testing.T) {
	notifier := &fakeNotifier{err: ErrNotificationDelivery}
	service := mustService(t, notifier)
	at := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	input := Input{
		Incident: incident(
			event.IncidentUpdated,
			event.SeverityWarning,
			event.IncidentRecoveryBudget,
			control.ComponentPritunl,
		),
		External: ExternalNotRequired,
	}

	failed, err := service.Dispatch(context.Background(), input, at)
	if !errors.Is(err, ErrNotificationDelivery) ||
		failed.LocalDelivery != LocalFailed ||
		len(notifier.calls) != 1 {
		t.Fatalf("failed=%+v err=%v calls=%v", failed, err, notifier.calls)
	}
	cooling, err := service.Dispatch(
		context.Background(),
		input,
		at.Add(retryCooldown-time.Second),
	)
	if err != nil ||
		cooling.LocalDelivery != LocalCoolingDown ||
		len(notifier.calls) != 1 {
		t.Fatalf("cooling=%+v err=%v calls=%v", cooling, err, notifier.calls)
	}
	notifier.err = nil
	retried, err := service.Dispatch(
		context.Background(),
		input,
		at.Add(retryCooldown),
	)
	if err != nil ||
		retried.LocalDelivery != LocalDelivered ||
		len(notifier.calls) != 2 {
		t.Fatalf("retried=%+v err=%v calls=%v", retried, err, notifier.calls)
	}
}

func TestServiceSuppressesNonActionableAndBoundsDedupeState(t *testing.T) {
	notifier := &fakeNotifier{}
	service := mustService(t, notifier)
	at := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	nonActionable := Input{
		Incident: incident(
			event.IncidentOpened,
			event.SeverityWarning,
			event.IncidentAvailability,
			control.ComponentTelegram,
		),
		External: ExternalNotRequired,
	}
	outcome, err := service.Dispatch(context.Background(), nonActionable, at)
	if err != nil ||
		outcome.LocalDelivery != LocalNotRequested ||
		len(notifier.calls) != 0 {
		t.Fatalf("outcome=%+v err=%v calls=%v", outcome, err, notifier.calls)
	}

	for index := 0; index < maxDeliveryEntries+20; index++ {
		input := Input{
			Incident: incident(
				event.IncidentOpened,
				event.SeverityCritical,
				event.IncidentAvailability,
				control.ComponentTunnel,
			),
			External: ExternalDelivered,
		}
		input.Incident.IncidentID = fmt.Sprintf("incident-%03d", index)
		input.Incident.Generation = uint64(index + 1)
		if _, err := service.Dispatch(
			context.Background(),
			input,
			at.Add(time.Duration(index)*time.Second),
		); err != nil {
			t.Fatalf("Dispatch(%d) error: %v", index, err)
		}
	}
	if len(service.entries) != maxDeliveryEntries {
		t.Fatalf("dedupe entries = %d, want %d", len(service.entries), maxDeliveryEntries)
	}
}

func mustService(t *testing.T, notifier Notifier) *Service {
	t.Helper()
	service, err := NewService(
		Policy{NightStartHour: 23, NightEndHour: 8},
		notifier,
	)
	if err != nil {
		t.Fatalf("NewService() error: %v", err)
	}
	return service
}
