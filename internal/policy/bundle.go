package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const MaxBundleArtifactSize = MaxCanonicalJSONSize

type CompilerIdentity struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type CandidateBundle struct {
	Manifest       Manifest
	Root           DomainPayload
	User           DomainPayload
	Snapshot       EffectiveSnapshot
	ManifestJSON   []byte
	RootJSON       []byte
	UserJSON       []byte
	ManifestSHA256 string
}

var (
	ErrInvalidCompilerIdentity = errors.New("invalid policy compiler identity")
	ErrInvalidCandidateBundle  = errors.New("invalid policy candidate bundle")
)

func (identity CompilerIdentity) Validate() error {
	if !validVersion(identity.Version) || !validSHA256(identity.SHA256) {
		return ErrInvalidCompilerIdentity
	}
	return nil
}

func CompileBundle(
	source OperatorSource,
	envelope SafetyEnvelope,
	identity CompilerIdentity,
	signerFingerprint string,
	current *EffectiveSnapshot,
) (CandidateBundle, error) {
	if identity.Validate() != nil || !validSHA256(signerFingerprint) {
		return CandidateBundle{}, ErrInvalidCompilerIdentity
	}
	snapshot, err := ComposeEffectiveSnapshot(source, envelope)
	if err != nil {
		return CandidateBundle{}, err
	}
	if current != nil {
		if err := ValidateSemanticAdvance(*current, snapshot); err != nil {
			return CandidateBundle{}, err
		}
	}

	rootDigest, rootJSON, err := CanonicalSHA256(snapshot.Root)
	if err != nil {
		return CandidateBundle{}, ErrInvalidCandidateBundle
	}
	userDigest, userJSON, err := CanonicalSHA256(snapshot.User)
	if err != nil {
		return CandidateBundle{}, ErrInvalidCandidateBundle
	}
	manifest := Manifest{
		Schema:                 ManifestSchema,
		PolicySchema:           snapshot.PolicySchema,
		CompilerVersion:        identity.Version,
		CompilerSHA256:         identity.SHA256,
		BundleGeneration:       snapshot.BundleGeneration,
		ParentBundleGeneration: snapshot.ParentBundleGeneration,
		Root: DomainReference{
			Generation: snapshot.Root.PolicyGeneration, PayloadSHA256: rootDigest,
		},
		User: DomainReference{
			Generation: snapshot.User.PolicyGeneration, PayloadSHA256: userDigest,
		},
		StaticSHA256:      snapshot.StaticSHA256,
		SignerFingerprint: signerFingerprint,
		IssuedAt:          snapshot.IssuedAt,
		NotBefore:         snapshot.NotBefore,
		ExpiresAt:         snapshot.ExpiresAt,
	}
	manifestDigest, manifestJSON, err := CanonicalSHA256(manifest)
	if err != nil {
		return CandidateBundle{}, ErrInvalidCandidateBundle
	}
	candidate := CandidateBundle{
		Manifest: manifest, Root: snapshot.Root, User: snapshot.User, Snapshot: snapshot,
		ManifestJSON: manifestJSON, RootJSON: rootJSON, UserJSON: userJSON,
		ManifestSHA256: manifestDigest,
	}
	if err := candidate.Validate(); err != nil {
		return CandidateBundle{}, err
	}
	return candidate, nil
}

