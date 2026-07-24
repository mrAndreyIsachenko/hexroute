package cloudingest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

func TestPostgresStorePersistsDeduplicatesAndTracksSequenceGaps(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	ingestDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_INGEST_DSN")
	if adminDSN == "" || ingestDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin pgxpool.New() error = %v", err)
	}
	t.Cleanup(adminPool.Close)
	ingestPool, err := pgxpool.New(ctx, ingestDSN)
	if err != nil {
		t.Fatalf("ingest pgxpool.New() error = %v", err)
	}
	t.Cleanup(ingestPool.Close)
	if err := adminPool.Ping(ctx); err != nil {
		t.Fatalf("admin PostgreSQL ping error = %v", err)
	}
	if err := ingestPool.Ping(ctx); err != nil {
		t.Fatalf("ingest PostgreSQL ping error = %v", err)
	}

	resetIntegrationData(t, ctx, adminPool)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetIntegrationData(t, cleanupCtx, adminPool)
	})

	now := time.Date(2026, time.July, 24, 19, 0, 0, 0, time.UTC)
	key := postgresSigningKey(t)
	registerIntegrationKey(t, ctx, adminPool, key, now)
	store, err := NewPostgresStore(ingestPool, rand.Reader)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	service, err := NewService(store, 5*time.Minute, rand.Reader, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	firstBody := encodedTestBatch(
		t,
		cloudBatchID,
		testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		testEntry(t, 3, "cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	)
	firstSigned := signedTestBatch(t, key, cloudRequestID, now, firstBody)
	acknowledgement, err := service.Accept(ctx, firstSigned, firstBody)
	if err != nil {
		t.Fatalf("Accept(first) error = %v", err)
	}
	if len(acknowledgement.AcceptedEventIDs) != 2 {
		t.Fatalf("first acknowledgement = %+v", acknowledgement)
	}
	assertCount(t, ctx, adminPool, "events", 2)
	assertCount(t, ctx, adminPool, "batches", 1)
	assertOpenGap(t, ctx, adminPool, 2, 2)
	assertCursor(t, ctx, adminPool, 3, 4)

	duplicateBody := encodedTestBatch(
		t,
		metadata.UUID("55555555-5555-4555-8555-555555555555"),
		testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		testEntry(t, 3, "cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	)
	duplicateSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("66666666-6666-4666-8666-666666666666"),
		now,
		duplicateBody,
	)
	if _, err := service.Accept(ctx, duplicateSigned, duplicateBody); err != nil {
		t.Fatalf("Accept(duplicate events) error = %v", err)
	}
	assertCount(t, ctx, adminPool, "events", 2)
	assertCount(t, ctx, adminPool, "batches", 2)

	lateBody := encodedTestBatch(
		t,
		metadata.UUID("77777777-7777-4777-8777-777777777777"),
		testEntry(t, 2, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
	)
	lateSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("88888888-8888-4888-8888-888888888888"),
		now,
		lateBody,
	)
	if _, err := service.Accept(ctx, lateSigned, lateBody); err != nil {
		t.Fatalf("Accept(late sequence) error = %v", err)
	}
	assertCount(t, ctx, adminPool, "events", 3)
	assertResolvedGap(t, ctx, adminPool)

	if _, err := service.Accept(ctx, lateSigned, lateBody); err == nil {
		t.Fatal("Accept(replayed request) error = nil")
	}
	assertAudit(t, ctx, adminPool, "replay", "request_reused", 1)

	conflictBody := encodedTestBatch(
		t,
		metadata.UUID("99999999-9999-4999-8999-999999999999"),
		testEntry(t, 3, "dddddddd-dddd-4ddd-8ddd-dddddddddddd"),
	)
	conflictSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"),
		now,
		conflictBody,
	)
	if _, err := service.Accept(ctx, conflictSigned, conflictBody); err == nil {
		t.Fatal("Accept(sequence conflict) error = nil")
	}
	assertCount(t, ctx, adminPool, "events", 3)
	assertCount(t, ctx, adminPool, "batches", 3)
	assertAudit(t, ctx, adminPool, "schema", "sequence_conflict", 1)

	splitSessionID := metadata.UUID("13131313-1313-4313-8313-131313131313")
	splitFirst := testEntry(t, 1, "16161616-1616-4616-8616-161616161616")
	splitFirst.Metadata.SessionID = splitSessionID
	splitLast := testEntry(t, 5, "17171717-1717-4717-8717-171717171717")
	splitLast.Metadata.SessionID = splitSessionID
	splitBody := encodedTestBatch(
		t,
		metadata.UUID("14141414-1414-4414-8414-141414141414"),
		splitFirst,
		splitLast,
	)
	splitSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("15151515-1515-4515-8515-151515151515"),
		now,
		splitBody,
	)
	if _, err := service.Accept(ctx, splitSigned, splitBody); err != nil {
		t.Fatalf("Accept(split gap source) error = %v", err)
	}

	splitMiddle := testEntry(t, 3, "20202020-2020-4020-8020-202020202020")
	splitMiddle.Metadata.SessionID = splitSessionID
	splitMiddleBody := encodedTestBatch(
		t,
		metadata.UUID("18181818-1818-4818-8818-181818181818"),
		splitMiddle,
	)
	splitMiddleSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("19191919-1919-4919-8919-191919191919"),
		now,
		splitMiddleBody,
	)
	if _, err := service.Accept(ctx, splitMiddleSigned, splitMiddleBody); err != nil {
		t.Fatalf("Accept(split gap middle) error = %v", err)
	}
	assertSplitGaps(t, ctx, adminPool, splitSessionID, "2-2,4-4")
}

