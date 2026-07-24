package dashboardauth

import (
	"errors"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type User struct {
	PrincipalID metadata.UUID
	Username    string
	DisplayName string
	UserHandle  []byte
	Enabled     bool
	Credentials []webauthn.Credential
}

func (user User) WebAuthnID() []byte {
	return append([]byte(nil), user.UserHandle...)
}

func (user User) WebAuthnName() string {
	return user.Username
}

func (user User) WebAuthnDisplayName() string {
	return user.DisplayName
}

func (user User) WebAuthnCredentials() []webauthn.Credential {
	return append([]webauthn.Credential(nil), user.Credentials...)
}

var (
	ErrInvalidAuthConfig = errors.New("invalid dashboard authentication configuration")
	ErrUnauthorized      = errors.New("dashboard authentication failed")
	ErrUserNotFound      = errors.New("dashboard principal not found")
	ErrCredentialExists  = errors.New("passkey credential already exists")
	ErrCeremonyExpired   = errors.New("passkey ceremony expired")
	ErrSessionExpired    = errors.New("dashboard session expired")
	ErrAuthUnavailable   = errors.New("dashboard authentication unavailable")
)
