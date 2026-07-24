package retention

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const retentionNodeID = metadata.UUID("11111111-1111-4111-8111-111111111111")

type retentionBatchFixture struct {
	batchID   metadata.UUID
	requestID metadata.UUID
	sequence  int
	received  time.Time
}

func TestPostgresRetentionIsBoundedAndPreservesDurableRecords(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := retentionIntegrationPool(t, ctx, adminDSN)
	maintenance := retentionIntegrationPool(t, ctx, maintenanceDSN)
	resetRetentionData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetRetentionData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	seedRetentionData(t, ctx, admin, now)
	worker, err := NewWorker(maintenance, 1)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	var deleted int64
	for iteration := 0; iteration < 16; iteration++ {
		result, runErr := worker.RunOnce(ctx, now)
		if runErr != nil {
			t.Fatalf("RunOnce(%d) error = %v", iteration, runErr)
		}
		for name, count := range map[string]int64{
			"detail_events":         result.DetailEvents,
			"transition_events":     result.TransitionEvents,
			"security_audit":        result.SecurityAudit,
			"sleep_intervals":       result.SleepIntervals,
			"resolved_gaps":         result.ResolvedGaps,
			"incident_alert_outbox": result.IncidentAlertOutbox,
			"incident_transitions":  result.IncidentTransitions,
			"terminal_alerts":       result.TerminalAlerts,
			"orphan_batches":        result.OrphanBatches,
		} {
			if count > 1 {
				t.Fatalf("%s deleted %d rows with batch size 1", name, count)
			}
		}
		deleted += result.Total()
		if result.Total() == 0 {
			break
		}
	}
	if deleted != 11 {
		t.Fatalf("deleted rows = %d, want 11", deleted)
	}
	assertRetentionState(t, ctx, admin)
}

func retentionIntegrationPool(
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

func resetRetentionData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			alert_deliveries,
			incident_alert_outbox,
			slo_incident_links,
			slo_aggregates,
			deployments,
			config_versions,
			incident_events,
			incident_transitions,
			incidents,
			latest_component_states,
			sleep_intervals,
			sequence_gaps,
			security_audit_records,
			events,
			batches,
			node_public_keys,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset retention data: %v", err)
	}
}

func seedRetentionData(
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
		) VALUES ($1, 'retention-test', 'mac', 60, $2, $3, $2)
	`, string(retentionNodeID), now, now.Add(-200*24*time.Hour))
	if err != nil {
		t.Fatalf("insert retention node: %v", err)
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
	`, string(retentionNodeID), make([]byte, 32), now.Add(-200*24*time.Hour))
	if err != nil {
		t.Fatalf("insert retention key: %v", err)
	}

	oldDetail := retentionBatchFixture{
		batchID:   "10101010-1010-4010-8010-101010101010",
		requestID: "20101010-1010-4010-8010-101010101010",
		sequence:  1,
		received:  now.Add(-40 * 24 * time.Hour),
	}
	recentDetail := retentionBatchFixture{
		batchID:   "10202020-2020-4020-8020-202020202020",
		requestID: "20202020-2020-4020-8020-202020202020",
		sequence:  2,
		received:  now.Add(-10 * 24 * time.Hour),
	}
	recentTransition := retentionBatchFixture{
		batchID:   "10303030-3030-4030-8030-303030303030",
		requestID: "20303030-3030-4030-8030-303030303030",
		sequence:  3,
		received:  now.Add(-100 * 24 * time.Hour),
	}
	oldTransition := retentionBatchFixture{
		batchID:   "10404040-4040-4040-8040-404040404040",
		requestID: "20404040-4040-4040-8040-404040404040",
		sequence:  4,
		received:  now.Add(-190 * 24 * time.Hour),
	}
	resolvedGapBatch := retentionBatchFixture{
		batchID:   "10505050-5050-4050-8050-505050505050",
		requestID: "20505050-5050-4050-8050-505050505050",
		sequence:  5,
		received:  now.Add(-40 * 24 * time.Hour),
	}
	openGapBatch := retentionBatchFixture{
		batchID:   "10606060-6060-4060-8060-606060606060",
		requestID: "20606060-6060-4060-8060-606060606060",
		sequence:  6,
		received:  now.Add(-40 * 24 * time.Hour),
	}
	for _, fixture := range []retentionBatchFixture{
		oldDetail,
		recentDetail,
		recentTransition,
		oldTransition,
		resolvedGapBatch,
		openGapBatch,
	} {
		insertRetentionBatch(t, ctx, admin, fixture)
	}

	oldDetailEvent := metadata.UUID("30101010-1010-4010-8010-101010101010")
	insertRetentionEvent(
		t,
		ctx,
		admin,
		oldDetail,
		oldDetailEvent,
		"component.observation",
		"operational",
	)
	insertRetentionEvent(
		t,
		ctx,
		admin,
		recentDetail,
		"30202020-2020-4020-8020-202020202020",
		"runtime.diagnostic",
		"diagnostic",
	)
	insertRetentionEvent(
		t,
		ctx,
		admin,
		recentTransition,
		"30303030-3030-4030-8030-303030303030",
		"state.transition",
		"critical",
	)
	insertRetentionEvent(
		t,
		ctx,
		admin,
		oldTransition,
		"30404040-4040-4040-8040-404040404040",
		"recovery.action",
		"critical",
	)

	seedRetentionReferences(
		t,
		ctx,
		admin,
		now,
		oldDetailEvent,
		resolvedGapBatch,
		openGapBatch,
	)
}

