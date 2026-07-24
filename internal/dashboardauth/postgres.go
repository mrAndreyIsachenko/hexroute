package dashboardauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresStore struct {
	database Database
	random   io.Reader
	randomMu sync.Mutex
}

func NewPostgresStore(
	database Database,
	randomSource io.Reader,
) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidAuthConfig
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &PostgresStore{
		database: database,
		random:   randomSource,
	}, nil
}

func (store *PostgresStore) LoadUser(
	ctx context.Context,
	username string,
) (User, error) {
	if ctx == nil || !validUsername(username) {
		return User{}, ErrUnauthorized
	}
	var (
		principalID string
		displayName string
		userHandle  []byte
		enabled     bool
	)
	err := store.database.QueryRow(ctx, `
		SELECT
			principal_id::text,
			display_name,
			webauthn_user_handle,
			enabled
		FROM dashboard_principals
		WHERE username = $1
	`, username).Scan(
		&principalID,
		&displayName,
		&userHandle,
		&enabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	user := User{
		PrincipalID: metadata.UUID(principalID),
		Username:    username,
		DisplayName: displayName,
		UserHandle:  append([]byte(nil), userHandle...),
		Enabled:     enabled,
	}
	if !validUser(user) {
		return User{}, ErrUnauthorized
	}
	rows, err := store.database.Query(ctx, `
		SELECT
			credential_id,
			cose_public_key,
			sign_count,
			aaguid::text,
			transports,
			user_present,
			user_verified,
			backup_eligible,
			backup_state,
			clone_warning,
			authenticator_attachment
		FROM passkey_credentials
		WHERE principal_id = $1
		  AND revoked_at IS NULL
		ORDER BY created_at, passkey_credential_id
	`, principalID)
	if err != nil {
		return User{}, err
	}
	defer rows.Close()
	for rows.Next() {
		credential, scanErr := scanCredential(rows)
		if scanErr != nil {
			return User{}, scanErr
		}
		user.Credentials = append(user.Credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return User{}, err
	}
	return user, nil
}

func (store *PostgresStore) AddCredential(
	ctx context.Context,
	user User,
	credential *webauthn.Credential,
	at time.Time,
) (err error) {
	if ctx == nil ||
		!validUser(user) ||
		credential == nil ||
		!validCredential(*credential) ||
		at.IsZero() {
		return ErrInvalidAuthConfig
	}
	credentialID, err := store.nextID()
	if err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	tag, err := transaction.Exec(ctx, `
		INSERT INTO passkey_credentials (
			passkey_credential_id,
			principal_id,
			credential_id,
			cose_public_key,
			sign_count,
			aaguid,
			transports,
			nickname,
			user_present,
			user_verified,
			backup_eligible,
			backup_state,
			clone_warning,
			authenticator_attachment,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, '',
			$8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (credential_id) DO NOTHING
	`,
		string(credentialID),
		string(user.PrincipalID),
		credential.ID,
		credential.PublicKey,
		int64(credential.Authenticator.SignCount),
		nullableAAGUID(credential.Authenticator.AAGUID),
		transportStrings(credential.Transport),
		credential.Flags.UserPresent,
		credential.Flags.UserVerified,
		credential.Flags.BackupEligible,
		credential.Flags.BackupState,
		credential.Authenticator.CloneWarning,
		string(credential.Authenticator.Attachment),
		at.UTC(),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrCredentialExists
	}
	if _, err = transaction.Exec(ctx, `
		UPDATE dashboard_principals
		SET last_authenticated_at = $2
		WHERE principal_id = $1
	`, string(user.PrincipalID), at.UTC()); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (store *PostgresStore) UpdateCredential(
	ctx context.Context,
	user User,
	credential *webauthn.Credential,
	at time.Time,
) (err error) {
	if ctx == nil ||
		!validUser(user) ||
		credential == nil ||
		!validCredential(*credential) ||
		at.IsZero() {
		return ErrInvalidAuthConfig
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, transaction, &err)
	tag, err := transaction.Exec(ctx, `
		UPDATE passkey_credentials
		SET sign_count = $3,
		    user_present = $4,
		    user_verified = $5,
		    backup_state = $6,
		    clone_warning = $7,
		    last_used_at = $8
		WHERE principal_id = $1
		  AND credential_id = $2
		  AND revoked_at IS NULL
	`,
		string(user.PrincipalID),
		credential.ID,
		int64(credential.Authenticator.SignCount),
		credential.Flags.UserPresent,
		credential.Flags.UserVerified,
		credential.Flags.BackupState,
		credential.Authenticator.CloneWarning,
		at.UTC(),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrUnauthorized
	}
	if _, err = transaction.Exec(ctx, `
		UPDATE dashboard_principals
		SET last_authenticated_at = $2
		WHERE principal_id = $1
	`, string(user.PrincipalID), at.UTC()); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func scanCredential(row pgx.Row) (webauthn.Credential, error) {
	var (
		credentialID   []byte
		publicKey      []byte
		signCount      int64
		aaguid         *string
		transports     []string
		userPresent    bool
		userVerified   bool
		backupEligible bool
		backupState    bool
		cloneWarning   bool
		attachment     string
	)
	if err := row.Scan(
		&credentialID,
		&publicKey,
		&signCount,
		&aaguid,
		&transports,
		&userPresent,
		&userVerified,
		&backupEligible,
		&backupState,
		&cloneWarning,
		&attachment,
	); err != nil {
		return webauthn.Credential{}, err
	}
	if signCount < 0 || signCount > int64(^uint32(0)) {
		return webauthn.Credential{}, ErrUnauthorized
	}
	rawAAGUID, err := parseAAGUID(aaguid)
	if err != nil {
		return webauthn.Credential{}, ErrUnauthorized
	}
	credential := webauthn.Credential{
		ID:        append([]byte(nil), credentialID...),
		PublicKey: append([]byte(nil), publicKey...),
		Transport: protocolTransports(transports),
		Flags: webauthn.CredentialFlags{
			UserPresent:    userPresent,
			UserVerified:   userVerified,
			BackupEligible: backupEligible,
			BackupState:    backupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:       rawAAGUID,
			SignCount:    uint32(signCount),
			CloneWarning: cloneWarning,
			Attachment:   protocol.AuthenticatorAttachment(attachment),
		},
	}
	if !validCredential(credential) {
		return webauthn.Credential{}, ErrUnauthorized
	}
	return credential, nil
}

func validUser(user User) bool {
	if _, err := metadata.ParseUUID(string(user.PrincipalID)); err != nil {
		return false
	}
	return validUsername(user.Username) &&
		user.DisplayName != "" &&
		len(user.DisplayName) <= 128 &&
		len(user.UserHandle) >= 16 &&
		len(user.UserHandle) <= 64 &&
		user.Enabled
}

func validCredential(credential webauthn.Credential) bool {
	if len(credential.ID) < 16 ||
		len(credential.ID) > 1024 ||
		len(credential.PublicKey) < 1 ||
		len(credential.PublicKey) > 4096 ||
		(len(credential.Authenticator.AAGUID) != 0 &&
			len(credential.Authenticator.AAGUID) != 16) ||
		!credential.Flags.UserPresent ||
		!credential.Flags.UserVerified ||
		credential.Authenticator.CloneWarning ||
		(credential.Authenticator.Attachment != "" &&
			credential.Authenticator.Attachment != protocol.Platform &&
			credential.Authenticator.Attachment != protocol.CrossPlatform) {
		return false
	}
	for _, transport := range credential.Transport {
		switch transport {
		case protocol.USB,
			protocol.NFC,
			protocol.BLE,
			protocol.Hybrid,
			protocol.Internal,
			protocol.SmartCard:
		default:
			return false
		}
	}
	return true
}

func validUsername(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		if index > 0 && strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func (store *PostgresStore) nextID() (metadata.UUID, error) {
	store.randomMu.Lock()
	defer store.randomMu.Unlock()
	return metadata.NewUUID(store.random)
}

func transportStrings(values []protocol.AuthenticatorTransport) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func protocolTransports(values []string) []protocol.AuthenticatorTransport {
	result := make([]protocol.AuthenticatorTransport, 0, len(values))
	for _, value := range values {
		result = append(result, protocol.AuthenticatorTransport(value))
	}
	return result
}

func nullableAAGUID(raw []byte) any {
	if len(raw) != 16 {
		return nil
	}
	encoded := hex.EncodeToString(raw)
	return encoded[0:8] + "-" +
		encoded[8:12] + "-" +
		encoded[12:16] + "-" +
		encoded[16:20] + "-" +
		encoded[20:32]
}

func parseAAGUID(value *string) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	compact := strings.ReplaceAll(*value, "-", "")
	raw, err := hex.DecodeString(compact)
	if err != nil || len(raw) != 16 {
		return nil, ErrUnauthorized
	}
	return raw, nil
}

func rollback(ctx context.Context, transaction pgx.Tx, resultErr *error) {
	rollbackErr := transaction.Rollback(ctx)
	if *resultErr == nil &&
		rollbackErr != nil &&
		!errors.Is(rollbackErr, pgx.ErrTxClosed) {
		*resultErr = rollbackErr
	}
}
