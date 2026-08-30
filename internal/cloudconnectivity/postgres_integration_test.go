package cloudconnectivity

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	projectionNodeID  = metadata.UUID("66666666-6666-4666-8666-666666666666")
	projectionKeyID   = metadata.UUID("77777777-7777-4777-8777-777777777777")
	projectionBatchID = metadata.UUID("88888888-8888-4888-8888-888888888888")
	projectionSession = metadata.UUID("99999999-9999-4999-8999-999999999999")
)

func projectionEventID(index int) metadata.UUID {
	return metadata.UUID("aaaaaaaa-0000-4000-8000-00000000000" + string(rune('0'+index)))
}

func integrationPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func resetProjectionData(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		`DELETE FROM connectivity_snapshot_proposal_classes`,
		`DELETE FROM connectivity_snapshot_components`,
		`DELETE FROM connectivity_snapshots`,
		`DELETE FROM events WHERE node_id = $1`,
		`DELETE FROM node_sequence_cursors WHERE node_id = $1`,
		`DELETE FROM batches WHERE node_id = $1`,
		`DELETE FROM node_public_keys WHERE node_id = $1`,
		`DELETE FROM nodes WHERE node_id = $1`,
	} {
		var err error
		if len(statement) > 0 && statement[len(statement)-1] == '1' {
			_, err = admin.Exec(ctx, statement, string(projectionNodeID))
		} else {
			_, err = admin.Exec(ctx, statement)
		}
		if err != nil {
			t.Fatalf("reset %q: %v", statement, err)
		}
	}
}

func seedProjectionNode(t *testing.T, ctx context.Context, admin *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := admin.Exec(ctx, `
		INSERT INTO nodes (
			node_id, node_name, node_kind, expected_heartbeat_seconds,
			last_seen_at, created_at, updated_at
		) VALUES ($1, 'projection-test', 'mac', 60, $2, $3, $3)
	`, string(projectionNodeID), now, now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO node_public_keys (
			public_key_id, node_id, key_id, public_key, key_status, valid_from
		) VALUES (
			'12121212-1212-4212-8212-121212121212', $1, $2, $3, 'active', $4
		)
	`, string(projectionNodeID), string(projectionKeyID), make([]byte, 32),
		now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO batches (
			batch_id, request_id, node_id, signing_key_id, protocol_version,
			first_sequence, last_sequence, event_count, compressed_bytes,
			content_sha256, signed_at, received_at
		) VALUES (
			$1, '13131313-1313-4313-8313-131313131313', $2, $3, 1,
			1, 9, 9, 256, $4, $5, $5
		)
	`, string(projectionBatchID), string(projectionNodeID), string(projectionKeyID),
		make([]byte, 32), now); err != nil {
		t.Fatalf("seed batch: %v", err)
	}
}

