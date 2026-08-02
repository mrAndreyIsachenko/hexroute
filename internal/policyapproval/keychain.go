package policyapproval

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os/exec"
	"regexp"
	"time"
)

const (
	securityCommand = "/usr/bin/security"
	maxKeychainSeed = 256
)

var keychainIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)

type KeychainRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type KeychainConfig struct {
	Service             string
	Account             string
	PublicKey           ed25519.PublicKey
	RequireUserPresence bool
	PromptTimeout       time.Duration
}

type KeychainSigner struct {
	runner KeychainRunner
	config KeychainConfig
}

var (
	ErrInvalidKeychainConfig = errors.New("invalid policy Keychain signer configuration")
	ErrKeychainAccess        = errors.New("policy Keychain signing key unavailable")
)

func NewKeychainSigner(runner KeychainRunner, config KeychainConfig) (*KeychainSigner, error) {
	if runner == nil || !keychainIdentifier.MatchString(config.Service) ||
		!keychainIdentifier.MatchString(config.Account) || len(config.PublicKey) != ed25519.PublicKeySize ||
		!config.RequireUserPresence || config.PromptTimeout <= 0 || config.PromptTimeout > 5*time.Minute {
		return nil, ErrInvalidKeychainConfig
	}
	config.PublicKey = append(ed25519.PublicKey(nil), config.PublicKey...)
	return &KeychainSigner{runner: runner, config: config}, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), signer.config.PromptTimeout)
	defer cancel()
	encoded, err := signer.runner.Output(
		ctx, securityCommand, "find-generic-password",
		"-s", signer.config.Service, "-a", signer.config.Account, "-w",
	)
	if err != nil || len(encoded) == 0 || len(encoded) > maxKeychainSeed {
		clear(encoded)
		return nil, ErrKeychainAccess
	}
	defer clear(encoded)
	encoded = bytes.TrimSpace(encoded)
	seed, err := decodeSeed(encoded)
	if err != nil {
		return nil, ErrKeychainAccess
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

type ExecKeychainRunner struct{}

func (ExecKeychainRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name != securityCommand {
		return nil, ErrKeychainAccess
	}
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		clear(output)
		return nil, ErrKeychainAccess
	}
	return output, nil
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

const policyApprovalMessageLimit = 4096

var _ Signer = (*KeychainSigner)(nil)
