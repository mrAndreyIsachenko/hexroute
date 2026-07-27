package cutoverfreeze

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresFreezeDrainsInflightWritesAndRejectsLaterWrites(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	ingestDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_INGEST_DSN")
	if adminDSN == "" || ingestDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer admin.Close()
	ingest, err := pgxpool.New(ctx, ingestDSN)
	if err != nil {
		t.Fatalf("ingest pool: %v", err)
	}
	defer ingest.Close()

	thaw := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanup, `
			UPDATE cutover_write_control
			SET cutover_id = NULL,
			    write_frozen = FALSE,
			    frozen_at = NULL,
			    deadline_at = NULL
			WHERE singleton
		`)
		_, _ = admin.Exec(cleanup, `
			DELETE FROM security_audit_records
			WHERE audit_record_id IN (
				'71000000-0000-4000-8000-000000000001',
				'71000000-0000-4000-8000-000000000002'
			)
		`)
	}
	thaw()
	defer thaw()

	transaction, err := ingest.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin ingest write: %v", err)
	}
	defer transaction.Rollback(ctx)
	if _, err = transaction.Exec(ctx, `
		INSERT INTO security_audit_records (
			audit_record_id, category, reason_code
		) VALUES (
			'71000000-0000-4000-8000-000000000001',
			'schema',
			'freeze_barrier_probe'
		)
	`); err != nil {
		t.Fatalf("in-flight write: %v", err)
	}

	freezeResult := make(chan error, 1)
	go func() {
		_, freezeErr := admin.Exec(ctx, `
			UPDATE cutover_write_control
			SET cutover_id = '72000000-0000-4000-8000-000000000002',
			    write_frozen = TRUE,
			    frozen_at = CURRENT_TIMESTAMP,
			    deadline_at = CURRENT_TIMESTAMP + INTERVAL '15 minutes'
			WHERE singleton
		`)
		freezeResult <- freezeErr
	}()
	select {
	case err := <-freezeResult:
		t.Fatalf("freeze did not wait for in-flight write: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err = transaction.Commit(ctx); err != nil {
		t.Fatalf("commit in-flight write: %v", err)
	}
	select {
	case err := <-freezeResult:
		if err != nil {
			t.Fatalf("freeze after drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("freeze remained blocked after write committed")
	}

	_, err = ingest.Exec(ctx, `
		INSERT INTO security_audit_records (
			audit_record_id, category, reason_code
		) VALUES (
			'71000000-0000-4000-8000-000000000002',
			'schema',
			'frozen_write_probe'
		)
	`)
	if !IsWriteFrozen(err) {
		t.Fatalf("post-freeze write error = %v", err)
	}

	store, err := NewStore(ingest)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	state, err := store.Read(ctx)
	if err != nil || !state.Frozen || state.DeadlineAt.IsZero() {
		t.Fatalf("runtime freeze state = %+v error=%v", state, err)
	}
	_, err = ingest.Exec(ctx, `
		UPDATE cutover_write_control SET write_frozen = FALSE WHERE singleton
	`)
	if err == nil || IsWriteFrozen(err) {
		t.Fatalf("runtime role changed freeze state: %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected context error: %v", err)
	}
}
