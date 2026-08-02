package policyapproval

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"time"
)

const maxKeychainSeed = 256

var keychainIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)

type KeychainReader interface {
	ReadUserPresence(context.Context, string, string) ([]byte, error)
}

type KeychainProvisioner interface {
	StoreUserPresence(context.Context, string, string, []byte) error
}

type KeychainConfig struct {
	Service             string
	Account             string
	PublicKey           ed25519.PublicKey
	RequireUserPresence bool
	PromptTimeout       time.Duration
}

type KeychainSigner struct {
	reader KeychainReader
	config KeychainConfig
}

var (
	ErrInvalidKeychainConfig      = errors.New("invalid policy Keychain signer configuration")
	ErrKeychainAccess             = errors.New("policy Keychain signing key unavailable")
	ErrKeychainDuplicate          = errors.New("policy Keychain signing key already exists")
	ErrKeychainInteractionDenied  = errors.New("policy Keychain user presence was denied")
	ErrKeychainMissingEntitlement = errors.New("policy Keychain user presence requires a signed binary entitlement")
	ErrKeychainAccessControl      = errors.New("policy Keychain user-presence control is unavailable")
)

func NewKeychainSigner(reader KeychainReader, config KeychainConfig) (*KeychainSigner, error) {
	if reader == nil || !keychainIdentifier.MatchString(config.Service) ||
		!keychainIdentifier.MatchString(config.Account) || len(config.PublicKey) != ed25519.PublicKeySize ||
		!config.RequireUserPresence || config.PromptTimeout <= 0 || config.PromptTimeout > 5*time.Minute {
		return nil, ErrInvalidKeychainConfig
	}
	config.PublicKey = append(ed25519.PublicKey(nil), config.PublicKey...)
	return &KeychainSigner{reader: reader, config: config}, nil
}

func (signer *KeychainSigner) PublicKey() (ed25519.PublicKey, error) {
	if signer == nil || len(signer.config.PublicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidKeychainConfig
	}
	return append(ed25519.PublicKey(nil), signer.config.PublicKey...), nil
}

func (signer *KeychainSigner) Sign(message []byte) ([]byte, error) {
	if signer == nil || len(message) == 0 || len(message) > policyApprovalMessageLimit {
		return nil, ErrInvalidApproval
	}
	seed, err := readKeychainSeed(signer.reader, signer.config.Service, signer.config.Account, signer.config.PromptTimeout)
	if err != nil {
		return nil, err
	}
	defer clear(seed)
	privateKey := ed25519.NewKeyFromSeed(seed)
	defer clear(privateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if !bytes.Equal(publicKey, signer.config.PublicKey) {
		return nil, ErrSignerMismatch
	}
	return ed25519.Sign(privateKey, message), nil
}

func ExportKeychainPublicIdentity(
	reader KeychainReader,
	service string,
	account string,
	promptTimeout time.Duration,
) (ed25519.PublicKey, string, error) {
	if reader == nil || !keychainIdentifier.MatchString(service) ||
		!keychainIdentifier.MatchString(account) || promptTimeout <= 0 || promptTimeout > 5*time.Minute {
		return nil, "", ErrInvalidKeychainConfig
	}
	seed, err := readKeychainSeed(reader, service, account, promptTimeout)
	if err != nil {
		return nil, "", err
	}
	defer clear(seed)
	privateKey := ed25519.NewKeyFromSeed(seed)
	defer clear(privateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return append(ed25519.PublicKey(nil), publicKey...), fmtDigest(publicKey), nil
}

func ProvisionKeychainKey(
	ctx context.Context,
	provisioner KeychainProvisioner,
	service string,
	account string,
	random io.Reader,
) (ed25519.PublicKey, string, error) {
	if ctx == nil || provisioner == nil || !keychainIdentifier.MatchString(service) ||
		!keychainIdentifier.MatchString(account) {
		return nil, "", ErrInvalidKeychainConfig
	}
	if random == nil {
		random = rand.Reader
	}
	publicKey, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		return nil, "", ErrKeychainAccess
	}
	defer clear(privateKey)
	seed := privateKey.Seed()
	defer clear(seed)
	encoded := make([]byte, base64.RawStdEncoding.EncodedLen(len(seed)))
	base64.RawStdEncoding.Encode(encoded, seed)
	defer clear(encoded)
	if err := provisioner.StoreUserPresence(ctx, service, account, encoded); err != nil {
		return nil, "", sanitizeKeychainError(err)
	}
	return append(ed25519.PublicKey(nil), publicKey...), policyFingerprint(publicKey), nil
}

func decodeSeed(encoded []byte) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		decoded := make([]byte, encoding.DecodedLen(len(encoded)))
		length, err := encoding.Decode(decoded, encoded)
		if err == nil && length == ed25519.SeedSize {
			return decoded[:length], nil
		}
		clear(decoded)
	}
	return nil, ErrKeychainAccess
}

func readKeychainSeed(reader KeychainReader, service, account string, promptTimeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()
	encoded, err := reader.ReadUserPresence(ctx, service, account)
	if err != nil || len(encoded) == 0 || len(encoded) > maxKeychainSeed {
		clear(encoded)
		return nil, sanitizeKeychainError(err)
	}
	defer clear(encoded)
	return decodeSeed(bytes.TrimSpace(encoded))
}

func sanitizeKeychainError(err error) error {
	for _, allowed := range []error{
		ErrKeychainDuplicate,
		ErrKeychainInteractionDenied,
		ErrKeychainMissingEntitlement,
		ErrKeychainAccessControl,
		ErrKeychainAccess,
	} {
		if errors.Is(err, allowed) {
			return allowed
		}
	}
	return ErrKeychainAccess
}

const policyApprovalMessageLimit = 4096

func policyFingerprint(publicKey ed25519.PublicKey) string {
	return fmtDigest(publicKey)
}

func fmtDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

var _ Signer = (*KeychainSigner)(nil)
