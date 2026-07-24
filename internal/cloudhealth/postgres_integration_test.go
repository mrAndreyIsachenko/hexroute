package cloudhealth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

func TestPostgresHeartbeatDrivesReadiness(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	ingestDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_INGEST_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || ingestDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminPool := integrationPool(t, ctx, adminDSN)
	ingestPool := integrationPool(t, ctx, ingestDSN)
	maintenancePool := integrationPool(t, ctx, maintenanceDSN)
	_, err := adminPool.Exec(ctx, "DELETE FROM worker_heartbeats")
	if err != nil {
		t.Fatalf("clear worker heartbeats: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := adminPool.Exec(
			cleanupCtx,
			"DELETE FROM worker_heartbeats",
		); cleanupErr != nil {
			t.Errorf("cleanup worker heartbeats: %v", cleanupErr)
		}
	})

	ingestStore, err := NewPostgresStore(ingestPool)
	if err != nil {
		t.Fatalf("NewPostgresStore(ingest) error = %v", err)
	}
	maintenanceStore, err := NewPostgresStore(maintenancePool)
	if err != nil {
		t.Fatalf("NewPostgresStore(maintenance) error = %v", err)
	}
	now := time.Date(2026, time.July, 24, 21, 0, 0, 0, time.UTC)
	checker, err := NewChecker(
		ingestStore,
		"primary",
		time.Minute,
		15*time.Second,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}
	if status, err := checker.Check(ctx); status != StatusNotReady || err == nil {
		t.Fatalf("Check(missing) = %q, %v", status, err)
	}

	writer, err := NewWriter(
		maintenanceStore,
		"primary",
		healthInstanceID,
		"v0.1.0-integration",
		now.Add(-time.Minute),
		30*time.Second,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Once(ctx); err != nil {
		t.Fatalf("Once() error = %v", err)
	}
	if status, err := checker.Check(ctx); status != StatusReady || err != nil {
		t.Fatalf("Check(fresh) = %q, %v", status, err)
	}

	olderWriter, err := NewWriter(
		maintenanceStore,
		"primary",
		metadata.UUID("22222222-2222-4222-8222-222222222222"),
		"v0.0.9-older",
		now.Add(-2*time.Minute),
		30*time.Second,
		func() time.Time { return now.Add(-30 * time.Second) },
	)
	if err != nil {
		t.Fatalf("NewWriter(older) error = %v", err)
	}
	if err := olderWriter.Once(ctx); err != nil {
		t.Fatalf("Once(older) error = %v", err)
	}
	stored, err := ingestStore.ReadHeartbeat(ctx, "primary")
	if err != nil {
		t.Fatalf("ReadHeartbeat() error = %v", err)
	}
	if stored.InstanceID != healthInstanceID || stored.HeartbeatAt != now {
		t.Fatalf("older heartbeat replaced current row: %+v", stored)
	}

	staleChecker, err := NewChecker(
		ingestStore,
		"primary",
		time.Minute,
		15*time.Second,
		func() time.Time { return now.Add(2 * time.Minute) },
	)
	if err != nil {
		t.Fatalf("NewChecker(stale) error = %v", err)
	}
	if status, err := staleChecker.Check(ctx); status != StatusNotReady || err == nil {
		t.Fatalf("Check(stale) = %q, %v", status, err)
	}

	recoveredAt := now.Add(2 * time.Minute)
	recoveredWriter, err := NewWriter(
		maintenanceStore,
		"primary",
		healthInstanceID,
		"v0.1.0-integration",
		now.Add(-time.Minute),
		30*time.Second,
		func() time.Time { return recoveredAt },
	)
	if err != nil {
		t.Fatalf("NewWriter(recovered) error = %v", err)
	}
	if err := recoveredWriter.Once(ctx); err != nil {
		t.Fatalf("Once(recovered) error = %v", err)
	}
	if status, err := staleChecker.Check(ctx); status != StatusReady || err != nil {
		t.Fatalf("Check(recovered) = %q, %v", status, err)
	}
}

func integrationPool(
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
