package incidentbundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

const (
	bundleNodeID       = metadata.UUID("11111111-1111-4111-8111-111111111111")
	bundleIncidentID   = metadata.UUID("22222222-2222-4222-8222-222222222222")
	bundleEvidenceID   = metadata.UUID("33333333-3333-4333-8333-333333333333")
	bundleUnlinkedID   = metadata.UUID("44444444-4444-4444-8444-444444444444")
	bundleBatchID      = metadata.UUID("55555555-5555-4555-8555-555555555555")
	firstExpiryWorker  = metadata.UUID("66666666-6666-4666-8666-666666666666")
	secondExpiryWorker = metadata.UUID("77777777-7777-4777-8777-777777777777")
)

func TestPostgresIncidentBundleIsPrivateBoundedAndExpires(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := bundleIntegrationPool(t, ctx, adminDSN)
	maintenance := bundleIntegrationPool(t, ctx, maintenanceDSN)
	resetBundleData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetBundleData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	seedBundleData(t, ctx, admin, now)
	storage := &recordingStorage{}
	creator, err := NewCreator(
		maintenance,
		storage,
		bytes.NewReader(make([]byte, 16)),
	)
	if err != nil {
		t.Fatalf("NewCreator() error = %v", err)
	}
	created, err := creator.Create(ctx, bundleIncidentID, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.Created ||
		created.ExpiresAt.Sub(created.CreatedAt) != Retention ||
		len(storage.puts) != 1 {
		t.Fatalf("created = %+v, puts = %d", created, len(storage.puts))
	}
	object := storage.puts[0]
	if object.Key != created.ObjectKey ||
		object.ContentEncoding != "gzip" ||
		object.ContentType != objectMediaType ||
		!object.ExpiresAt.Equal(created.ExpiresAt) {
		t.Fatalf("private object = %+v", object)
	}
	digest := sha256.Sum256(object.Content)
	if digest != object.ContentSHA256 || digest != created.ContentSHA256 {
		t.Fatalf("content digest mismatch")
	}
	decoded, err := telemetry.DecodeIncidentBundle(object.Content)
	if err != nil {
		t.Fatalf("DecodeIncidentBundle() error = %v", err)
	}
	if decoded.IncidentID != string(bundleIncidentID) ||
		len(decoded.Events) != 1 ||
		decoded.Events[0].Metadata.EventID != bundleEvidenceID {
		t.Fatalf("decoded bundle = %+v", decoded)
	}
	if _, err := event.Decode(decoded.Events[0].Record); err != nil {
		t.Fatalf("bundle event is not an allowlisted typed event: %v", err)
	}

	duplicate, err := creator.Create(ctx, bundleIncidentID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Create(duplicate) error = %v", err)
	}
	if duplicate.Created ||
		duplicate.BundleID != created.BundleID ||
		len(storage.puts) != 1 {
		t.Fatalf("duplicate = %+v, puts = %d", duplicate, len(storage.puts))
	}
	assertBundleRow(t, ctx, admin, created)

	store, err := NewPostgresStore(maintenance)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	beforeExpiry, err := store.ClaimExpired(
		ctx,
		firstExpiryWorker,
		created.ExpiresAt.Add(-time.Nanosecond),
		1,
	)
	if err != nil || len(beforeExpiry) != 0 {
		t.Fatalf("ClaimExpired(before) = %+v, %v", beforeExpiry, err)
	}
	firstClaim, err := store.ClaimExpired(
		ctx,
		firstExpiryWorker,
		created.ExpiresAt,
		1,
	)
	if err != nil || len(firstClaim) != 1 {
		t.Fatalf("ClaimExpired(first) = %+v, %v", firstClaim, err)
	}
	blocked, err := store.ClaimExpired(
		ctx,
		secondExpiryWorker,
		created.ExpiresAt,
		1,
	)
	if err != nil || len(blocked) != 0 {
		t.Fatalf("ClaimExpired(blocked) = %+v, %v", blocked, err)
	}
	if err := store.CompleteDeletion(
		ctx,
		firstExpiryWorker,
		firstClaim[0],
		DeletionUnavailable,
		created.ExpiresAt,
	); err != nil {
		t.Fatalf("CompleteDeletion(unavailable) error = %v", err)
	}
	tooEarly, err := store.ClaimExpired(
		ctx,
		secondExpiryWorker,
		created.ExpiresAt.Add(minRetryDelay-time.Nanosecond),
		1,
	)
	if err != nil || len(tooEarly) != 0 {
		t.Fatalf("ClaimExpired(retry early) = %+v, %v", tooEarly, err)
	}

	retryAt := created.ExpiresAt.Add(minRetryDelay)
	worker, err := NewExpiryWorker(
		store,
		storage,
		secondExpiryWorker,
		func() time.Time { return retryAt },
		1,
	)
	if err != nil {
		t.Fatalf("NewExpiryWorker() error = %v", err)
	}
	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Deleted != 1 ||
		result.Deferred != 0 ||
		len(storage.deletes) != 1 ||
		storage.deletes[0] != created.ObjectKey {
		t.Fatalf("expiry result = %+v, deletes = %+v", result, storage.deletes)
	}
	assertBundleDeleted(t, ctx, admin, created.BundleID, retryAt)

	recreatedAt := retryAt.Add(time.Minute)
	recreated, err := creator.Create(ctx, bundleIncidentID, recreatedAt)
	if err != nil {
		t.Fatalf("Create(after expiry) error = %v", err)
	}
	if !recreated.Created ||
		recreated.BundleID != created.BundleID ||
		len(storage.puts) != 2 ||
		!recreated.CreatedAt.Equal(recreatedAt) ||
		!recreated.ExpiresAt.Equal(recreatedAt.Add(Retention)) {
		t.Fatalf("recreated = %+v, puts = %d", recreated, len(storage.puts))
	}
}

type recordingStorage struct {
	puts    []PrivateObject
	deletes []string
}

func (storage *recordingStorage) PutPrivate(
	_ context.Context,
	object PrivateObject,
) error {
	object.Content = append([]byte(nil), object.Content...)
	storage.puts = append(storage.puts, object)
	return nil
}

func (storage *recordingStorage) DeletePrivate(
	_ context.Context,
	objectKey string,
) error {
	storage.deletes = append(storage.deletes, objectKey)
	return nil
}

func bundleIntegrationPool(
	t *testing.T,
	ctx context.Context,
	dsn string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("PostgreSQL ping error = %v", err)
	}
	return pool
}

