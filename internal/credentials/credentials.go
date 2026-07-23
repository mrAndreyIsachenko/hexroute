package credentials

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

var (
	ErrCredentialsClosed   = errors.New("credentials are closed")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrSecretSerialization = errors.New("secret serialization is forbidden")
)

type Source interface {
	ReadPritunl(context.Context) (*Pritunl, error)
}

type Pritunl struct {
	pin      secret
	totpSeed secret
	closed   bool
}

func newPritunl(pin, totpSeed []byte) (*Pritunl, error) {
	if len(pin) == 0 || len(totpSeed) == 0 {
		return nil, ErrInvalidCredentials
	}
	return &Pritunl{
		pin:      newSecret(pin),
		totpSeed: newSecret(totpSeed),
	}, nil
}

func (credentials *Pritunl) UsePIN(use func([]byte) error) error {
	if credentials == nil || credentials.closed {
		return ErrCredentialsClosed
	}
	return credentials.pin.use(use)
}

func (credentials *Pritunl) UseTOTPSeed(use func([]byte) error) error {
	if credentials == nil || credentials.closed {
		return ErrCredentialsClosed
	}
	return credentials.totpSeed.use(use)
}

func (credentials *Pritunl) Close() error {
	if credentials == nil || credentials.closed {
		return nil
	}
	credentials.pin.clear()
	credentials.totpSeed.clear()
	credentials.closed = true
	return nil
}

func (Pritunl) String() string {
	return "[REDACTED]"
}

func (Pritunl) GoString() string {
	return "[REDACTED]"
}

func (Pritunl) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

func (Pritunl) MarshalJSON() ([]byte, error) {
	return nil, ErrSecretSerialization
}

func (Pritunl) MarshalText() ([]byte, error) {
	return nil, ErrSecretSerialization
}

func (Pritunl) MarshalBinary() ([]byte, error) {
	return nil, ErrSecretSerialization
}

type secret struct {
	value []byte
}

func newSecret(value []byte) secret {
	return secret{value: append([]byte(nil), value...)}
}

func (secret *secret) use(use func([]byte) error) error {
	if secret == nil || len(secret.value) == 0 || use == nil {
		return ErrCredentialsClosed
	}
	value := append([]byte(nil), secret.value...)
	defer clear(value)
	return use(value)
}

func (secret *secret) clear() {
	if secret == nil {
		return
	}
	clear(secret.value)
	secret.value = nil
}

var (
	_ fmt.Stringer             = Pritunl{}
	_ fmt.GoStringer           = Pritunl{}
	_ slog.LogValuer           = Pritunl{}
	_ json.Marshaler           = Pritunl{}
	_ encoding.TextMarshaler   = Pritunl{}
	_ encoding.BinaryMarshaler = Pritunl{}
)
