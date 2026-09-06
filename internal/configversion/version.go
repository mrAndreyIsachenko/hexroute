// Package configversion is the form an ingress configuration takes between the
// operator who signs it and the host that applies it.
//
// The format exists so that a host can decide, on its own and offline, whether
// the bytes it is holding are the ones an operator meant it to run. Nothing
// about where the bytes came from is part of that decision.
package configversion

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	// StatementSchema names this format. A host refuses anything else rather
	// than guessing at a field it does not know.
	StatementSchema = "hexroute.config-version.v1"
	// StatementVersion is the format's own revision, separate from the
	// version label of the configuration being carried.
	StatementVersion = 1
	// MaxContentBytes bounds one configuration. An ingress runtime
	// configuration is a few kilobytes; a hundred times that is already far
	// past anything this delivers, and the bound is what lets a host read an
	// artifact without trusting the length it was told.
	MaxContentBytes = 128 * 1024
	// MaxArtifactBytes bounds the encoded artifact, content included.
	MaxArtifactBytes = policy.MaxCanonicalJSONSize
)

// TargetKind is what a version is addressed to. The three values are the ones
// the configuration ledger accepts, so a version that validates here can be
// recorded without a second vocabulary.
type TargetKind string

const (
	TargetNode   TargetKind = "node"
	TargetGroup  TargetKind = "group"
	TargetGlobal TargetKind = "global"
)

// Target addresses one version. A host verifies against the target it is,
// which is what stops a version meant for another host from being applied
// merely because it verified.
type Target struct {
	Kind TargetKind
	Key  string
}

// Statement is what the operator signs. It carries the digest of the content
// rather than the content, so that the signature covers the content and its
// digest as one thing and cannot be carried over to different bytes.
type Statement struct {
	Schema            string `json:"schema"`
	Version           uint16 `json:"version"`
	TargetKind        string `json:"target_kind"`
	TargetKey         string `json:"target_key"`
	VersionLabel      string `json:"version_label"`
	ContentSHA256     string `json:"content_sha256"`
	ContentBytes      int    `json:"content_bytes"`
	SignerFingerprint string `json:"signer_fingerprint"`
	CreatedAt         string `json:"created_at"`
}

// Artifact is one published version: the signed statement and the bytes it
// describes, together. They travel as one object because a host that fetched
// them separately could be handed a matched pair of the wrong two halves.
type Artifact struct {
	Statement Statement `json:"statement"`
	Signature string    `json:"signature"`
	Content   string    `json:"content"`
}

// Signer is the operator key. It is the method set of the policy Keychain
// signer, deliberately and not by coincidence: signing a configuration a host
// will run is the same authority as signing a policy generation, and this
// package holds no key of its own to weaken it with.
type Signer interface {
	PublicKey() (ed25519.PublicKey, error)
	Sign(message []byte) ([]byte, error)
}

var (
	// ErrMalformed is a version that is not this format at all.
	ErrMalformed = errors.New("malformed configuration version")
	// ErrNotSigned is a publish that produced no signature. It is returned
	// instead of an unsigned version so that want of the key is a refusal
	// and never a fallback.
	ErrNotSigned = errors.New("configuration version was not signed")
	// ErrWrongTarget is a version addressed to something else.
	ErrWrongTarget = errors.New("configuration version is for another target")
	// ErrDigestMismatch is content that is not what the statement describes.
	ErrDigestMismatch = errors.New("configuration content does not match its signed digest")
	// ErrWrongKey is a signature by a key the host was not built to trust.
	ErrWrongKey = errors.New("configuration version was signed by an unpinned key")
	// ErrSignature is a signature that does not verify.
	ErrSignature = errors.New("invalid configuration version signature")
)

