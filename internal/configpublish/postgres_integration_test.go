package configpublish

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	ledgerVersionID = metadata.UUID("aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa")
	otherVersionID  = metadata.UUID("bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb")
)

// The ledger is the only producer these two tables have ever had. What this
// proves is that the row lands, that publishing the same version again is
// free, and that the publisher's grant stops exactly where its authority does.
func TestPostgresConfigVersionLedgerRecordsOnceAndRefusesAReusedLabel(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	publisherDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_PUBLISHER_DSN")
	if adminDSN == "" || publisherDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := ledgerPool(t, ctx, adminDSN)
	publisher := ledgerPool(t, ctx, publisherDSN)
	resetVersions(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetVersions(t, cleanupCtx, admin)
	})

	ledger, err := NewPostgresLedger(publisher)
	if err != nil {
		t.Fatalf("NewPostgresLedger() error = %v", err)
	}
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	record := Record{
		ConfigVersionID: ledgerVersionID,
		TargetKind:      "node",
		TargetKey:       "ingress-provider-b",
		SchemaVersion:   1,
		VersionLabel:    "2026-09-06.1",
		SigningKeyID:    strings.Repeat("a", 64),
		ObjectKey:       "versions/node/ingress-provider-b/2026-09-06.1.json",
		CreatedAt:       now,
	}
	copy(record.ContentSHA256[:], bytes.Repeat([]byte{3}, 32))

	if err := ledger.RecordVersion(ctx, record); err != nil {
		t.Fatalf("RecordVersion() error = %v", err)
	}
	assertVersionRow(t, ctx, admin, record, "staged")

	// A publish interrupted between the row and the object is retried by
	// repeating it, so repeating has to be free.
	if err := ledger.RecordVersion(ctx, record); err != nil {
		t.Fatalf("RecordVersion(repeat) error = %v", err)
	}
	if count := versionCount(t, ctx, admin); count != 1 {
		t.Fatalf("rows after a repeated publish = %d", count)
	}

	different := record
	different.ConfigVersionID = otherVersionID
	copy(different.ContentSHA256[:], bytes.Repeat([]byte{4}, 32))
	if err := ledger.RecordVersion(ctx, different); !errors.Is(err, ErrLabelReused) {
		t.Fatalf("RecordVersion(reused label) error = %v", err)
	}
	if count := versionCount(t, ctx, admin); count != 1 {
		t.Fatalf("rows after a refused publish = %d", count)
	}

	// Whether a version is proven is not the publisher's to assert, and a
	// deployment is not its to record.
	for _, statement := range []string{
		`UPDATE config_versions SET lifecycle_status = 'proven'`,
		`INSERT INTO deployments (
			deployment_id, target_key, application_version, artifact_sha256,
			deployment_status, started_at
		 ) VALUES (
			'cccccccc-3333-4333-8333-cccccccccccc', 'ingress-provider-b',
			'1.0.0', repeat('\x03', 32)::bytea, 'staged', now()
		 )`,
	} {
		if _, err := publisher.Exec(ctx, statement); err == nil {
			t.Fatalf("the publisher was allowed to run: %s", statement)
		}
	}
}

func ledgerPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
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

func resetVersions(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if _, err := admin.Exec(ctx,
		`TRUNCATE TABLE deployments, config_versions RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset error = %v", err)
	}
}

func versionCount(t *testing.T, ctx context.Context, admin *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM config_versions`).
		Scan(&count); err != nil {
		t.Fatalf("count error = %v", err)
	}
	return count
}

func assertVersionRow(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	record Record,
	lifecycle string,
) {
	t.Helper()
	var (
		targetKind    string
		targetKey     string
		schemaVersion int
		label         string
		digest        []byte
		signingKey    string
		status        string
		activatedAt   *time.Time
		provenAt      *time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT target_kind, target_key, schema_version, version_label,
		       content_sha256, signing_key_id, lifecycle_status,
		       activated_at, proven_at
		FROM config_versions
		WHERE config_version_id = $1
	`, string(record.ConfigVersionID)).Scan(
		&targetKind, &targetKey, &schemaVersion, &label,
		&digest, &signingKey, &status, &activatedAt, &provenAt,
	); err != nil {
		t.Fatalf("row error = %v", err)
	}
	if targetKind != record.TargetKind || targetKey != record.TargetKey ||
		schemaVersion != record.SchemaVersion || label != record.VersionLabel ||
		!bytes.Equal(digest, record.ContentSHA256[:]) ||
		signingKey != record.SigningKeyID || status != lifecycle {
		t.Fatalf("row = %s/%s/%d/%s/%s/%s",
			targetKind, targetKey, schemaVersion, label, signingKey, status)
	}
	// A published version is staged and nothing more. Activation and proof
	// are what a host and its heartbeat establish later.
	if activatedAt != nil || provenAt != nil {
		t.Fatal("a published version was already activated or proven")
	}
}
