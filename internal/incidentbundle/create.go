package incidentbundle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Creator struct {
	database Database
	storage  Storage
	random   io.Reader
	randomMu sync.Mutex
}

func NewCreator(
	database Database,
	storage Storage,
	randomSource io.Reader,
) (*Creator, error) {
	if database == nil || storage == nil {
		return nil, ErrInvalidBundle
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &Creator{
		database: database,
		storage:  storage,
		random:   randomSource,
	}, nil
}

func (creator *Creator) Create(
	ctx context.Context,
	incidentID metadata.UUID,
	at time.Time,
) (bundle Bundle, err error) {
	if creator == nil ||
		creator.database == nil ||
		creator.storage == nil ||
		ctx == nil ||
		!validUUID(incidentID) ||
		at.IsZero() {
		return Bundle{}, ErrInvalidBundle
	}
	at = at.UTC()
	transaction, err := creator.database.BeginTx(
		ctx,
		pgx.TxOptions{IsoLevel: pgx.RepeatableRead},
	)
	if err != nil {
		return Bundle{}, err
	}
	defer rollback(ctx, transaction, &err)
	if _, err = transaction.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"incident-bundle:"+string(incidentID),
	); err != nil {
		return Bundle{}, err
	}

	var snapshotAt time.Time
	err = transaction.QueryRow(ctx, `
		SELECT last_observed_at
		FROM incidents
		WHERE incident_id = $1
	`, string(incidentID)).Scan(&snapshotAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bundle{}, ErrIncidentNotFound
	}
	if err != nil {
		return Bundle{}, err
	}
	entries, err := loadEvidence(ctx, transaction, incidentID)
	if err != nil {
		return Bundle{}, err
	}
	if len(entries) == 0 {
		return Bundle{}, ErrNoIncidentEvidence
	}
	content, err := telemetry.EncodeIncidentBundle(
		string(incidentID),
		snapshotAt.UTC(),
		entries,
	)
	if err != nil {
		return Bundle{}, err
	}
	digest := sha256.Sum256(content)
	existing, err := loadExisting(ctx, transaction, incidentID, digest)
	if err != nil {
		return Bundle{}, err
	}
	if existing != nil && existing.deletedAt == nil {
		if err = transaction.Commit(ctx); err != nil {
			return Bundle{}, err
		}
		return existing.Bundle, nil
	}

	objectKey := objectKey(digest)
	expiresAt := at.Add(Retention)
	if err := creator.storage.PutPrivate(ctx, PrivateObject{
		Key:             objectKey,
		Content:         append([]byte(nil), content...),
		ContentSHA256:   digest,
		ContentType:     objectMediaType,
		ContentEncoding: "gzip",
		ExpiresAt:       expiresAt,
	}); err != nil {
		return Bundle{}, ErrObjectStorageUnavailable
	}
	if existing != nil {
		_, err = transaction.Exec(ctx, `
			UPDATE incident_bundles
			SET object_key = $2,
			    compressed_bytes = $3,
			    created_at = $4,
			    expires_at = $5,
			    deleted_at = NULL,
			    delete_claim_owner = NULL,
			    delete_claim_until = NULL,
			    delete_attempt_count = 0,
			    next_delete_attempt_at = $5,
			    last_delete_result_code = NULL
			WHERE incident_bundle_id = $1
		`,
			string(existing.BundleID),
			objectKey,
			len(content),
			at,
			expiresAt,
		)
		if err != nil {
			return Bundle{}, err
		}
		bundle = Bundle{
			BundleID:        existing.BundleID,
			IncidentID:      incidentID,
			ObjectKey:       objectKey,
			ContentSHA256:   digest,
			CompressedBytes: int64(len(content)),
			CreatedAt:       at,
			ExpiresAt:       expiresAt,
			Created:         true,
		}
	} else {
		bundleID, idErr := creator.nextID()
		if idErr != nil {
			return Bundle{}, idErr
		}
		tag, insertErr := transaction.Exec(ctx, `
			INSERT INTO incident_bundles (
				incident_bundle_id,
				incident_id,
				object_key,
				content_sha256,
				compressed_bytes,
				created_at,
				expires_at,
				next_delete_attempt_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
			ON CONFLICT (incident_id, content_sha256) DO NOTHING
		`,
			string(bundleID),
			string(incidentID),
			objectKey,
			digest[:],
			len(content),
			at,
			expiresAt,
		)
		if insertErr != nil {
			return Bundle{}, insertErr
		}
		if tag.RowsAffected() == 0 {
			existing, err = loadExisting(ctx, transaction, incidentID, digest)
			if err != nil {
				return Bundle{}, err
			}
			if existing == nil {
				return Bundle{}, ErrInvalidBundle
			}
			bundle = existing.Bundle
		} else {
			bundle = Bundle{
				BundleID:        bundleID,
				IncidentID:      incidentID,
				ObjectKey:       objectKey,
				ContentSHA256:   digest,
				CompressedBytes: int64(len(content)),
				CreatedAt:       at,
				ExpiresAt:       expiresAt,
				Created:         true,
			}
		}
	}
	if err = transaction.Commit(ctx); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func loadEvidence(
	ctx context.Context,
	transaction pgx.Tx,
	incidentID metadata.UUID,
) ([]spool.Entry, error) {
	rows, err := transaction.Query(ctx, `
		SELECT
			event_id::text,
			node_id::text,
			boot_session_id::text,
			sequence,
			occurred_at,
			monotonic_offset_ns,
			schema_name,
			schema_version,
			priority,
			payload
		FROM (
			SELECT
				e.event_id,
				e.node_id,
				e.boot_session_id,
				e.sequence,
				e.occurred_at,
				e.monotonic_offset_ns,
				e.schema_name,
				e.schema_version,
				e.priority,
				e.payload
			FROM incident_events ie
			JOIN events e ON e.event_id = ie.event_id
			WHERE ie.incident_id = $1
			ORDER BY e.occurred_at DESC, e.event_id DESC
			LIMIT $2
		) evidence
		ORDER BY occurred_at, event_id
	`, string(incidentID), telemetry.MaxIncidentBundleEvents)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]spool.Entry, 0, telemetry.MaxIncidentBundleEvents)
	for rows.Next() {
		var (
			eventID       string
			nodeID        string
			sessionID     string
			sequence      int64
			occurredAt    time.Time
			monotonic     int64
			schema        event.Schema
			schemaVersion int16
			priority      event.Priority
			payload       json.RawMessage
		)
		if err := rows.Scan(
			&eventID,
			&nodeID,
			&sessionID,
			&sequence,
			&occurredAt,
			&monotonic,
			&schema,
			&schemaVersion,
			&priority,
			&payload,
		); err != nil {
			return nil, err
		}
		if sequence <= 0 || schemaVersion <= 0 {
			return nil, ErrInvalidBundle
		}
		record, err := json.Marshal(struct {
			Schema   event.Schema    `json:"schema"`
			Version  uint16          `json:"version"`
			Priority event.Priority  `json:"priority"`
			Payload  json.RawMessage `json:"payload"`
		}{
			Schema:   schema,
			Version:  uint16(schemaVersion),
			Priority: priority,
			Payload:  payload,
		})
		if err != nil {
			return nil, ErrInvalidBundle
		}
		if _, err := event.Decode(record); err != nil {
			return nil, ErrInvalidBundle
		}
		entries = append(entries, spool.Entry{
			Sequence: uint64(sequence),
			Priority: priority,
			Metadata: metadata.Metadata{
				EventID:        metadata.UUID(eventID),
				NodeID:         metadata.UUID(nodeID),
				SessionID:      metadata.UUID(sessionID),
				Sequence:       uint64(sequence),
				WallClock:      occurredAt.UTC(),
				MonotonicNanos: monotonic,
			},
			Event: record,
			Size:  int64(len(record)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type storedBundle struct {
	Bundle
	deletedAt *time.Time
}

func loadExisting(
	ctx context.Context,
	transaction pgx.Tx,
	incidentID metadata.UUID,
	digest [32]byte,
) (*storedBundle, error) {
	var (
		bundleID  string
		objectKey string
		size      int64
		createdAt time.Time
		expiresAt time.Time
		deletedAt *time.Time
	)
	err := transaction.QueryRow(ctx, `
		SELECT
			incident_bundle_id::text,
			object_key,
			compressed_bytes,
			created_at,
			expires_at,
			deleted_at
		FROM incident_bundles
		WHERE incident_id = $1
		  AND content_sha256 = $2
		FOR UPDATE
	`, string(incidentID), digest[:]).Scan(
		&bundleID,
		&objectKey,
		&size,
		&createdAt,
		&expiresAt,
		&deletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &storedBundle{
		Bundle: Bundle{
			BundleID:        metadata.UUID(bundleID),
			IncidentID:      incidentID,
			ObjectKey:       objectKey,
			ContentSHA256:   digest,
			CompressedBytes: size,
			CreatedAt:       createdAt.UTC(),
			ExpiresAt:       expiresAt.UTC(),
		},
		deletedAt: deletedAt,
	}, nil
}

func (creator *Creator) nextID() (metadata.UUID, error) {
	creator.randomMu.Lock()
	defer creator.randomMu.Unlock()
	return metadata.NewUUID(creator.random)
}

func objectKey(digest [32]byte) string {
	encoded := hex.EncodeToString(digest[:])
	return "incident-bundles/sha256/" + encoded[:2] + "/" + encoded + ".json.gz"
}
