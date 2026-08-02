package policyapproval

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

type keychainRunnerStub struct {
	output []byte
	err    error
	name   string
	args   []string
}

func (runner *keychainRunnerStub) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.name = name
	runner.args = append([]string(nil), args...)
	return append([]byte(nil), runner.output...), runner.err
}

func TestKeychainSignerUsesProtectedLookupAndMatchesPinnedKey(t *testing.T) {
	seed := bytes.Repeat([]byte{4}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	runner := &keychainRunnerStub{output: []byte(base64.RawStdEncoding.EncodeToString(seed) + "\n")}
	signer, err := NewKeychainSigner(runner, KeychainConfig{
		Service: "hexroute-policy-signing", Account: "operator",
		PublicKey: publicKey, RequireUserPresence: true, PromptTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("canonical approval statement")
	signature, err := signer.Sign(message)
	if err != nil || !ed25519.Verify(publicKey, message, signature) {
		t.Fatalf("keychain signature: %v", err)
	}
	joined := strings.Join(runner.args, " ")
	if runner.name != securityCommand || joined != "find-generic-password -s hexroute-policy-signing -a operator -w" ||
		strings.Contains(joined, base64.RawStdEncoding.EncodeToString(seed)) {
		t.Fatalf("unsafe Keychain command: %s %s", runner.name, joined)
	}
}

func TestKeychainSignerErrorsNeverExposeSeed(t *testing.T) {
	secret := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{8}, ed25519.SeedSize))
	runner := &keychainRunnerStub{output: []byte(secret), err: errors.New("private failure " + secret)}
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	signer, err := NewKeychainSigner(runner, KeychainConfig{
		Service: "hexroute-policy-signing", Account: "operator",
		PublicKey: publicKey, RequireUserPresence: true, PromptTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = signer.Sign([]byte("message"))
	if !errors.Is(err, ErrKeychainAccess) || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret-bearing error escaped: %v", err)
	}
}
