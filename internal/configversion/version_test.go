package configversion

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

// The operator key is the policy signing key. These tests sign through the
// policy Keychain signer rather than through a local ed25519 key, so that a
// change which gave this package a key of its own would fail here.
type keychainStub struct {
	seed []byte
	err  error
	read int
}

func (stub *keychainStub) ReadUserPresence(context.Context, string, string) ([]byte, error) {
	stub.read++
	if stub.err != nil {
		return nil, stub.err
	}
	return []byte(base64.RawStdEncoding.EncodeToString(stub.seed)), nil
}

func operatorSigner(t *testing.T, stub *keychainStub) (*policyapproval.KeychainSigner, ed25519.PublicKey) {
	t.Helper()
	publicKey := ed25519.NewKeyFromSeed(stub.seed).Public().(ed25519.PublicKey)
	signer, err := policyapproval.NewKeychainSigner(stub, policyapproval.KeychainConfig{
		Service:             "hexroute-policy-signing",
		Account:             "operator",
		PublicKey:           publicKey,
		RequireUserPresence: true,
		PromptTimeout:       time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return signer, publicKey
}

func testTarget() Target {
	return Target{Kind: TargetNode, Key: "ingress-provider-b"}
}

func publishForTest(t *testing.T) (Artifact, ed25519.PublicKey, []byte) {
	t.Helper()
	stub := &keychainStub{seed: bytes.Repeat([]byte{7}, ed25519.SeedSize)}
	signer, publicKey := operatorSigner(t, stub)
	content := []byte(`{"inbounds":[{"port":443}]}`)
	artifact, err := Publish(signer, testTarget(), "2026-09-06.1", content,
		time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if stub.read != 1 {
		t.Fatalf("operator key was read %d times", stub.read)
	}
	return artifact, publicKey, content
}

func TestPublishedVersionVerifiesAgainstThePinnedOperatorKey(t *testing.T) {
	artifact, publicKey, content := publishForTest(t)
	verified, err := Verify(artifact, publicKey, testTarget())
	if err != nil || !bytes.Equal(verified, content) {
		t.Fatalf("verify: %v", err)
	}
	if artifact.Statement.SignerFingerprint != policy.SHA256Hex(publicKey) ||
		artifact.Statement.ContentSHA256 != policy.SHA256Hex(content) {
		t.Fatal("statement does not name the key and the content it covers")
	}
	if Reason(err) != "verified" {
		t.Fatalf("reason for success: %s", Reason(err))
	}
}

// A signature that could be lifted onto other bytes would make the format
// worthless: the host would be verifying that some configuration was signed,
// not that this one was.
func TestASignatureCannotBeMovedOntoDifferentBytes(t *testing.T) {
	artifact, publicKey, _ := publishForTest(t)
	other := []byte(`{"inbounds":[{"port":8080}]}`)

	swapped := artifact
	swapped.Content = base64.RawURLEncoding.EncodeToString(other)
	if _, err := Verify(swapped, publicKey, testTarget()); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("content swapped under the signature: %v", err)
	}

	// Repairing the digest to match the substituted bytes leaves the
	// signature covering a statement nobody signed.
	repaired := swapped
	repaired.Statement.ContentSHA256 = policy.SHA256Hex(other)
	repaired.Statement.ContentBytes = len(other)
	if _, err := Verify(repaired, publicKey, testTarget()); !errors.Is(err, ErrSignature) {
		t.Fatalf("statement rewritten to match: %v", err)
	}
}

func TestVerificationRefusesEachCheckWithItsOwnReason(t *testing.T) {
	artifact, publicKey, _ := publishForTest(t)
	otherKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).
		Public().(ed25519.PublicKey)

	for _, testCase := range []struct {
		name    string
		mutate  func(Artifact) Artifact
		pinned  ed25519.PublicKey
		target  Target
		wantErr error
		reason  string
	}{
		{
			name:    "signed by a key the host was not built to trust",
			mutate:  func(a Artifact) Artifact { return a },
			pinned:  otherKey,
			target:  testTarget(),
			wantErr: ErrWrongKey,
			reason:  "unpinned_key",
		},
		{
			name:    "addressed to another target",
			mutate:  func(a Artifact) Artifact { return a },
			pinned:  publicKey,
			target:  Target{Kind: TargetNode, Key: "ingress-provider-a"},
			wantErr: ErrWrongTarget,
			reason:  "wrong_target",
		},
		{
			name: "signature replaced",
			mutate: func(a Artifact) Artifact {
				a.Signature = base64.RawURLEncoding.EncodeToString(
					bytes.Repeat([]byte{1}, ed25519.SignatureSize))
				return a
			},
			pinned:  publicKey,
			target:  testTarget(),
			wantErr: ErrSignature,
			reason:  "invalid_signature",
		},
		{
			name: "not this format",
			mutate: func(a Artifact) Artifact {
				a.Statement.Schema = "hexroute.config-version.v2"
				return a
			},
			pinned:  publicKey,
			target:  testTarget(),
			wantErr: ErrMalformed,
			reason:  "malformed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			content, err := Verify(testCase.mutate(artifact), testCase.pinned, testCase.target)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error: %v, want %v", err, testCase.wantErr)
			}
			if content != nil {
				t.Fatal("refused version returned content to apply")
			}
			if Reason(err) != testCase.reason {
				t.Fatalf("reason: %s, want %s", Reason(err), testCase.reason)
			}
		})
	}
}