func resetBundleData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			incident_bundles,
			incident_events,
			incident_transitions,
			incidents,
			events,
			batches,
			node_public_keys,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset incident bundle data: %v", err)
	}
}

func seedBundleData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO nodes (
			node_id,
			node_name,
			node_kind,
			expected_heartbeat_seconds,
			last_seen_at,
			created_at,
			updated_at
		) VALUES ($1, 'incident-bundle-test', 'mac', 60, $2, $2, $2);

		INSERT INTO node_public_keys (
			public_key_id,
			node_id,
			key_id,
			public_key,
			key_status,
			valid_from
		) VALUES (
			'88888888-8888-4888-8888-888888888888',
			$1,
			'99999999-9999-4999-8999-999999999999',
			$3,
			'active',
			$2
		);

		INSERT INTO batches (
			batch_id,
			request_id,
			node_id,
			signing_key_id,
			protocol_version,
			first_sequence,
			last_sequence,
			event_count,
			compressed_bytes,
			content_sha256,
			signed_at,
			received_at
		) VALUES (
			$4,
			'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
			$1,
			'99999999-9999-4999-8999-999999999999',
			1,
			1,
			2,
			2,
			256,
			$3,
			$2,
			$2
		);

		INSERT INTO events (
			event_id,
			batch_id,
			node_id,
			boot_session_id,
			sequence,
			occurred_at,
			monotonic_offset_ns,
			schema_name,
			schema_version,
			priority,
			payload,
			received_at
		) VALUES
		(
			$5,
			$4,
			$1,
			'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
			1,
			$2,
			1,
			'component.observation',
			1,
			'operational',
			'{
				"component": "tunnel",
				"health": "ready",
				"reason": "probe_succeeded",
				"consecutive_failures": 0
			}'::jsonb,
			$2
		),
		(
			$6,
			$4,
			$1,
			'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
			2,
			$2::timestamptz + INTERVAL '1 second',
			2,
			'runtime.diagnostic',
			1,
			'diagnostic',
			'{
				"component": "runtime",
				"code": "adapter_sampled",
				"count": 1,
				"duration_ms": 1
			}'::jsonb,
			$2
		);

		INSERT INTO incidents (
			incident_id,
			node_id,
			correlation_key,
			category,
			component,
			severity,
			incident_status,
			requires_action,
			generation,
			opened_at,
			last_observed_at,
			created_at,
			updated_at
		) VALUES (
			$7,
			$1,
			'incident-bundle:test',
			'availability',
			'tunnel',
			'warning',
			'open',
			TRUE,
			1,
			$2,
			$2,
			$2,
			$2
		);

		INSERT INTO incident_events (
			incident_id,
			event_id,
			evidence_role,
			linked_at
		) VALUES ($7, $5, 'trigger', $2);
	`,
		pgx.QueryExecModeSimpleProtocol,
		string(bundleNodeID),
		now,
		make([]byte, 32),
		string(bundleBatchID),
		string(bundleEvidenceID),
		string(bundleUnlinkedID),
		string(bundleIncidentID),
	)
	if err != nil {
		t.Fatalf("seed incident bundle data: %v", err)
	}
}

func assertBundleRow(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	want Bundle,
) {
	t.Helper()
	var (
		count       int
		objectKey   string
		digest      []byte
		size        int64
		expiresAt   time.Time
		nextAttempt time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			count(*) OVER (),
			object_key,
			content_sha256,
			compressed_bytes,
			expires_at,
			next_delete_attempt_at
		FROM incident_bundles
		WHERE incident_id = $1
	`, string(want.IncidentID)).Scan(
		&count,
		&objectKey,
		&digest,
		&size,
		&expiresAt,
		&nextAttempt,
	); err != nil {
		t.Fatalf("read incident bundle: %v", err)
	}
	if count != 1 ||
		objectKey != want.ObjectKey ||
		!bytes.Equal(digest, want.ContentSHA256[:]) ||
		size != want.CompressedBytes ||
		!expiresAt.Equal(want.ExpiresAt) ||
		!nextAttempt.Equal(want.ExpiresAt) {
		t.Fatalf(
			"bundle row = count:%d key:%s size:%d expires:%v next:%v",
			count,
			objectKey,
			size,
			expiresAt,
			nextAttempt,
		)
	}
}

