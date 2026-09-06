package configprove

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	integrationVersionID  = metadata.UUID("aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa")
	integrationSecondID   = metadata.UUID("bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb")
	integrationDeployment = metadata.UUID("cccccccc-3333-4333-8333-cccccccccccc")
	integrationSecondDep  = metadata.UUID("dddddddd-4444-4444-8444-dddddddddddd")
)

// The deployments table has never had a row, and the dashboard view that joins
// it has never rendered anything. This is that row, and the lifecycle it moves
// through.
func TestPostgresProvingRecordsTheDeploymentAndTheLifecycle(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := provePool(t, ctx, adminDSN)
	maintenance := provePool(t, ctx, maintenanceDSN)
	resetProveData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetProveData(t, cleanupCtx, admin)
	})

	published := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
	seedVersion(t, ctx, admin, integrationVersionID, "2026-09-06.1", published)
	ledger, err := NewPostgresLedger(maintenance, "0.0.0-test")
	if err != nil {
		t.Fatalf("NewPostgresLedger() error = %v", err)
	}

	version, err := ledger.LatestVersion(ctx, "node", "ingress-provider-b")
	if err != nil || version.Lifecycle != LifecycleStaged ||
		version.VersionLabel != "2026-09-06.1" ||
		version.ConfigVersionID != integrationVersionID {
		t.Fatalf("LatestVersion() = %+v, %v", version, err)
	}

	activated := published.Add(time.Minute)
	if err := ledger.MarkActive(ctx, version, integrationDeployment, activated); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	// A prover that runs on a timer runs again. One deployment, not two.
	if err := ledger.MarkActive(ctx, version, integrationSecondDep, activated.Add(time.Minute)); err != nil {
		t.Fatalf("MarkActive(repeat) error = %v", err)
	}
	assertLifecycle(t, ctx, admin, integrationVersionID, "active", "")
	assertDeployment(t, ctx, admin, integrationVersionID, "started", 1)

	version.Lifecycle = LifecycleActive
	version.ActivatedAt = activated
	if err := ledger.MarkProven(ctx, version, activated.Add(2*time.Hour)); err != nil {
		t.Fatalf("MarkProven() error = %v", err)
	}
	assertLifecycle(t, ctx, admin, integrationVersionID, "proven", "")
	assertDeployment(t, ctx, admin, integrationVersionID, "healthy", 1)

	// A second version that never proves. The host has already returned to
	// what it was running, so the deployment is rolled back rather than
	// failed, and the reason it did not prove is kept.
	seedVersion(t, ctx, admin, integrationSecondID, "2026-09-07.1", published.Add(24*time.Hour))
	unproven, err := ledger.LatestVersion(ctx, "node", "ingress-provider-b")
	if err != nil || unproven.ConfigVersionID != integrationSecondID {
		t.Fatalf("LatestVersion(second) = %+v, %v", unproven, err)
	}
	if err := ledger.MarkActive(ctx, unproven, integrationSecondDep,
		published.Add(25*time.Hour)); err != nil {
		t.Fatalf("MarkActive(second) error = %v", err)
	}
	if err := ledger.MarkRejected(ctx, unproven, published.Add(32*time.Hour),
		"transport_unhealthy"); err != nil {
		t.Fatalf("MarkRejected() error = %v", err)
	}
	assertLifecycle(t, ctx, admin, integrationSecondID, "rejected", "transport_unhealthy")
	assertDeployment(t, ctx, admin, integrationSecondID, "rolled_back", 1)

	// Nothing that runs unattended may bring a version into existence.
	if _, err := maintenance.Exec(ctx, `
		INSERT INTO config_versions (
			config_version_id, target_kind, target_key, schema_version,
			version_label, content_sha256, signing_key_id, lifecycle_status
		) VALUES (
			'eeeeeeee-5555-4555-8555-eeeeeeeeeeee', 'node', 'ingress-provider-b',
			1, 'invented.1', repeat('\x03', 32)::bytea, $1, 'staged'
		)
	`, strings.Repeat("a", 64)); err == nil {
		t.Fatal("the worker was allowed to publish a configuration version")
	}
}

func provePool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
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

func resetProveData(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if _, err := admin.Exec(ctx,
		`TRUNCATE TABLE deployments, config_versions RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset error = %v", err)
	}
}

func seedVersion(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	versionID metadata.UUID,
	label string,
	createdAt time.Time,
) {
	t.Helper()
	if _, err := admin.Exec(ctx, `
		INSERT INTO config_versions (
			config_version_id, target_kind, target_key, schema_version,
			version_label, content_sha256, signing_key_id, lifecycle_status,
			created_at
		) VALUES ($1, 'node', 'ingress-provider-b', 1, $2, $3, $4, 'staged', $5)
	`, string(versionID), label, bytes.Repeat([]byte{3}, 32),
		strings.Repeat("a", 64), createdAt); err != nil {
		t.Fatalf("seed error = %v", err)
	}
}

func assertLifecycle(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	versionID metadata.UUID,
	want string,
	wantReason string,
) {
	t.Helper()
	var (
		lifecycle string
		reason    *string
	)
	if err := admin.QueryRow(ctx, `
		SELECT lifecycle_status, unproven_reason
		FROM config_versions WHERE config_version_id = $1
	`, string(versionID)).Scan(&lifecycle, &reason); err != nil {
		t.Fatalf("lifecycle error = %v", err)
	}
	if lifecycle != want {
		t.Fatalf("lifecycle = %q, want %q", lifecycle, want)
	}
	if wantReason == "" {
		if reason != nil {
			t.Fatalf("unproven_reason = %q, want none", *reason)
		}
		return
	}
	if reason == nil || *reason != wantReason {
		t.Fatalf("unproven_reason = %v, want %q", reason, wantReason)
	}
}

func assertDeployment(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	versionID metadata.UUID,
	wantStatus string,
	wantRows int,
) {
	t.Helper()
	var (
		rows        int
		status      string
		completedAt *time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM deployments WHERE config_version_id = $1
	`, string(versionID)).Scan(&rows); err != nil {
		t.Fatalf("deployment count error = %v", err)
	}
	if rows != wantRows {
		t.Fatalf("deployments = %d, want %d", rows, wantRows)
	}
	if err := admin.QueryRow(ctx, `
		SELECT deployment_status, completed_at
		FROM deployments WHERE config_version_id = $1
	`, string(versionID)).Scan(&status, &completedAt); err != nil {
		t.Fatalf("deployment error = %v", err)
	}
	if status != wantStatus {
		t.Fatalf("deployment status = %q, want %q", status, wantStatus)
	}
	if (status == "started") != (completedAt == nil) {
		t.Fatalf("deployment %q completed_at = %v", status, completedAt)
	}
}