// The refusal has to be for want of the key. A publisher that fell back to an
// unsigned or self-signed version when the Keychain declined would move the
// signing authority into whatever was running at the time.
func TestPublishRefusesForWantOfTheKeyRatherThanFallingBack(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		stub  *keychainStub
		wants error
	}{
		{
			name:  "user presence denied",
			stub:  &keychainStub{seed: bytes.Repeat([]byte{7}, ed25519.SeedSize), err: policyapproval.ErrKeychainInteractionDenied},
			wants: policyapproval.ErrKeychainInteractionDenied,
		},
		{
			name:  "key absent",
			stub:  &keychainStub{seed: bytes.Repeat([]byte{7}, ed25519.SeedSize), err: policyapproval.ErrKeychainAccess},
			wants: policyapproval.ErrKeychainAccess,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			signer, _ := operatorSigner(t, testCase.stub)
			artifact, err := Publish(signer, testTarget(), "denied.1",
				[]byte(`{"inbounds":[]}`), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC))
			if !errors.Is(err, ErrNotSigned) || !errors.Is(err, testCase.wants) {
				t.Fatalf("error: %v", err)
			}
			if artifact != (Artifact{}) {
				t.Fatal("refusal produced a version")
			}
			if Reason(err) != "not_signed" {
				t.Fatalf("reason: %s", Reason(err))
			}
		})
	}
}

// No daemon, worker or build holds the operator key, so none of them can reach
// Publish with anything that signs. The signer is an interface only so that
// the Keychain can be stubbed in a test; there is no key-less implementation
// in the tree for a component to pick up.
func TestNoComponentCanPublishWithoutTheOperatorKey(t *testing.T) {
	if artifact, err := Publish(nil, testTarget(), "no-signer.1",
		[]byte(`{"inbounds":[]}`), time.Now()); !errors.Is(err, ErrNotSigned) ||
		artifact != (Artifact{}) {
		t.Fatalf("published without a signer: %v", err)
	}

	// A Keychain signer cannot even be constructed without user presence,
	// which is what keeps an unattended component from holding one.
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).
		Public().(ed25519.PublicKey)
	stub := &keychainStub{seed: bytes.Repeat([]byte{7}, ed25519.SeedSize)}
	if _, err := policyapproval.NewKeychainSigner(stub, policyapproval.KeychainConfig{
		Service: "hexroute-policy-signing", Account: "operator",
		PublicKey: publicKey, RequireUserPresence: false, PromptTimeout: time.Minute,
	}); !errors.Is(err, policyapproval.ErrInvalidKeychainConfig) {
		t.Fatalf("signer built without user presence: %v", err)
	}
}

// The signer this package accepts is the policy one. If the policy signing
// path changed shape, this stops compiling rather than quietly diverging.
func TestTheOperatorSignerIsThePolicySigningPath(t *testing.T) {
	var signer Signer = (*policyapproval.KeychainSigner)(nil)
	if signer == nil {
		t.Fatal("policy Keychain signer is not this package's signer")
	}
}

func TestStoredArtifactMustBeExactlyWhatWasPublished(t *testing.T) {
	artifact, publicKey, content := publishForTest(t)
	encoded, err := Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(decoded, publicKey, testTarget())
	if err != nil || !bytes.Equal(verified, content) {
		t.Fatalf("round trip: %v", err)
	}

	for _, testCase := range []struct {
		name    string
		encoded []byte
	}{
		{name: "trailing content", encoded: append(append([]byte(nil), encoded...), '{', '}')},
		{name: "unknown field", encoded: withField(t, encoded, `"applied":true`)},
		{name: "re-encoded away from canonical", encoded: append([]byte(" "), encoded...)},
		{name: "empty", encoded: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Decode(testCase.encoded); !errors.Is(err, ErrMalformed) {
				t.Fatalf("accepted %s: %v", testCase.name, err)
			}
		})
	}
}

func withField(t *testing.T, encoded []byte, field string) []byte {
	t.Helper()
	if !json.Valid(encoded) || !strings.HasPrefix(string(encoded), "{") {
		t.Fatal("artifact is not a JSON object")
	}
	return []byte("{" + field + "," + string(encoded)[1:])
}
