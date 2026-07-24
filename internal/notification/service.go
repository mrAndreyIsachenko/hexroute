package notification

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	maxDeliveryEntries = 256
	retryCooldown      = time.Minute
)

type LocalDelivery string

const (
	LocalNotRequested LocalDelivery = "not_requested"
	LocalDelivered    LocalDelivery = "delivered"
	LocalDuplicate    LocalDelivery = "duplicate"
	LocalCoolingDown  LocalDelivery = "cooling_down"
	LocalFailed       LocalDelivery = "failed"
)

type Notifier interface {
	Deliver(context.Context, Template) error
}

type Outcome struct {
	Decision
	LocalDelivery LocalDelivery
}

type deliveryKey struct {
	incidentID string
	generation uint64
	status     string
	template   Template
}

type deliveryEntry struct {
	at        time.Time
	delivered bool
}

type Service struct {
	mu       sync.Mutex
	policy   Policy
	notifier Notifier
	entries  map[deliveryKey]deliveryEntry
}

func NewService(policy Policy, notifier Notifier) (*Service, error) {
	if !validPolicy(policy) || notifier == nil {
		return nil, ErrInvalidNotificationInput
	}
	return &Service{
		policy:   policy,
		notifier: notifier,
		entries:  make(map[deliveryKey]deliveryEntry),
	}, nil
}

func (service *Service) Dispatch(
	ctx context.Context,
	input Input,
	at time.Time,
) (Outcome, error) {
	if service == nil || service.notifier == nil || ctx == nil {
		return Outcome{}, ErrInvalidNotificationInput
	}
	decision, err := service.policy.Decide(input, at)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{
		Decision:      decision,
		LocalDelivery: LocalNotRequested,
	}
	if !decision.LocalImmediate {
		return outcome, nil
	}

	key := deliveryKey{
		incidentID: input.Incident.IncidentID,
		generation: input.Incident.Generation,
		status:     string(input.Incident.Status),
		template:   decision.Template,
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if entry, exists := service.entries[key]; exists {
		if entry.delivered {
			outcome.LocalDelivery = LocalDuplicate
			return outcome, nil
		}
		if at.Sub(entry.at) < retryCooldown {
			outcome.LocalDelivery = LocalCoolingDown
			return outcome, nil
		}
	}

	if err := service.notifier.Deliver(ctx, decision.Template); err != nil {
		service.record(key, deliveryEntry{at: at})
		outcome.LocalDelivery = LocalFailed
		return outcome, ErrNotificationDelivery
	}
	service.record(key, deliveryEntry{at: at, delivered: true})
	outcome.LocalDelivery = LocalDelivered
	return outcome, nil
}

func (service *Service) record(key deliveryKey, entry deliveryEntry) {
	if len(service.entries) >= maxDeliveryEntries {
		var oldestKey deliveryKey
		var oldest deliveryEntry
		first := true
		for candidateKey, candidate := range service.entries {
			if first || candidate.at.Before(oldest.at) {
				oldestKey = candidateKey
				oldest = candidate
				first = false
			}
		}
		delete(service.entries, oldestKey)
	}
	service.entries[key] = entry
}

func IsDeliveryFailure(err error) bool {
	return errors.Is(err, ErrNotificationDelivery)
}