// versionLabel is deliberately narrow: a label names an object and appears in
// a ledger row, and neither should have to quote it.
var versionLabel = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// targetKey is the same shape a node or group key takes elsewhere.
var targetKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,191}$`)

// Publish signs content for one target.
//
// The signature is verified against the signer's own public key before the
// artifact is returned. A signer that produced something that does not verify
// is a failure to sign, not a version to publish.
func Publish(
	signer Signer,
	target Target,
	label string,
	content []byte,
	createdAt time.Time,
) (Artifact, error) {
	if signer == nil {
		return Artifact{}, fmt.Errorf("%w: no signer", ErrNotSigned)
	}
	if err := target.validate(); err != nil {
		return Artifact{}, err
	}
	if !versionLabel.MatchString(label) {
		return Artifact{}, fmt.Errorf("%w: version label", ErrMalformed)
	}
	if len(content) == 0 || len(content) > MaxContentBytes {
		return Artifact{}, fmt.Errorf("%w: %d content bytes", ErrMalformed, len(content))
	}
	if createdAt.IsZero() {
		return Artifact{}, fmt.Errorf("%w: no creation time", ErrMalformed)
	}

	publicKey, err := signer.PublicKey()
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: %w", ErrNotSigned, err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return Artifact{}, fmt.Errorf("%w: no operator public key", ErrNotSigned)
	}

	statement := Statement{
		Schema:            StatementSchema,
		Version:           StatementVersion,
		TargetKind:        string(target.Kind),
		TargetKey:         target.Key,
		VersionLabel:      label,
		ContentSHA256:     policy.SHA256Hex(content),
		ContentBytes:      len(content),
		SignerFingerprint: policy.SHA256Hex(publicKey),
		CreatedAt:         createdAt.UTC().Format(time.RFC3339Nano),
	}
	canonical, err := canonicalStatement(statement)
	if err != nil {
		return Artifact{}, err
	}
	signature, err := signer.Sign(canonical)
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: %w", ErrNotSigned, err)
	}
	if len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(publicKey, canonical, signature) {
		return Artifact{}, fmt.Errorf("%w: signature did not verify", ErrNotSigned)
	}
	artifact := Artifact{
		Statement: statement,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
		Content:   base64.RawURLEncoding.EncodeToString(content),
	}
	if artifact.validateStructure() != nil {
		return Artifact{}, ErrMalformed
	}
	return artifact, nil
}

// Verify decides whether an artifact may be applied, and returns the content
// only when it may.
//
// The checks run in a fixed order and each has its own error, because a host
// that refuses a version has to record which check failed. "It did not verify"
// is not a diagnosis anyone can act on.
func Verify(
	artifact Artifact,
	pinnedPublicKey ed25519.PublicKey,
	expected Target,
) ([]byte, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	if len(pinnedPublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: no pinned public key", ErrWrongKey)
	}
	if err := artifact.validateStructure(); err != nil {
		return nil, err
	}
	statement := artifact.Statement
	if statement.TargetKind != string(expected.Kind) || statement.TargetKey != expected.Key {
		return nil, ErrWrongTarget
	}
	content, err := base64.RawURLEncoding.DecodeString(artifact.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: content encoding", ErrMalformed)
	}
	if len(content) != statement.ContentBytes ||
		policy.SHA256Hex(content) != statement.ContentSHA256 {
		return nil, ErrDigestMismatch
	}
	if statement.SignerFingerprint != policy.SHA256Hex(pinnedPublicKey) {
		return nil, ErrWrongKey
	}
	canonical, err := canonicalStatement(statement)
	if err != nil {
		return nil, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(artifact.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(pinnedPublicKey, canonical, signature) {
		return nil, ErrSignature
	}
	return content, nil
}

// Reason names the check a refusal failed, in a form fit for a record. It is
// closed over the errors this package returns: an unrecognised error is
// reported as unverified rather than as some check that did pass.
func Reason(err error) string {
	switch {
	case err == nil:
		return "verified"
	case errors.Is(err, ErrDigestMismatch):
		return "digest_mismatch"
	case errors.Is(err, ErrWrongKey):
		return "unpinned_key"
	case errors.Is(err, ErrSignature):
		return "invalid_signature"
	case errors.Is(err, ErrWrongTarget):
		return "wrong_target"
	case errors.Is(err, ErrMalformed):
		return "malformed"
	case errors.Is(err, ErrNotSigned):
		return "not_signed"
	default:
		return "unverified"
	}
}

// Encode renders an artifact as the canonical bytes that are stored and
// fetched. Decode accepts only those exact bytes back.
func Encode(artifact Artifact) ([]byte, error) {
	if err := artifact.validateStructure(); err != nil {
		return nil, err
	}
	canonical, err := policy.MarshalCanonical(artifact)
	if err != nil || len(canonical) > MaxArtifactBytes {
		return nil, ErrMalformed
	}
	return canonical, nil
}

// Decode reads a stored artifact.
//
// It requires the encoding to be canonical and to carry no unknown field, so
// that an artifact cannot be re-encoded on the way to a host with something
// added beside the signed statement.
func Decode(encoded []byte) (Artifact, error) {
	if len(encoded) == 0 || len(encoded) > MaxArtifactBytes {
		return Artifact{}, fmt.Errorf("%w: %d artifact bytes", ErrMalformed, len(encoded))
	}
	var artifact Artifact
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, fmt.Errorf("%w: %s", ErrMalformed, "artifact encoding")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Artifact{}, fmt.Errorf("%w: trailing content", ErrMalformed)
	}
	canonical, err := policy.MarshalCanonical(artifact)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return Artifact{}, fmt.Errorf("%w: not canonical", ErrMalformed)
	}
	if err := artifact.validateStructure(); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func (target Target) validate() error {
	switch target.Kind {
	case TargetNode, TargetGroup, TargetGlobal:
	default:
		return fmt.Errorf("%w: target kind", ErrMalformed)
	}
	if !targetKey.MatchString(target.Key) {
		return fmt.Errorf("%w: target key", ErrMalformed)
	}
	return nil
}

func (artifact Artifact) validateStructure() error {
	statement := artifact.Statement
	if statement.Schema != StatementSchema || statement.Version != StatementVersion {
		return fmt.Errorf("%w: schema", ErrMalformed)
	}
	target := Target{Kind: TargetKind(statement.TargetKind), Key: statement.TargetKey}
	if err := target.validate(); err != nil {
		return err
	}
	if !versionLabel.MatchString(statement.VersionLabel) ||
		!validDigest(statement.ContentSHA256) ||
		!validDigest(statement.SignerFingerprint) ||
		statement.ContentBytes <= 0 || statement.ContentBytes > MaxContentBytes {
		return fmt.Errorf("%w: statement fields", ErrMalformed)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, statement.CreatedAt)
	if err != nil || createdAt.Location() != time.UTC ||
		createdAt.Format(time.RFC3339Nano) != statement.CreatedAt {
		return fmt.Errorf("%w: creation time", ErrMalformed)
	}
	signature, err := base64.RawURLEncoding.DecodeString(artifact.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature encoding", ErrMalformed)
	}
	content, err := base64.RawURLEncoding.DecodeString(artifact.Content)
	if err != nil || len(content) == 0 || len(content) > MaxContentBytes {
		return fmt.Errorf("%w: content encoding", ErrMalformed)
	}
	return nil
}

func canonicalStatement(statement Statement) ([]byte, error) {
	canonical, err := policy.MarshalCanonical(statement)
	if err != nil {
		return nil, ErrMalformed
	}
	return canonical, nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
