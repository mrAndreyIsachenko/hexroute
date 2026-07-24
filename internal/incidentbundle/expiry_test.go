package incidentbundle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

func TestExpiryWorkerDefersDeletionWithoutRecordingStorageDetails(t *testing.T) {
	at := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	workerID := metadata.UUID("11111111-1111-4111-8111-111111111111")
	deletion := Deletion{
		BundleID:  "22222222-2222-4222-8222-222222222222",
		ObjectKey: "incident-bundles/sha256/aa/fixture.json.gz",
		Attempt:   1,
	}
	store := &expiryStoreFixture{claimed: []Deletion{deletion}}
	storage := &storageFixture{deleteErr: errors.New("provider detail must not persist")}
	worker, err := NewExpiryWorker(store, storage, workerID, func() time.Time {
		return at
	}, 1)
	if err != nil {
		t.Fatalf("NewExpiryWorker() error = %v", err)
	}

	result, err := worker.RunOnce(context.Background())
	if !errors.Is(err, ErrObjectStorageUnavailable) {
		t.Fatalf("RunOnce() error = %v, want %v", err, ErrObjectStorageUnavailable)
	}
	if result.Deferred != 1 || result.Deleted != 0 {
		t.Fatalf("result = %+v", result)
	}
	if store.completed != DeletionUnavailable ||
		store.completedDeletion.BundleID != deletion.BundleID {
		t.Fatalf("completion = %q %+v", store.completed, store.completedDeletion)
	}
}

type expiryStoreFixture struct {
	claimed           []Deletion
	completed         DeletionCompletion
	completedDeletion Deletion
}

func (store *expiryStoreFixture) ClaimExpired(
	context.Context,
	metadata.UUID,
	time.Time,
	int,
) ([]Deletion, error) {
	claimed := store.claimed
	store.claimed = nil
	return claimed, nil
}

func (store *expiryStoreFixture) CompleteDeletion(
	_ context.Context,
	_ metadata.UUID,
	deletion Deletion,
	completion DeletionCompletion,
	_ time.Time,
) error {
	store.completed = completion
	store.completedDeletion = deletion
	return nil
}

type storageFixture struct {
	deleteErr error
}

func (*storageFixture) PutPrivate(context.Context, PrivateObject) error {
	return nil
}

func (storage *storageFixture) DeletePrivate(context.Context, string) error {
	return storage.deleteErr
}