func (candidate CandidateBundle) Validate() error {
	if candidate.Manifest.Validate() != nil || candidate.Snapshot.Validate() != nil ||
		candidate.Root.Validate() != nil || candidate.User.Validate() != nil ||
		candidate.Root.Domain != DomainRoot || candidate.User.Domain != DomainUser ||
		candidate.Manifest.BundleGeneration != candidate.Snapshot.BundleGeneration ||
		candidate.Manifest.ParentBundleGeneration != candidate.Snapshot.ParentBundleGeneration ||
		candidate.Manifest.PolicySchema != candidate.Snapshot.PolicySchema ||
		candidate.Manifest.StaticSHA256 != candidate.Snapshot.StaticSHA256 ||
		candidate.Manifest.IssuedAt != candidate.Snapshot.IssuedAt ||
		candidate.Manifest.NotBefore != candidate.Snapshot.NotBefore ||
		candidate.Manifest.ExpiresAt != candidate.Snapshot.ExpiresAt {
		return ErrInvalidCandidateBundle
	}
	rootDigest, rootJSON, err := CanonicalSHA256(candidate.Root)
	if err != nil || rootDigest != candidate.Manifest.Root.PayloadSHA256 ||
		candidate.Manifest.Root.Generation != candidate.Root.PolicyGeneration ||
		!bytesEqual(rootJSON, candidate.RootJSON) {
		return ErrInvalidCandidateBundle
	}
	snapshotRootDigest, _, err := CanonicalSHA256(candidate.Snapshot.Root)
	if err != nil || snapshotRootDigest != rootDigest {
		return ErrInvalidCandidateBundle
	}
	userDigest, userJSON, err := CanonicalSHA256(candidate.User)
	if err != nil || userDigest != candidate.Manifest.User.PayloadSHA256 ||
		candidate.Manifest.User.Generation != candidate.User.PolicyGeneration ||
		!bytesEqual(userJSON, candidate.UserJSON) {
		return ErrInvalidCandidateBundle
	}
	snapshotUserDigest, _, err := CanonicalSHA256(candidate.Snapshot.User)
	if err != nil || snapshotUserDigest != userDigest {
		return ErrInvalidCandidateBundle
	}
	manifestDigest, manifestJSON, err := CanonicalSHA256(candidate.Manifest)
	if err != nil || manifestDigest != candidate.ManifestSHA256 ||
		!bytesEqual(manifestJSON, candidate.ManifestJSON) {
		return ErrInvalidCandidateBundle
	}
	return nil
}

func DecodeCandidateBundle(manifestJSON, rootJSON, userJSON []byte) (CandidateBundle, error) {
	manifest, manifestDigest, err := DecodeManifestArtifact(manifestJSON)
	if err != nil {
		return CandidateBundle{}, err
	}
	root, _, err := DecodeDomainPayloadArtifact(rootJSON)
	if err != nil {
		return CandidateBundle{}, err
	}
	user, _, err := DecodeDomainPayloadArtifact(userJSON)
	if err != nil {
		return CandidateBundle{}, ErrInvalidCandidateBundle
	}
	snapshot := EffectiveSnapshot{
		Schema: EffectiveSnapshotSchema, PolicySchema: manifest.PolicySchema,
		BundleGeneration: manifest.BundleGeneration, ParentBundleGeneration: manifest.ParentBundleGeneration,
		StaticSHA256: manifest.StaticSHA256, IssuedAt: manifest.IssuedAt,
		NotBefore: manifest.NotBefore, ExpiresAt: manifest.ExpiresAt,
		Root: root, User: user,
	}
	candidate := CandidateBundle{
		Manifest: manifest, Root: root, User: user, Snapshot: snapshot,
		ManifestJSON: manifestJSON, RootJSON: rootJSON, UserJSON: userJSON,
		ManifestSHA256: manifestDigest,
	}
	if candidate.Validate() != nil {
		return CandidateBundle{}, ErrInvalidCandidateBundle
	}
	return candidate, nil
}

func DecodeManifestArtifact(encoded []byte) (Manifest, string, error) {
	var manifest Manifest
	digest, err := decodeCanonicalArtifact(encoded, &manifest, func() error {
		return manifest.Validate()
	})
	if err != nil {
		return Manifest{}, "", ErrInvalidCandidateBundle
	}
	return manifest, digest, nil
}

func DecodeDomainPayloadArtifact(encoded []byte) (DomainPayload, string, error) {
	var payload DomainPayload
	digest, err := decodeCanonicalArtifact(encoded, &payload, func() error {
		return payload.Validate()
	})
	if err != nil {
		return DomainPayload{}, "", ErrInvalidCandidateBundle
	}
	return payload, digest, nil
}

func decodeCanonicalArtifact(encoded []byte, destination any, validate func() error) (string, error) {
	if len(encoded) == 0 || len(encoded) > MaxBundleArtifactSize ||
		strictJSONDecode(encoded, destination) != nil || validate() != nil {
		return "", ErrInvalidCandidateBundle
	}
	digest, canonical, err := CanonicalSHA256(destination)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return "", ErrInvalidCandidateBundle
	}
	return digest, nil
}

func strictJSONDecode(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidCandidateBundle
	}
	return nil
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
