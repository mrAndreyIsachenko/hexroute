package alertdelivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type fakeDeliveryStore struct {
	telegram  []Delivery
	digest    []Delivery
	completed [][]metadata.UUID
	outcomes  []Completion
}

func (store *fakeDeliveryStore) ClaimDue(
	_ context.Context,
	_ metadata.UUID,
	channel Channel,
	_ time.Time,
	_ int,
) ([]Delivery, error) {
	switch channel {
	case ChannelTelegram:
		if len(store.telegram) == 0 {
			return nil, nil
		}
		result := []Delivery{store.telegram[0]}
		store.telegram = store.telegram[1:]
		return result, nil
	case ChannelMorningDigest:
		result := store.digest
		store.digest = nil
		return result, nil
	default:
		return nil, ErrInvalidDelivery
	}
}

func (store *fakeDeliveryStore) Complete(
	_ context.Context,
	_ metadata.UUID,
	deliveryIDs []metadata.UUID,
	completion Completion,
	_ time.Time,
) error {
	store.completed = append(store.completed, append([]metadata.UUID(nil), deliveryIDs...))
	store.outcomes = append(store.outcomes, completion)
	return nil
}

type fakeSender struct {
	messages []string
	err      error
}

func (sender *fakeSender) Send(_ context.Context, message string) error {
	sender.messages = append(sender.messages, message)
	return sender.err
}

func TestProcessorBatchesDigestAndCompletesEveryDelivery(t *testing.T) {
	now := time.Date(2026, time.July, 25, 8, 1, 0, 0, time.UTC)
	store := &fakeDeliveryStore{
		telegram: []Delivery{processorDelivery(
			"11111111-1111-4111-8111-111111111111",
			cloudincident.StatusOpen,
		)},
		digest: []Delivery{
			processorDelivery(
				"22222222-2222-4222-8222-222222222222",
				cloudincident.StatusResolved,
			),
			processorDelivery(
				"33333333-3333-4333-8333-333333333333",
				cloudincident.StatusOpen,
			),
		},
	}
	sender := &fakeSender{}
	processor, err := NewProcessor(
		store,
		sender,
		alertWorkerOneID,
		func() time.Time { return now },
		10,
	)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	result, err := processor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Delivered != 3 || result.Deferred != 0 ||
		len(sender.messages) != 2 ||
		len(store.completed) != 2 ||
		len(store.completed[1]) != 2 {
		t.Fatalf(
			"result=%+v messages=%d completed=%+v",
			result,
			len(sender.messages),
			store.completed,
		)
	}
	if !strings.Contains(sender.messages[1], "Overnight updates: 2") ||
		!strings.Contains(sender.messages[1], "Recovered: 1") ||
		!strings.Contains(sender.messages[1], "Active: 1") {
		t.Fatalf("digest = %q", sender.messages[1])
	}
}

func TestProcessorPersistsGenericRetryAfterTelegramFailure(t *testing.T) {
	store := &fakeDeliveryStore{
		telegram: []Delivery{processorDelivery(
			"44444444-4444-4444-8444-444444444444",
			cloudincident.StatusOpen,
		)},
	}
	sender := &fakeSender{err: errors.New("HEXROUTE_CANARY_TRANSPORT_SECRET")}
	processor, err := NewProcessor(
		store,
		sender,
		alertWorkerOneID,
		time.Now,
		10,
	)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	result, err := processor.RunOnce(context.Background())
	if !errors.Is(err, ErrTelegramUnavailable) ||
		strings.Contains(err.Error(), "HEXROUTE_CANARY") ||
		result.Deferred != 1 ||
		len(store.outcomes) != 1 ||
		store.outcomes[0] != CompletionUnavailable {
		t.Fatalf("result=%+v error=%v outcomes=%+v", result, err, store.outcomes)
	}
}

func processorDelivery(
	deliveryID metadata.UUID,
	status cloudincident.Status,
) Delivery {
	return Delivery{
		DeliveryID: deliveryID,
		Snapshot: Snapshot{
			IncidentID:     policyIncidentID,
			Generation:     1,
			Status:         status,
			Severity:       event.SeverityWarning,
			Category:       event.IncidentAvailability,
			Component:      control.ComponentRuntime,
			TransitionedAt: time.Now(),
		},
		Channel:    ChannelTelegram,
		Actionable: status != cloudincident.StatusResolved,
	}
}
