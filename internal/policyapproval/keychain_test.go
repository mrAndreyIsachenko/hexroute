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
	input  []byte
}

func (runner *keychainRunnerStub) StoreUserPresence(
	_ context.Context,
	service string,
	account string,
	input []byte,
) error {
	runner.name = service
	runner.args = []string{account}
	runner.input = append([]byte(nil), input...)
	return runner.err
}

func (runner *keychainRunnerStub) ReadUserPresence(_ context.Context, service string, account string) ([]byte, error) {
	runner.name = service
	runner.args = []string{account}
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
	if runner.name != "hexroute-policy-signing" || joined != "operator" ||
		strings.Contains(joined, base64.RawStdEncoding.EncodeToString(seed)) {
		t.Fatalf("unsafe Keychain lookup: %s %s", runner.name, joined)
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

func TestProvisionKeychainKeyUsesUserPresenceStoreWithoutArguments(t *testing.T) {
	runner := &keychainRunnerStub{}
	publicKey, fingerprint, err := ProvisionKeychainKey(
		context.Background(), runner, "hexroute-policy-signing", "operator",
		bytes.NewReader(bytes.Repeat([]byte{6}, ed25519.SeedSize)),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(runner.input)))
	if err != nil || len(decoded) != ed25519.SeedSize {
		t.Fatal("provisioner did not receive an Ed25519 seed through its private input")
	}
	privateKey := ed25519.NewKeyFromSeed(decoded)
	if !bytes.Equal(publicKey, privateKey.Public().(ed25519.PublicKey)) || fingerprint != fmtDigest(publicKey) {
		t.Fatal("provisioned public identity does not match generated seed")
	}
	if runner.name != "hexroute-policy-signing" || len(runner.args) != 1 || runner.args[0] != "operator" ||
		strings.Contains(strings.Join(runner.args, " "), strings.TrimSpace(string(runner.input))) {
		t.Fatal("seed escaped the user-presence Keychain store input")
	}
}

func TestExportKeychainPublicIdentityDerivesOnlyPublicMetadata(t *testing.T) {
	seed := bytes.Repeat([]byte{5}, ed25519.SeedSize)
	runner := &keychainRunnerStub{output: []byte(base64.RawStdEncoding.EncodeToString(seed))}
	publicKey, fingerprint, err := ExportKeychainPublicIdentity(
		runner, "hexroute-policy-signing", "operator", time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !bytes.Equal(publicKey, expected) || fingerprint != fmtDigest(expected) {
		t.Fatal("exported public identity does not match Keychain seed")
	}
}