func insertRetentionBatch(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	fixture retentionBatchFixture,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
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
		) VALUES ($1, $2, $3, $4, 1, $5, $5, 1, 64, $6, $7, $7)
	`,
		string(fixture.batchID),
		string(fixture.requestID),
		string(retentionNodeID),
		"33333333-3333-4333-8333-333333333333",
		fixture.sequence,
		make([]byte, 32),
		fixture.received,
	)
	if err != nil {
		t.Fatalf("insert batch %s: %v", fixture.batchID, err)
	}
}

func insertRetentionEvent(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	fixture retentionBatchFixture,
	eventID metadata.UUID,
	schema string,
	priority string,
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
			$2,
			$3,
			'40404040-4040-4040-8040-404040404040',
			$4,
			$5,
			$4,
			$6,
			1,
			$7,
			'{}'::jsonb,
			$5
		)
	`,
		string(eventID),
		string(fixture.batchID),
		string(retentionNodeID),
		fixture.sequence,
		fixture.received,
		schema,
		priority,
	)
	if err != nil {
		t.Fatalf("insert event %s: %v", eventID, err)
	}
}

func seedRetentionReferences(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	now time.Time,
	oldDetailEvent metadata.UUID,
	resolvedGapBatch retentionBatchFixture,
	openGapBatch retentionBatchFixture,
) {
	t.Helper()
	old40 := now.Add(-40 * 24 * time.Hour)
	old100 := now.Add(-100 * 24 * time.Hour)
	old190 := now.Add(-190 * 24 * time.Hour)
	_, err := admin.Exec(ctx, `
		INSERT INTO latest_component_states (
			node_id,
			component,
			control_state,
			health,
			reason_code,
			generation,
			observed_at,
			event_id
		) VALUES ($1, 'runtime', 'HEALTHY', 'ready', 'probe_succeeded', 1, $2, $3)
	`,
		string(retentionNodeID),
		old40,
		string(oldDetailEvent),
	)
	if err != nil {
		t.Fatalf("insert latest state: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO sleep_intervals (
			sleep_interval_id,
			node_id,
			boot_session_id,
			started_at,
			ended_at,
			start_event_id,
			end_event_id,
			reason_code
		) VALUES (
			'50505050-5050-4050-8050-505050505050',
			$1,
			'40404040-4040-4040-8040-404040404040',
			$2,
			$3,
			$4,
			$4,
			'lid_closed'
		), (
			'51515151-5151-4151-8151-515151515151',
			$1,
			'41414141-4141-4141-8141-414141414141',
			$2,
			NULL,
			NULL,
			NULL,
			'system_sleep'
		)
	`, string(retentionNodeID), old40.Add(-time.Hour), old40, string(oldDetailEvent))
	if err != nil {
		t.Fatalf("insert sleep intervals: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO sequence_gaps (
			sequence_gap_id,
			node_id,
			boot_session_id,
			first_sequence,
			last_sequence,
			detected_batch_id,
			detected_at,
			resolved_at
		) VALUES (
			'60606060-6060-4060-8060-606060606060',
			$1,
			'42424242-4242-4242-8242-424242424242',
			10,
			11,
			$2,
			$3,
			$3
		), (
			'61616161-6161-4161-8161-616161616161',
			$1,
			'43434343-4343-4343-8343-434343434343',
			20,
			21,
			$4,
			$3,
			NULL
		)
	`,
		string(retentionNodeID),
		string(resolvedGapBatch.batchID),
		old40,
		string(openGapBatch.batchID),
	)
	if err != nil {
		t.Fatalf("insert sequence gaps: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO security_audit_records (
			audit_record_id,
			node_id,
			category,
			reason_code,
			occurred_at
		) VALUES (
			'70707070-7070-4070-8070-707070707070',
			$1,
			'schema',
			'expired',
			$2
		), (
			'71717171-7171-4171-8171-717171717171',
			$1,
			'schema',
			'recent',
			$3
		)
	`, string(retentionNodeID), old40, now.Add(-10*24*time.Hour))
	if err != nil {
		t.Fatalf("insert audit records: %v", err)
	}
	seedDurableRetentionRecords(t, ctx, admin, oldDetailEvent, old100, old190)
}

func seedDurableRetentionRecords(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	oldDetailEvent metadata.UUID,
	old100 time.Time,
	old190 time.Time,
) {
	t.Helper()
	const incidentID = "80808080-8080-4080-8080-808080808080"
	_, err := admin.Exec(ctx, `
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
			resolved_at,
			created_at,
			updated_at
		) VALUES (
			$1,
			$2,
			'retention-incident',
			'availability',
			'runtime',
			'warning',
			'resolved',
			false,
			2,
			$3,
			$4,
			$4,
			$3,
			$4
		)
	`, incidentID, string(retentionNodeID), old190, old100)
	if err != nil {
		t.Fatalf("insert durable incident: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO incident_events (incident_id, event_id, evidence_role)
		VALUES ($1, $2, 'trigger')
	`, incidentID, string(oldDetailEvent))
	if err != nil {
		t.Fatalf("insert incident evidence: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO incident_transitions (
			incident_transition_id,
			incident_id,
			generation,
			from_status,
			to_status,
			reason_code,
			transitioned_at
		) VALUES (
			'81818181-8181-4181-8181-818181818181',
			$1,
			1,
			'new',
			'open',
			'condition_detected',
			$2
		), (
			'82828282-8282-4282-8282-828282828282',
			$1,
			2,
			'open',
			'resolved',
			'condition_cleared',
			$3
		)
	`, incidentID, old190, old100)
	if err != nil {
		t.Fatalf("insert incident transitions: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO incident_alert_outbox (
			incident_id,
			incident_generation,
			node_id,
			snapshot_status,
			snapshot_severity,
			snapshot_category,
			snapshot_component,
			snapshot_requires_action,
			snapshot_transitioned_at,
			created_at,
			processed_at,
			last_result_code
		) VALUES (
			$1, 1, $2, 'open', 'warning', 'availability', 'runtime',
			false, $3, $3, $3, 'queued'
		), (
			$1, 2, $2, 'resolved', 'warning', 'availability', 'runtime',
			false, $4, $4, NULL, NULL
		)
	`, incidentID, string(retentionNodeID), old190, old100)
	if err != nil {
		t.Fatalf("insert retention outbox: %v", err)
	}
	seedRetentionAlerts(t, ctx, admin, incidentID, old100, old190)
	seedRetentionDeploymentAndSLO(t, ctx, admin, incidentID, old190)
}

