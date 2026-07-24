package slo

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSLOUpsertIsIdempotentAndPreservesIncidentLinks(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := sloIntegrationPool(t, ctx, adminDSN)
	maintenance := sloIntegrationPool(t, ctx, maintenanceDSN)
	resetSLOData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetSLOData(t, cleanupCtx, admin)
	})

	request := hourlyRequest()
	seedSLOData(t, ctx, admin, request.WindowStart)
	aggregate, err := CalculateTwilight(request, []TwilightState{
		{
			At:               request.WindowStart,
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   true,
		},
		{
			At:               request.WindowStart.Add(15 * time.Minute),
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   false,
			IncidentID:       firstIncident,
		},
		{
			At:               request.WindowStart.Add(45 * time.Minute),
			Awake:            true,
			CarrierAvailable: true,
			TransportReady:   true,
			IncidentID:       firstIncident,
		},
	})
	if err != nil {
		t.Fatalf("CalculateTwilight() error = %v", err)
	}
	store, err := NewPostgresStore(
		maintenance,
		bytes.NewReader(make([]byte, 16)),
	)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	firstID, err := store.Upsert(ctx, aggregate)
	if err != nil {
		t.Fatalf("Upsert(first) error = %v", err)
	}
	aggregate.ComputedAt = aggregate.ComputedAt.Add(time.Minute)
	secondID, err := store.Upsert(ctx, aggregate)
	if err != nil {
		t.Fatalf("Upsert(second) error = %v", err)
	}
	if firstID != secondID {
		t.Fatalf("aggregate IDs = %s and %s", firstID, secondID)
	}

	var (
		aggregateCount int
		linkCount      int
		eligible       int64
		good           int64
		bad            int64
		excluded       int64
		computedAt     time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			count(*) OVER (),
			eligible_milliseconds,
			good_milliseconds,
			bad_milliseconds,
			excluded_milliseconds,
			computed_at
		FROM slo_aggregates
		WHERE slo_aggregate_id = $1
	`, string(firstID)).Scan(
		&aggregateCount,
		&eligible,
		&good,
		&bad,
		&excluded,
		&computedAt,
	); err != nil {
		t.Fatalf("read SLO aggregate: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT count(*)
		FROM slo_incident_links
		WHERE slo_aggregate_id = $1
	`, string(firstID)).Scan(&linkCount); err != nil {
		t.Fatalf("count SLO links: %v", err)
	}
	if aggregateCount != 1 ||
		linkCount != 2 ||
		eligible != aggregate.EligibleMilliseconds ||
		good != aggregate.GoodMilliseconds ||
		bad != aggregate.BadMilliseconds ||
		excluded != aggregate.ExcludedMilliseconds ||
		!computedAt.Equal(aggregate.ComputedAt) {
		t.Fatalf(
			"stored SLO = count:%d links:%d eligible:%d good:%d bad:%d excluded:%d computed:%v",
			aggregateCount,
			linkCount,
			eligible,
			good,
			bad,
			excluded,
			computedAt,
		)
	}
}

func sloIntegrationPool(
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

func resetSLOData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			slo_incident_links,
			slo_aggregates,
			incident_events,
			incident_transitions,
			incidents,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset SLO data: %v", err)
	}
}

func seedSLOData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	at time.Time,
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
		) VALUES ($1, 'slo-test', 'mac', 60, $2, $2, $2)
	`, string(sloNodeID), at)
	if err != nil {
		t.Fatalf("insert SLO node: %v", err)
	}
	for _, fixture := range []struct {
		incidentID string
		key        string
	}{
		{string(firstIncident), "slo:first"},
		{string(secondIncident), "slo:second"},
	} {
		_, err = admin.Exec(ctx, `
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
				$3,
				'availability',
				'tunnel',
				'warning',
				'resolved',
				FALSE,
				1,
				$4,
				$5,
				$5,
				$4,
				$5
			)
		`,
			fixture.incidentID,
			string(sloNodeID),
			fixture.key,
			at,
			at.Add(time.Minute),
		)
		if err != nil {
			t.Fatalf("insert SLO incident %s: %v", fixture.incidentID, err)
		}
	}
}
