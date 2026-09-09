package notification

import (
	"context"
	"errors"
	"sort"
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
	// durable marks an entry worth carrying past this process. Only a caller
	// can say so, because only the caller knows what its generation counts.
	durable bool
}

type Service struct {
	mu       sync.Mutex
	policy   Policy
	notifier Notifier
	entries  map[deliveryKey]deliveryEntry
	// record carries the deliveries whose identity outlives this process.
	//
	// Without it, suppression lasted the life of the process rather than the
	// life of the generation: twelve restarts on 2026-09-07 put five identical
	// expiry announcements in front of the operator, which is how the one that
	// mattered two days later went unread.
	record DeliveryRecord
}

func NewService(
	policy Policy,
	notifier Notifier,
	record DeliveryRecord,
) (*Service, error) {
	if !validPolicy(policy) || notifier == nil {
		return nil, ErrInvalidNotificationInput
	}
	service := &Service{
		policy:   policy,
		notifier: notifier,
		entries:  make(map[deliveryKey]deliveryEntry),
		record:   record,
	}
	if record != nil {
		for _, remembered := range record.Load() {
			service.entries[deliveryKey{
				incidentID: remembered.IncidentID,
				generation: remembered.Generation,
				status:     remembered.Status,
				template:   remembered.Template,
			}] = deliveryEntry{
				at: remembered.At, delivered: true, durable: true,
			}
		}
	}
	return service, nil
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
		service.remember(key, deliveryEntry{at: at})
		outcome.LocalDelivery = LocalFailed
		return outcome, ErrNotificationDelivery
	}
	service.remember(key, deliveryEntry{
		at: at, delivered: true, durable: input.DurableGeneration,
	})
	outcome.LocalDelivery = LocalDelivered
	return outcome, nil
}

func (service *Service) remember(key deliveryKey, entry deliveryEntry) {
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
	if entry.durable && entry.delivered {
		service.writeRecord()
	}
}

// writeRecord persists the deliveries that outlive this process, newest last.
//
// A failure to write is not reported to the caller and does not fail the
// dispatch: the announcement has already been made, and the record exists so
// the next process does not repeat it. Losing it costs one repeated
// announcement, which is what happened before it existed.
func (service *Service) writeRecord() {
	if service.record == nil {
		return
	}
	durable := make([]rememberedDelivery, 0, len(service.entries))
	for key, entry := range service.entries {
		if !entry.durable || !entry.delivered {
			continue
		}
		durable = append(durable, rememberedDelivery{
			IncidentID: key.incidentID,
			Generation: key.generation,
			Status:     key.status,
			Template:   key.template,
			At:         entry.at,
		})
	}
	sort.Slice(durable, func(one, other int) bool {
		return durable[one].At.Before(durable[other].At)
	})
	service.record.Save(durable)
}

func IsDeliveryFailure(err error) bool {
	return errors.Is(err, ErrNotificationDelivery)
}
