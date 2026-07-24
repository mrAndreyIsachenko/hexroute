package alertdelivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Sender interface {
	Send(context.Context, string) error
}

type DeliveryStore interface {
	ClaimDue(context.Context, metadata.UUID, Channel, time.Time, int) ([]Delivery, error)
	Complete(
		context.Context,
		metadata.UUID,
		[]metadata.UUID,
		Completion,
		time.Time,
	) error
}

type Processor struct {
	store    DeliveryStore
	sender   Sender
	workerID metadata.UUID
	now      func() time.Time
	limit    int
}

type ProcessResult struct {
	Delivered int
	Deferred  int
}

func NewProcessor(
	store DeliveryStore,
	sender Sender,
	workerID metadata.UUID,
	now func() time.Time,
	limit int,
) (*Processor, error) {
	if store == nil ||
		sender == nil ||
		metadataUUID(workerID) == "" ||
		now == nil ||
		limit <= 0 ||
		limit > maxClaimBatch {
		return nil, ErrInvalidDelivery
	}
	return &Processor{
		store:    store,
		sender:   sender,
		workerID: workerID,
		now:      now,
		limit:    limit,
	}, nil
}

func (processor *Processor) RunOnce(
	ctx context.Context,
) (ProcessResult, error) {
	if processor == nil ||
		processor.store == nil ||
		processor.sender == nil ||
		ctx == nil {
		return ProcessResult{}, ErrInvalidDelivery
	}
	at := processor.now().UTC()
	if at.IsZero() {
		return ProcessResult{}, ErrInvalidDelivery
	}
	result := ProcessResult{}
	var unavailable bool
	for range processor.limit {
		at = processor.now().UTC()
		if at.IsZero() {
			return result, ErrInvalidDelivery
		}
		deliveries, err := processor.store.ClaimDue(
			ctx,
			processor.workerID,
			ChannelTelegram,
			at,
			1,
		)
		if err != nil {
			return result, err
		}
		if len(deliveries) == 0 {
			break
		}
		delivery := deliveries[0]
		completion := CompletionDelivered
		if sendErr := processor.sender.Send(ctx, renderDelivery(delivery)); sendErr != nil {
			completion = CompletionUnavailable
			unavailable = true
			result.Deferred++
		} else {
			result.Delivered++
		}
		if err := processor.store.Complete(
			ctx,
			processor.workerID,
			[]metadata.UUID{delivery.DeliveryID},
			completion,
			at,
		); err != nil {
			return result, err
		}
	}

	at = processor.now().UTC()
	if at.IsZero() {
		return result, ErrInvalidDelivery
	}
	digest, err := processor.store.ClaimDue(
		ctx,
		processor.workerID,
		ChannelMorningDigest,
		at,
		processor.limit,
	)
	if err != nil {
		return result, err
	}
	if len(digest) > 0 {
		ids := make([]metadata.UUID, 0, len(digest))
		for _, delivery := range digest {
			ids = append(ids, delivery.DeliveryID)
		}
		completion := CompletionDelivered
		if sendErr := processor.sender.Send(ctx, renderDigest(digest)); sendErr != nil {
			completion = CompletionUnavailable
			unavailable = true
			result.Deferred += len(digest)
		} else {
			result.Delivered += len(digest)
		}
		if err := processor.store.Complete(
			ctx,
			processor.workerID,
			ids,
			completion,
			at,
		); err != nil {
			return result, err
		}
	}
	if unavailable {
		return result, ErrTelegramUnavailable
	}
	return result, nil
}

func renderDelivery(delivery Delivery) string {
	action := "no"
	if delivery.Actionable {
		action = "yes"
	}
	return strings.Join([]string{
		"Hexroute alert",
		"State: " + string(delivery.Snapshot.Status),
		"Severity: " + string(delivery.Snapshot.Severity),
		"Component: " + string(delivery.Snapshot.Component),
		"Category: " + string(delivery.Snapshot.Category),
		"Action required: " + action,
		fmt.Sprintf("Generation: %d", delivery.Snapshot.Generation),
		"Inspect the Hexroute dashboard for evidence.",
	}, "\n")
}

func renderDigest(deliveries []Delivery) string {
	var open, acknowledged, resolved int
	for _, delivery := range deliveries {
		switch delivery.Snapshot.Status {
		case cloudincident.StatusOpen:
			open++
		case cloudincident.StatusAcknowledged:
			acknowledged++
		case cloudincident.StatusResolved:
			resolved++
		}
	}
	return strings.Join([]string{
		"Hexroute morning digest",
		fmt.Sprintf("Overnight updates: %d", len(deliveries)),
		fmt.Sprintf("Active: %d", open),
		fmt.Sprintf("Acknowledged: %d", acknowledged),
		fmt.Sprintf("Recovered: %d", resolved),
		"Inspect the Hexroute dashboard for evidence.",
	}, "\n")
}

func IsTelegramUnavailable(err error) bool {
	return errors.Is(err, ErrTelegramUnavailable)
}
