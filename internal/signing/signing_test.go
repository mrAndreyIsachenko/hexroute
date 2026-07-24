package signing

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	signingNodeID    = metadata.UUID("11111111-1111-4111-8111-111111111111")
	signingRequestID = metadata.UUID("22222222-2222-4222-8222-222222222222")
)

func TestGenerateAndLoadPrivateNodeKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "node.json")
	random := bytes.NewReader(make([]byte, ed25519.SeedSize+16))
	generated, err := GenerateFile(path, signingNodeID, random)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", info.Mode().Perm())
	}
	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat key directory: %v", err)
	}
	if parentInfo.Mode().Perm() != 0o700 {
		t.Fatalf("key directory mode = %o, want 700", parentInfo.Mode().Perm())
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if loaded.NodeID != generated.NodeID || loaded.KeyID != generated.KeyID ||
		!bytes.Equal(loaded.PublicKey(), generated.PublicKey()) {
		t.Fatal("loaded key does not match generated key")
	}
	if _, err := GenerateFile(
		path,
		signingNodeID,
		bytes.NewReader(make([]byte, ed25519.SeedSize+16)),
	); !errors.Is(err, ErrKeyFileExists) {
		t.Fatalf("GenerateFile(existing) error = %v, want %v", err, ErrKeyFileExists)
	}
}

func TestLoadRejectsBroadPermissionsAndSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "node.json")
	if _, err := GenerateFile(
		path,
		signingNodeID,
		bytes.NewReader(make([]byte, ed25519.SeedSize+16)),
	); err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	if _, err := LoadFile(path); !errors.Is(err, ErrInsecureKeyFile) {
		t.Fatalf("LoadFile(insecure) error = %v, want %v", err, ErrInsecureKeyFile)
	}

	link := filepath.Join(t.TempDir(), "node-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatalf("symlink key: %v", err)
	}
	if _, err := LoadFile(link); !errors.Is(err, ErrInsecureKeyFile) {
		t.Fatalf("LoadFile(symlink) error = %v, want %v", err, ErrInsecureKeyFile)
	}
}

func TestSignedEnvelopeVerifiesAndRejectsTamperingReplayAndRevocation(t *testing.T) {
	key := testKey(t)
	now := time.Date(2026, time.July, 23, 19, 0, 0, 0, time.UTC)
	body := []byte("canonical gzip body")
	signed, err := Sign(key, signingRequestID, now, body)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verifier, err := NewVerifier(5*time.Minute, []RegisteredKey{{
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: key.PublicKey(),
		Status:    KeyActive,
	}})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	if err := verifier.Verify(signed, body, now); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := verifier.Verify(signed, body, now); !errors.Is(err, ErrReplay) {
		t.Fatalf("Verify(replay) error = %v, want %v", err, ErrReplay)
	}

	otherRequestID := metadata.UUID("33333333-3333-4333-8333-333333333333")
	tampered, err := Sign(key, otherRequestID, now, body)
	if err != nil {
		t.Fatalf("Sign(tampered) error = %v", err)
	}
	if err := verifier.Verify(tampered, []byte("changed"), now); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("Verify(tampered body) error = %v, want %v", err, ErrInvalidEnvelope)
	}
	if err := verifier.Revoke(key.KeyID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := verifier.Verify(tampered, body, now); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("Verify(revoked) error = %v, want %v", err, ErrRevokedKey)
	}
}

func TestVerifierRejectsInvalidSignatureAndStaleTimestamp(t *testing.T) {
	key := testKey(t)
	now := time.Date(2026, time.July, 23, 19, 0, 0, 0, time.UTC)
	body := []byte("body")
	signed, err := Sign(key, signingRequestID, now, body)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verifier, err := NewVerifier(time.Minute, []RegisteredKey{{
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: key.PublicKey(),
		Status:    KeyActive,
	}})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	invalid := signed
	invalid.Signature = "invalid"
	if err := verifier.Verify(invalid, body, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify(invalid signature) error = %v, want %v", err, ErrInvalidSignature)
	}
	if err := verifier.Verify(signed, body, now.Add(2*time.Minute)); !errors.Is(err, ErrTimestamp) {
		t.Fatalf("Verify(stale timestamp) error = %v, want %v", err, ErrTimestamp)
	}
}

func TestStatelessAuthenticityVerificationDoesNotConsumeRequestID(t *testing.T) {
	key := testKey(t)
	now := time.Date(2026, time.July, 23, 19, 0, 0, 0, time.UTC)
	body := []byte("body")
	signed, err := Sign(key, signingRequestID, now, body)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	registered := RegisteredKey{
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: key.PublicKey(),
		Status:    KeyActive,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := VerifyAuthenticity(signed, body, now, time.Minute, registered); err != nil {
			t.Fatalf("VerifyAuthenticity(attempt %d) error = %v", attempt+1, err)
		}
	}

	registered.Status = KeyRetired
	if err := VerifyAuthenticity(
		signed,
		body,
		now,
		time.Minute,
		registered,
	); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("VerifyAuthenticity(retired) error = %v, want %v", err, ErrRevokedKey)
	}
}

func testKey(t *testing.T) Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.json")
	randomBytes := make([]byte, ed25519.SeedSize+16)
	randomBytes[0] = 7
	key, err := GenerateFile(path, signingNodeID, bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	return key
}
