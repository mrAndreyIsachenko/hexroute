package incidentbundle

import (
	"context"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	Retention       = 30 * 24 * time.Hour
	deleteLease     = 2 * time.Minute
	minRetryDelay   = time.Minute
	maxRetryDelay   = time.Hour
	maxDeleteBatch  = 64
	objectMediaType = "application/json"
)

type PrivateObject struct {
	Key             string
	Content         []byte
	ContentSHA256   [32]byte
	ContentType     string
	ContentEncoding string
	ExpiresAt       time.Time
}

// Storage must keep objects private, apply ExpiresAt as a lifecycle ceiling,
// and make repeated writes of identical content to the same key idempotent.
type Storage interface {
	PutPrivate(context.Context, PrivateObject) error
	DeletePrivate(context.Context, string) error
}

type Bundle struct {
	BundleID        metadata.UUID
	IncidentID      metadata.UUID
	ObjectKey       string
	ContentSHA256   [32]byte
	CompressedBytes int64
	CreatedAt       time.Time
	ExpiresAt       time.Time
	Created         bool
}

type Deletion struct {
	BundleID     metadata.UUID
	ObjectKey    string
	Attempt      uint32
	ClaimedUntil time.Time
}

type DeletionCompletion string

const (
	DeletionSucceeded   DeletionCompletion = "succeeded"
	DeletionUnavailable DeletionCompletion = "unavailable"
)

var (
	ErrInvalidBundle            = errors.New("invalid incident bundle request")
	ErrIncidentNotFound         = errors.New("incident not found")
	ErrNoIncidentEvidence       = errors.New("incident has no retained evidence")
	ErrBundleClaimLost          = errors.New("incident bundle deletion claim lost")
	ErrBundleNotFound           = errors.New("incident bundle not found")
	ErrObjectStorageUnavailable = errors.New("private object storage unavailable")
)
