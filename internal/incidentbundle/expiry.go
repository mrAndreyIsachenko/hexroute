package incidentbundle

import (
	"context"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type DeletionStore interface {
	ClaimExpired(context.Context, metadata.UUID, time.Time, int) ([]Deletion, error)
	CompleteDeletion(
		context.Context,
		metadata.UUID,
		Deletion,
		DeletionCompletion,
		time.Time,
	) error
}

type ExpiryWorker struct {
	store    DeletionStore
	storage  Storage
	workerID metadata.UUID
	now      func() time.Time
	limit    int
}

type ExpiryResult struct {
	Deleted  int
	Deferred int
}

func NewExpiryWorker(
	store DeletionStore,
	storage Storage,
	workerID metadata.UUID,
	now func() time.Time,
	limit int,
) (*ExpiryWorker, error) {
	if store == nil ||
		storage == nil ||
		!validUUID(workerID) ||
		now == nil ||
		limit <= 0 ||
		limit > maxDeleteBatch {
		return nil, ErrInvalidBundle
	}
	return &ExpiryWorker{
		store:    store,
		storage:  storage,
		workerID: workerID,
		now:      now,
		limit:    limit,
	}, nil
}

func (worker *ExpiryWorker) RunOnce(ctx context.Context) (ExpiryResult, error) {
	if worker == nil ||
		worker.store == nil ||
		worker.storage == nil ||
		ctx == nil {
		return ExpiryResult{}, ErrInvalidBundle
	}
	result := ExpiryResult{}
	var unavailable bool
	for range worker.limit {
		at := worker.now().UTC()
		if at.IsZero() {
			return result, ErrInvalidBundle
		}
		deletions, err := worker.store.ClaimExpired(
			ctx,
			worker.workerID,
			at,
			1,
		)
		if err != nil {
			return result, err
		}
		if len(deletions) == 0 {
			break
		}
		deletion := deletions[0]
		completion := DeletionSucceeded
		if err := worker.storage.DeletePrivate(ctx, deletion.ObjectKey); err != nil {
			completion = DeletionUnavailable
			unavailable = true
			result.Deferred++
		} else {
			result.Deleted++
		}
		if err := worker.store.CompleteDeletion(
			ctx,
			worker.workerID,
			deletion,
			completion,
			at,
		); err != nil {
			return result, err
		}
	}
	if unavailable {
		return result, ErrObjectStorageUnavailable
	}
	return result, nil
}
