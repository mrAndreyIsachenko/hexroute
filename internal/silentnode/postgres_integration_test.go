package silentnode

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	sleepStartEventID  = metadata.UUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	sleepEndEventID    = metadata.UUID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	unmatchedWakeEvent = metadata.UUID("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
)

func TestPostgresSleepProjectionSuppressesOnlyExplicitSleep(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := silentIntegrationPool(t, ctx, adminDSN)
	maintenance := silentIntegrationPool(t, ctx, maintenanceDSN)
	resetSilentIntegrationData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetSilentIntegrationData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.July, 24, 23, 0, 0, 0, time.UTC)
	seedSleepEvents(t, ctx, admin, now)
	store, err := NewPostgresStore(maintenance)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	policy := Policy{
		MissedHeartbeats: 3,
		MinimumGrace:     time.Minute,
		FutureTolerance:  15 * time.Second,
	}

	if err := store.ProjectSleepEvent(ctx, sleepStartEventID); err != nil {
		t.Fatalf("ProjectSleepEvent(start) error = %v", err)
	}
	if err := store.ProjectSleepEvent(ctx, sleepStartEventID); err != nil {
		t.Fatalf("ProjectSleepEvent(duplicate start) error = %v", err)
	}
	decisions, err := store.Decisions(ctx, policy, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Decisions(sleeping) error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].State != StateSleeping {
		t.Fatalf("sleeping decisions = %+v", decisions)
	}
	if decisions[0].SleepEventID != sleepStartEventID {
		t.Fatalf("sleep evidence = %q, want %q", decisions[0].SleepEventID, sleepStartEventID)
	}

	projected, err := store.ProjectPendingSleepEvents(ctx, 10)
	if err != nil || projected != 1 {
		t.Fatalf("ProjectPendingSleepEvents() = %d, %v", projected, err)
	}
	projected, err = store.ProjectPendingSleepEvents(ctx, 10)
	if err != nil || projected != 0 {
		t.Fatalf("ProjectPendingSleepEvents(repeat) = %d, %v", projected, err)
	}
	if err := store.ProjectSleepEvent(ctx, sleepEndEventID); err != nil {
		t.Fatalf("ProjectSleepEvent(duplicate end) error = %v", err)
	}
	_, err = admin.Exec(ctx, `
		UPDATE nodes
		SET last_seen_at = $2, updated_at = $2
		WHERE node_id = $1
	`, string(silentNodeID), now)
	if err != nil {
		t.Fatalf("update wake last_seen: %v", err)
	}
	decisions, err = store.Decisions(ctx, policy, now)
	if err != nil {
		t.Fatalf("Decisions(awake) error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].State != StateHealthy {
		t.Fatalf("awake decisions = %+v", decisions)
	}
	if decisions[0].ReferenceEventID != sleepEndEventID {
		t.Fatalf("recovery evidence = %q, want %q", decisions[0].ReferenceEventID, sleepEndEventID)
	}

	decisions, err = store.Decisions(ctx, policy, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("Decisions(silent) error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].State != StateSilent {
		t.Fatalf("silent decisions = %+v", decisions)
	}

	var (
		count   int
		started time.Time
		ended   *time.Time
	)
	err = admin.QueryRow(ctx, `
		SELECT count(*), min(started_at), max(ended_at)
		FROM sleep_intervals
		WHERE node_id = $1
	`, string(silentNodeID)).Scan(&count, &started, &ended)
	if err != nil {
		t.Fatalf("query sleep interval: %v", err)
	}
	if count != 1 ||
		!started.Equal(now.Add(-5*time.Minute)) ||
		ended == nil ||
		!ended.Equal(now) {
		t.Fatalf("sleep interval count=%d start=%v end=%v", count, started, ended)
	}

	insertSleepEvent(
		t,
		ctx,
		admin,
		unmatchedWakeEvent,
		3,
		now.Add(time.Minute),
		`{"phase":"ended","reason":"full_wake"}`,
	)
	projected, err = store.ProjectPendingSleepEvents(ctx, 10)
	if err != nil || projected != 1 {
		t.Fatalf("ProjectPendingSleepEvents(unmatched wake) = %d, %v", projected, err)
	}
	decisions, err = store.Decisions(ctx, policy, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("Decisions(after unmatched wake) error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].State != StateSilent {
		t.Fatalf("unmatched wake suppressed silence: %+v", decisions)
	}
	var zeroLengthCount int
	if err := admin.QueryRow(ctx, `
		SELECT count(*)
		FROM sleep_intervals
		WHERE node_id = $1
		  AND start_event_id IS NULL
		  AND end_event_id = $2
		  AND started_at = ended_at
	`, string(silentNodeID), string(unmatchedWakeEvent)).Scan(
		&zeroLengthCount,
	); err != nil {
		t.Fatalf("query unmatched wake evidence: %v", err)
	}
	if zeroLengthCount != 1 {
		t.Fatalf("unmatched wake evidence count = %d, want 1", zeroLengthCount)
	}
}

func silentIntegrationPool(
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

func resetSilentIntegrationData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			sleep_intervals,
			events,
			batches,
			node_public_keys,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset silent-node data: %v", err)
	}
}

func seedSleepEvents(
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
		) VALUES ($1, 'sleep-test', 'mac', 60, $2, $3, $3)
	`, string(silentNodeID), now.Add(-10*time.Minute), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("insert silent-node fixture: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO node_public_keys (
			public_key_id,
			node_id,
			key_id,
			public_key,
			key_status,
			valid_from
		) VALUES (
			'22222222-2222-4222-8222-222222222222',
			$1,
			'33333333-3333-4333-8333-333333333333',
			$2,
			'active',
			$3
		)
	`, string(silentNodeID), make([]byte, 32), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("insert sleep signing key fixture: %v", err)
	}
	_, err = admin.Exec(ctx, `
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
			'44444444-4444-4444-8444-444444444444',
			'55555555-5555-4555-8555-555555555555',
			$1,
			'33333333-3333-4333-8333-333333333333',
			1,
			1,
			2,
			2,
			128,
			$2,
			$3,
			$3
		)
	`, string(silentNodeID), make([]byte, 32), now)
	if err != nil {
		t.Fatalf("insert sleep batch fixture: %v", err)
	}
	insertSleepEvent(
		t,
		ctx,
		admin,
		sleepStartEventID,
		1,
		now.Add(-5*time.Minute),
		`{"phase":"started","reason":"lid_closed"}`,
	)
	insertSleepEvent(
		t,
		ctx,
		admin,
		sleepEndEventID,
		2,
		now,
		`{"phase":"ended","reason":"full_wake"}`,
	)
}

func insertSleepEvent(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	eventID metadata.UUID,
	sequence int64,
	occurredAt time.Time,
	payload string,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
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
		) VALUES (
			$1,
			'44444444-4444-4444-8444-444444444444',
			$2,
			'66666666-6666-4666-8666-666666666666',
			$3,
			$4,
			$3,
			'node.sleep',
			1,
			'critical',
			$5::jsonb,
			$4
		)
	`, string(eventID), string(silentNodeID), sequence, occurredAt, payload)
	if err != nil {
		t.Fatalf("insert sleep event fixture: %v", err)
	}
}