func assertBundleDeleted(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	bundleID metadata.UUID,
	deletedAt time.Time,
) {
	t.Helper()
	var (
		actualDeletedAt time.Time
		resultCode      string
		claimOwner      *string
		claimUntil      *time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			deleted_at,
			last_delete_result_code,
			delete_claim_owner::text,
			delete_claim_until
		FROM incident_bundles
		WHERE incident_bundle_id = $1
	`, string(bundleID)).Scan(
		&actualDeletedAt,
		&resultCode,
		&claimOwner,
		&claimUntil,
	); err != nil {
		t.Fatalf("read deleted incident bundle: %v", err)
	}
	if !actualDeletedAt.Equal(deletedAt) ||
		resultCode != "object_deleted" ||
		claimOwner != nil ||
		claimUntil != nil {
		t.Fatalf(
			"deleted bundle = at:%v result:%s owner:%v until:%v",
			actualDeletedAt,
			resultCode,
			claimOwner,
			claimUntil,
		)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if retryDelay(1) != minRetryDelay {
		t.Fatalf("retryDelay(1) = %v", retryDelay(1))
	}
	if retryDelay(1000) != maxRetryDelay {
		t.Fatalf("retryDelay(1000) = %v", retryDelay(1000))
	}
}

const (
	closedUnbundledID = metadata.UUID("a1111111-1111-4111-8111-111111111111")
	closedNoEvidence  = metadata.UUID("a2222222-2222-4222-8222-222222222222")
	closedExpiredID   = metadata.UUID("a3333333-3333-4333-8333-333333333333")
	expiredBundleID   = metadata.UUID("a4444444-4444-4444-8444-444444444444")
)

// TestPostgresOnlyClosedIncidentsNeverBundledArePending fixes the rule the
// bundle pass selects by. Three of the four incidents it seeds are excluded,
// and each exclusion is a failure mode the pass would otherwise run into on
// every interval for as long as the deployment lives:
//
// An open incident is not finished producing evidence.
//
// A closed incident with nothing linked to it makes Create return
// ErrNoIncidentEvidence, and no later pass will find it different.
//
// A closed incident whose bundle was deleted at its recorded expiry must stay
// excluded, because Create revives a deleted row rather than skipping it: the
// pass would put back what expiry just removed, expiry would remove it again,
// and retention would never take effect.
func TestPostgresOnlyClosedIncidentsNeverBundledArePending(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := bundleIntegrationPool(t, ctx, adminDSN)
	maintenance := bundleIntegrationPool(t, ctx, maintenanceDSN)
	resetBundleData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		resetBundleData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	seedBundleData(t, ctx, admin, now)
	seedPendingCandidates(t, ctx, admin, now)

	store, err := NewPostgresStore(maintenance)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	pending, err := store.PendingClosedIncidents(ctx, maxPendingBatch)
	if err != nil {
		t.Fatalf("PendingClosedIncidents() error = %v", err)
	}
	if len(pending) != 1 || pending[0] != closedUnbundledID {
		t.Fatalf(
			"PendingClosedIncidents() = %v, want exactly [%s]",
			pending,
			closedUnbundledID,
		)
	}

	// Bundling the one candidate must empty the selection, or the pass would
	// meet the same incident again on its next interval.
	if _, err := admin.Exec(ctx, `
		INSERT INTO incident_bundles (
			incident_bundle_id,
			incident_id,
			object_key,
			content_sha256,
			compressed_bytes,
			created_at,
			expires_at,
			next_delete_attempt_at
		) VALUES (
			'a5555555-5555-4555-8555-555555555555',
			$1,
			'bundles/just-created',
			$2,
			128,
			$3,
			$3::timestamptz + INTERVAL '30 days',
			$3::timestamptz + INTERVAL '30 days'
		)
	`, pgx.QueryExecModeSimpleProtocol,
		string(closedUnbundledID),
		make([]byte, 32),
		now,
	); err != nil {
		t.Fatalf("record the created bundle: %v", err)
	}
	pending, err = store.PendingClosedIncidents(ctx, maxPendingBatch)
	if err != nil {
		t.Fatalf("PendingClosedIncidents() after bundling error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf(
			"PendingClosedIncidents() after bundling = %v, want none",
			pending,
		)
	}
}

// unreachableDatabase fails the test if a bounds check ever lets a query
// through. A rejected limit must be rejected before anything is asked of the
// database, not after.
type unreachableDatabase struct{ t *testing.T }

func (database unreachableDatabase) BeginTx(
	context.Context,
	pgx.TxOptions,
) (pgx.Tx, error) {
	database.t.Fatal("an out-of-range limit reached the database")
	return nil, nil
}

func TestAPendingBatchIsBounded(t *testing.T) {
	store, err := NewPostgresStore(unreachableDatabase{t: t})
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	for _, limit := range []int{0, -1, maxPendingBatch + 1} {
		if _, err := store.PendingClosedIncidents(
			context.Background(),
			limit,
		); !errors.Is(err, ErrInvalidBundle) {
			t.Fatalf(
				"PendingClosedIncidents(limit=%d) error = %v, want %v",
				limit,
				err,
				ErrInvalidBundle,
			)
		}
	}
}

func seedPendingCandidates(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO incidents (
			incident_id, node_id, correlation_key, category, component,
			severity, incident_status, requires_action, generation,
			opened_at, last_observed_at, resolved_at, created_at, updated_at
		) VALUES
		($2, $1, 'pending:closed-unbundled', 'availability', 'tunnel',
		 'warning', 'resolved', FALSE, 1, $5, $5, $5, $5, $5),
		($3, $1, 'pending:closed-no-evidence', 'availability', 'tunnel',
		 'warning', 'resolved', FALSE, 1, $5, $5, $5, $5, $5),
		($4, $1, 'pending:closed-expired', 'availability', 'tunnel',
		 'warning', 'resolved', FALSE, 1, $5, $5, $5, $5, $5);

		INSERT INTO incident_events (incident_id, event_id, evidence_role, linked_at)
		VALUES ($2, $6, 'trigger', $5),
		       ($4, $6, 'trigger', $5);

		INSERT INTO incident_bundles (
			incident_bundle_id, incident_id, object_key, content_sha256,
			compressed_bytes, created_at, expires_at, deleted_at,
			next_delete_attempt_at
		) VALUES (
			$7, $4, 'bundles/already-expired', $8, 256,
			$5::timestamptz - INTERVAL '60 days',
			$5::timestamptz - INTERVAL '30 days',
			$5::timestamptz - INTERVAL '30 days',
			$5::timestamptz - INTERVAL '30 days'
		);
	`,
		pgx.QueryExecModeSimpleProtocol,
		string(bundleNodeID),
		string(closedUnbundledID),
		string(closedNoEvidence),
		string(closedExpiredID),
		now,
		string(bundleEvidenceID),
		string(expiredBundleID),
		make([]byte, 32),
	)
	if err != nil {
		t.Fatalf("seed pending candidates: %v", err)
	}
}