func insertProjectionEvent(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	eventID metadata.UUID,
	sequence int64,
	occurredAt time.Time,
	projection event.ConnectivityProjection,
) {
	t.Helper()
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO events (
			event_id, batch_id, node_id, boot_session_id, sequence,
			occurred_at, monotonic_offset_ns, schema_name, schema_version,
			priority, payload, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, 'operational', $9::jsonb, $6)
	`,
		string(eventID),
		string(projectionBatchID),
		string(projectionNodeID),
		string(projectionSession),
		sequence,
		occurredAt,
		sequence*1_000_000,
		string(event.SchemaConnectivityProjection),
		string(payload),
	); err != nil {
		t.Fatalf("insert projection event: %v", err)
	}
}

func sampleProjection(generation uint64, aggregate string) event.ConnectivityProjection {
	return event.ConnectivityProjection{
		SnapshotGeneration:  generation,
		ReducerVersion:      1,
		BundleGeneration:    7,
		RootGeneration:      3,
		UserGeneration:      2,
		Aggregate:           aggregate,
		Authorization:       "authorized",
		AuthorizationReason: "none",
		Components: []event.ProjectedComponent{
			{Component: "dns", State: "unknown", Freshness: "never_observed", Reason: "no_observation"},
			{Component: "relays", State: "ready", Freshness: "fresh", Reason: "none"},
		},
		OpenGaps:         1,
		GapOverflow:      true,
		SourceConflicts:  1,
		AwaitingBaseline: 1,
		ConflictOverflow: true,
		ProposalClasses:  []event.ProjectedProposalClass{{Class: "restore", Count: 2}},
	}
}

func TestPostgresProjectionIsOrderedIdempotentAndStaleSafe(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin := integrationPool(t, ctx, adminDSN)
	maintenance := integrationPool(t, ctx, maintenanceDSN)
	resetProjectionData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		resetProjectionData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	seedProjectionNode(t, ctx, admin, now)
	store, err := NewPostgresStore(maintenance)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}

	insertProjectionEvent(t, ctx, admin, projectionEventID(1), 1,
		now.Add(-10*time.Minute), sampleProjection(4, "degraded"))
	applied, err := store.ProjectPending(ctx, 10)
	if err != nil || applied != 1 {
		t.Fatalf("ProjectPending = %d, %v", applied, err)
	}

	// A replayed pass must change nothing: the node's stored position already
	// covers the event.
	applied, err = store.ProjectPending(ctx, 10)
	if err != nil || applied != 0 {
		t.Fatalf("ProjectPending(replay) = %d, %v", applied, err)
	}

	stored, err := store.Load(ctx, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("Load = %+v, %v", stored, err)
	}
	if stored[0].Aggregate != "degraded" || stored[0].SnapshotGeneration != 4 {
		t.Fatalf("stored projection = %+v", stored[0])
	}
	if !stored[0].GapOverflow || !stored[0].ConflictOverflow ||
		stored[0].AwaitingBaseline != 1 {
		t.Fatalf("integrity signals were lost on the way in: %+v", stored[0])
	}
	if len(stored[0].Components) != 2 || len(stored[0].ProposalClasses) != 1 {
		t.Fatalf("component and proposal detail = %+v", stored[0])
	}

	// A newer projection replaces it.
	insertProjectionEvent(t, ctx, admin, projectionEventID(2), 2,
		now.Add(-5*time.Minute), sampleProjection(5, "ready"))
	if applied, err = store.ProjectPending(ctx, 10); err != nil || applied != 1 {
		t.Fatalf("ProjectPending(newer) = %d, %v", applied, err)
	}
	stored, err = store.Load(ctx, 10)
	if err != nil || len(stored) != 1 || stored[0].Aggregate != "ready" {
		t.Fatalf("newer projection did not replace the older one: %+v, %v", stored, err)
	}

	// A projection describing an earlier host position must not overwrite
	// what is already stored. This is the one way stale cloud data could
	// mislead an operator about a host they cannot reach, so both layers that
	// prevent it are exercised: the pass never selects it, and the write is
	// guarded even if a concurrent worker offered it anyway.
	insertProjectionEvent(t, ctx, admin, projectionEventID(3), 3,
		now.Add(-20*time.Minute), sampleProjection(2, "failed"))
	if applied, err = store.ProjectPending(ctx, 10); err != nil || applied != 0 {
		t.Fatalf("ProjectPending(late) = %d, %v", applied, err)
	}
	late := pending{
		eventID:    projectionEventID(3),
		nodeID:     projectionNodeID,
		sessionID:  projectionSession,
		sequence:   3,
		occurredAt: now.Add(-20 * time.Minute),
		version:    1,
		priority:   event.PriorityOperational,
	}
	changed, err := store.fold(ctx, late, sampleProjection(2, "failed"))
	if err != nil {
		t.Fatalf("fold(late) error = %v", err)
	}
	if changed {
		t.Fatal("the write guard let a stale projection through")
	}
	stored, err = store.Load(ctx, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("Load after late arrival = %+v, %v", stored, err)
	}
	if stored[0].Aggregate != "ready" || stored[0].SnapshotGeneration != 5 {
		t.Fatalf("a late projection overwrote a newer one: %+v", stored[0])
	}

	// A host that could not recover its lineage restarts its snapshot
	// generation. The later position still wins, and the reset is recorded
	// rather than smoothed over.
	insertProjectionEvent(t, ctx, admin, projectionEventID(4), 4,
		now, sampleProjection(1, "unknown"))
	if applied, err = store.ProjectPending(ctx, 10); err != nil || applied != 1 {
		t.Fatalf("ProjectPending(reset) = %d, %v", applied, err)
	}
	stored, err = store.Load(ctx, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("Load after reset = %+v, %v", stored, err)
	}
	if stored[0].SnapshotGeneration != 1 || !stored[0].LineageReset {
		t.Fatalf("a lineage reset was not recorded: %+v", stored[0])
	}
}

// The schema refuses anything the projection alphabet cannot express, so a
// regressed encoder cannot store an address, a path or a digest here.
func TestPostgresProjectionSchemaRefusesUnboundedTokens(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	if adminDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := integrationPool(t, ctx, adminDSN)
	resetProjectionData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		resetProjectionData(t, cleanupCtx, admin)
	})
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	seedProjectionNode(t, ctx, admin, now)
	if _, err := admin.Exec(ctx, `
		INSERT INTO connectivity_snapshots (
			node_id, boot_session_id, node_sequence, observed_at,
			snapshot_generation, reducer_version,
			bundle_generation, root_generation, user_generation,
			aggregate_state, authorization_state, authorization_reason,
			open_gaps, gap_overflow, source_conflicts,
			awaiting_baseline, conflict_overflow, updated_at
		) VALUES (
			$1, $2, 1, $3, 1, 1, 1, 1, 1,
			'ready', 'authorized', 'none', 0, FALSE, 0, 0, FALSE, $3
		)
	`, string(projectionNodeID), string(projectionSession), now); err != nil {
		t.Fatalf("seed snapshot row: %v", err)
	}

	for name, value := range map[string]string{
		"an address":  "198.51.100.7",
		"a path":      "/var/run/hexroute.sock",
		"an endpoint": "relay.example.test:443",
		"an upper":    "Relays",
	} {
		_, err := admin.Exec(ctx, `
			INSERT INTO connectivity_snapshot_components (
				node_id, component, component_state, freshness, diff_reason
			) VALUES ($1, $2, 'ready', 'fresh', 'none')
		`, string(projectionNodeID), value)
		if err == nil {
			t.Errorf("the schema stored %s (%q)", name, value)
		}
	}
}
