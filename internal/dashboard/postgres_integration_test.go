package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresDashboardLoadsBoundedReadOnlySnapshot(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	dashboardDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_DASHBOARD_DSN")
	if adminDSN == "" || dashboardDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := dashboardIntegrationPool(t, ctx, adminDSN)
	dashboard := dashboardIntegrationPool(t, ctx, dashboardDSN)
	resetDashboardData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetDashboardData(t, cleanupCtx, admin)
	})
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	seedDashboardData(t, ctx, admin, now)
	store, err := NewPostgresStore(dashboard, time.Minute)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	snapshot, err := store.Load(ctx, now)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Nodes) != 1 ||
		snapshot.Nodes[0].Name != "mac-primary" ||
		snapshot.Nodes[0].Stale ||
		len(snapshot.Nodes[0].Components) != 1 ||
		len(snapshot.Incidents) != 1 ||
		len(snapshot.Workers) != 1 ||
		snapshot.Workers[0].Stale ||
		len(snapshot.SLOs) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func dashboardIntegrationPool(
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

func resetDashboardData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	if _, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			slo_incident_links,
			slo_aggregates,
			deployments,
			config_versions,
			incident_events,
			incident_transitions,
			incidents,
			latest_component_states,
			worker_heartbeats,
			nodes
		CASCADE
	`); err != nil {
		t.Fatalf("reset dashboard data: %v", err)
	}
}

func seedDashboardData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	nodeID := "11111111-1111-4111-8111-111111111111"
	if _, err := admin.Exec(ctx, `
		INSERT INTO nodes (
			node_id,
			node_name,
			node_kind,
			expected_heartbeat_seconds,
			last_seen_at,
			created_at,
			updated_at
		) VALUES ($1, 'mac-primary', 'mac', 60, $2, $2, $2)
	`, nodeID, now.Add(-10*time.Second)); err != nil {
		t.Fatalf("insert dashboard node: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO latest_component_states (
			node_id,
			component,
			control_state,
			health,
			reason_code,
			generation,
			observed_at,
			updated_at
		) VALUES ($1, 'tunnel', 'healthy', 'ready', 'probe_succeeded', 1, $2, $2)
	`, nodeID, now.Add(-10*time.Second)); err != nil {
		t.Fatalf("insert component state: %v", err)
	}
	if _, err := admin.Exec(ctx, `
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
			'22222222-2222-4222-8222-222222222222',
			$1,
			'dashboard:test',
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
		)
	`, nodeID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("insert dashboard incident: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO worker_heartbeats (
			worker_name,
			instance_id,
			application_version,
			started_at,
			heartbeat_at
		) VALUES (
			'primary',
			'33333333-3333-4333-8333-333333333333',
			'v0.1.0',
			$1,
			$2
		)
	`, now.Add(-time.Hour), now.Add(-10*time.Second)); err != nil {
		t.Fatalf("insert dashboard worker: %v", err)
	}
	if _, err := admin.Exec(ctx, `
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
			'44444444-4444-4444-8444-444444444444',
			'hour',
			'mac:primary',
			$1,
			'twilight_transport',
			'availability_99_9',
			$2,
			$3,
			3600000,
			3599000,
			1000,
			0,
			0,
			0,
			$3
		)
	`, nodeID, now.Add(-time.Hour), now); err != nil {
		t.Fatalf("insert dashboard SLO: %v", err)
	}
}