func seedRetentionAlerts(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	incidentID string,
	old100 time.Time,
	old190 time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO alert_deliveries (
			alert_delivery_id,
			incident_id,
			incident_generation,
			channel,
			delivery_status,
			actionable,
			attempt_count,
			next_attempt_at,
			delivered_at,
			last_result_code,
			created_at,
			updated_at,
			snapshot_status,
			snapshot_severity,
			snapshot_category,
			snapshot_component,
			snapshot_transitioned_at
		) VALUES (
			'83838383-8383-4383-8383-838383838383',
			$1, 1, 'telegram', 'delivered', true, 1, NULL, $2,
			'telegram_ok', $2, $2, 'open', 'warning', 'availability', 'runtime', $2
		), (
			'84848484-8484-4484-8484-848484848484',
			$1, 2, 'telegram', 'pending', false, 0, $2, NULL,
			NULL, $2, $2, 'resolved', 'warning', 'availability', 'runtime', $3
		), (
			'85858585-8585-4585-8585-858585858585',
			$1, 2, 'morning_digest', 'suppressed', false, 0, NULL, NULL,
			'night_suppressed', $3, $3,
			'resolved', 'warning', 'availability', 'runtime', $3
		)
	`, incidentID, old190, old100)
	if err != nil {
		t.Fatalf("insert retention alerts: %v", err)
	}
}

func seedRetentionDeploymentAndSLO(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	incidentID string,
	old190 time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO config_versions (
			config_version_id,
			target_kind,
			target_key,
			schema_version,
			version_label,
			content_sha256,
			signing_key_id,
			lifecycle_status,
			created_at,
			activated_at,
			proven_at
		) VALUES (
			'86868686-8686-4686-8686-868686868686',
			'global',
			'retention',
			1,
			'v1',
			$1,
			'synthetic-key',
			'proven',
			$2,
			$2,
			$2
		)
	`, make([]byte, 32), old190)
	if err != nil {
		t.Fatalf("insert retained config: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO deployments (
			deployment_id,
			node_id,
			target_key,
			application_version,
			artifact_sha256,
			config_version_id,
			deployment_status,
			started_at,
			completed_at,
			created_at
		) VALUES (
			'87878787-8787-4787-8787-878787878787',
			$1,
			'retention',
			'v1',
			$2,
			'86868686-8686-4686-8686-868686868686',
			'healthy',
			$3,
			$3,
			$3
		)
	`, string(retentionNodeID), make([]byte, 32), old190)
	if err != nil {
		t.Fatalf("insert retained deployment: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO slo_aggregates (
			slo_aggregate_id,
			granularity,
			target_key,
			node_id,
			service,
			objective,
			window_start,
			window_end,
			eligible_milliseconds,
			good_milliseconds,
			bad_milliseconds,
			excluded_milliseconds,
			qualifying_count,
			total_count,
			computed_at
		) VALUES (
			'88888888-8888-4888-8888-888888888888',
			'day',
			'retention',
			$1,
			'twilight_transport',
			'availability',
			$2,
			$3,
			86400000,
			86000000,
			400000,
			0,
			1,
			1,
			$3
		)
	`, string(retentionNodeID), old190, old190.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("insert retained SLO: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO slo_incident_links (
			slo_aggregate_id,
			incident_id,
			linkage_role
		) VALUES (
			'88888888-8888-4888-8888-888888888888',
			$1,
			'failure'
		)
	`, incidentID)
	if err != nil {
		t.Fatalf("insert retained SLO link: %v", err)
	}
}

func assertRetentionState(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	expected := map[string]int{
		"events":                 2,
		"batches":                3,
		"security_audit_records": 1,
		"sleep_intervals":        1,
		"sequence_gaps":          1,
		"incident_events":        0,
		"incident_alert_outbox":  1,
		"incident_transitions":   1,
		"alert_deliveries":       2,
		"incidents":              1,
		"deployments":            1,
		"config_versions":        1,
		"slo_aggregates":         1,
		"slo_incident_links":     1,
	}
	for table, want := range expected {
		var count int
		if err := admin.QueryRow(
			ctx,
			"SELECT count(*) FROM "+table,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", table, count, want)
		}
	}
	var latestEventID *string
	if err := admin.QueryRow(ctx, `
		SELECT event_id::text
		FROM latest_component_states
		WHERE node_id = $1 AND component = 'runtime'
	`, string(retentionNodeID)).Scan(&latestEventID); err != nil {
		t.Fatalf("read retained latest state: %v", err)
	}
	if latestEventID != nil {
		t.Fatalf("latest state retained deleted event ID %q", *latestEventID)
	}
	var pendingAlerts int
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM alert_deliveries
		WHERE delivery_status = 'pending'
	`).Scan(&pendingAlerts); err != nil {
		t.Fatalf("count pending alerts: %v", err)
	}
	if pendingAlerts != 1 {
		t.Fatalf("pending alert count = %d, want 1", pendingAlerts)
	}
}