func postgresSigningKey(t *testing.T) signing.Key {
	t.Helper()
	randomBytes := make([]byte, ed25519.SeedSize+16)
	randomBytes[0] = 21
	key, err := signing.GenerateFile(
		filepath.Join(t.TempDir(), "postgres-node.json"),
		cloudNodeID,
		bytes.NewReader(randomBytes),
	)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	return key
}

func registerIntegrationKey(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	key signing.Key,
	now time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO nodes (node_id, node_name, node_kind)
		VALUES ($1, 'integration-node', 'mac')
	`, string(key.NodeID))
	if err != nil {
		t.Fatalf("register integration node: %v", err)
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
			'12121212-1212-4212-8212-121212121212',
			$1,
			$2,
			$3,
			'active',
			$4
		)
	`, string(key.NodeID), string(key.KeyID), []byte(key.PublicKey()), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("register integration key: %v", err)
	}
}

func resetIntegrationData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			security_audit_records,
			sequence_gaps,
			node_sequence_cursors,
			events,
			batches,
			node_public_keys,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset integration data: %v", err)
	}
}

func assertCount(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	table string,
	want int,
) {
	t.Helper()
	query := "SELECT count(*) FROM " + table
	var count int
	if err := admin.QueryRow(ctx, query).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func assertOpenGap(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	wantFirst uint64,
	wantLast uint64,
) {
	t.Helper()
	var first, last uint64
	var resolved *time.Time
	err := admin.QueryRow(ctx, `
		SELECT first_sequence, last_sequence, resolved_at
		FROM sequence_gaps
	`).Scan(&first, &last, &resolved)
	if err != nil {
		t.Fatalf("query open sequence gap: %v", err)
	}
	if first != wantFirst || last != wantLast || resolved != nil {
		t.Fatalf("sequence gap = %d-%d resolved=%v", first, last, resolved)
	}
}

func assertResolvedGap(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	var resolved *time.Time
	if err := admin.QueryRow(ctx, `
		SELECT resolved_at
		FROM sequence_gaps
	`).Scan(&resolved); err != nil {
		t.Fatalf("query resolved sequence gap: %v", err)
	}
	if resolved == nil {
		t.Fatal("sequence gap is still open")
	}
}

func assertSplitGaps(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	sessionID metadata.UUID,
	want string,
) {
	t.Helper()
	var ranges string
	err := admin.QueryRow(ctx, `
		SELECT string_agg(
			first_sequence::text || '-' || last_sequence::text,
			',' ORDER BY first_sequence
		)
		FROM sequence_gaps
		WHERE boot_session_id = $1 AND resolved_at IS NULL
	`, string(sessionID)).Scan(&ranges)
	if err != nil {
		t.Fatalf("query split sequence gaps: %v", err)
	}
	if ranges != want {
		t.Fatalf("split sequence gaps = %q, want %q", ranges, want)
	}
}

func assertCursor(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	wantHighest uint64,
	wantNext uint64,
) {
	t.Helper()
	var highest, next uint64
	err := admin.QueryRow(ctx, `
		SELECT highest_sequence, next_expected_sequence
		FROM node_sequence_cursors
	`).Scan(&highest, &next)
	if err != nil {
		t.Fatalf("query sequence cursor: %v", err)
	}
	if highest != wantHighest || next != wantNext {
		t.Fatalf("sequence cursor = %d/%d, want %d/%d", highest, next, wantHighest, wantNext)
	}
}

func assertAudit(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	category string,
	reason string,
	want int,
) {
	t.Helper()
	var count int
	err := admin.QueryRow(ctx, `
		SELECT count(*)
		FROM security_audit_records
		WHERE category = $1 AND reason_code = $2
	`, category, reason).Scan(&count)
	if err != nil {
		t.Fatalf("query audit %s/%s: %v", category, reason, err)
	}
	if count != want {
		t.Fatalf("audit %s/%s count = %d, want %d", category, reason, count, want)
	}
}
