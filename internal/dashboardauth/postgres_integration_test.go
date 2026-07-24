package dashboardauth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresPasskeyStoreUsesNarrowAuthRole(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	authDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_AUTH_DSN")
	if adminDSN == "" || authDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := authIntegrationPool(t, ctx, adminDSN)
	auth := authIntegrationPool(t, ctx, authDSN)
	resetAuthData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetAuthData(t, cleanupCtx, admin)
	})
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	_, err := admin.Exec(ctx, `
		INSERT INTO dashboard_principals (
			principal_id,
			username,
			display_name,
			webauthn_user_handle,
			enabled,
			created_at
		) VALUES ($1, 'operator', 'Operator', $2, TRUE, $3)
	`, string(authPrincipalID), bytes.Repeat([]byte{1}, 32), now)
	if err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	store, err := NewPostgresStore(auth, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	user, err := store.LoadUser(ctx, "operator")
	if err != nil || len(user.Credentials) != 0 {
		t.Fatalf("LoadUser(empty) = %+v, %v", user, err)
	}
	credential := webauthn.Credential{
		ID:        bytes.Repeat([]byte{2}, 32),
		PublicKey: []byte{1, 2, 3},
		Transport: []protocol.AuthenticatorTransport{protocol.Internal},
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:     bytes.Repeat([]byte{3}, 16),
			SignCount:  4,
			Attachment: protocol.Platform,
		},
	}
	if err := store.AddCredential(ctx, user, &credential, now); err != nil {
		t.Fatalf("AddCredential() error = %v", err)
	}
	if err := store.AddCredential(ctx, user, &credential, now); !errors.Is(err, ErrCredentialExists) {
		t.Fatalf("AddCredential(duplicate) error = %v", err)
	}
	loaded, err := store.LoadUser(ctx, "operator")
	if err != nil || len(loaded.Credentials) != 1 {
		t.Fatalf("LoadUser(with credential) = %+v, %v", loaded, err)
	}
	stored := loaded.Credentials[0]
	if !bytes.Equal(stored.ID, credential.ID) ||
		!bytes.Equal(stored.PublicKey, credential.PublicKey) ||
		stored.Authenticator.SignCount != 4 ||
		stored.Authenticator.Attachment != protocol.Platform ||
		!stored.Flags.BackupEligible ||
		!stored.Flags.BackupState {
		t.Fatalf("stored credential = %+v", stored)
	}
	stored.Authenticator.SignCount = 5
	stored.Flags.BackupState = false
	if err := store.UpdateCredential(
		ctx,
		loaded,
		&stored,
		now.Add(time.Minute),
	); err != nil {
		t.Fatalf("UpdateCredential() error = %v", err)
	}
	var (
		signCount           int64
		backupState         bool
		lastAuthenticatedAt time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			c.sign_count,
			c.backup_state,
			p.last_authenticated_at
		FROM passkey_credentials c
		JOIN dashboard_principals p ON p.principal_id = c.principal_id
		WHERE c.credential_id = $1
	`, credential.ID).Scan(
		&signCount,
		&backupState,
		&lastAuthenticatedAt,
	); err != nil {
		t.Fatalf("read updated credential: %v", err)
	}
	if signCount != 5 ||
		backupState ||
		!lastAuthenticatedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf(
			"updated credential = count:%d backup:%t authenticated:%v",
			signCount,
			backupState,
			lastAuthenticatedAt,
		)
	}
}

func authIntegrationPool(
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

func resetAuthData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	if _, err := admin.Exec(ctx, `
		TRUNCATE TABLE passkey_credentials, dashboard_principals CASCADE
	`); err != nil {
		t.Fatalf("reset auth data: %v", err)
	}
}
