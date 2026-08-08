package policyinstaller

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyconfig"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
	"github.com/mrAndreyIsachenko/hexroute/internal/replay"
)

func TestInstallCandidateVerifiesAndWritesImmutableDomainArtifactsIdempotently(t *testing.T) {
	fixture := newInstallerFixture(t)
	store := &memoryStore{domain: policy.DomainRoot, artifacts: make(map[policystore.ArtifactKind][]byte)}
	if err := installCandidate(store, fixture.rootRuntime, fixture.bundle, fixture.now); err != nil {
		t.Fatalf("installCandidate() error: %v", err)
	}
	if len(store.artifacts) != 4 {
		t.Fatalf("installed artifacts=%d", len(store.artifacts))
	}
	if err := installCandidate(store, fixture.rootRuntime, fixture.bundle, fixture.now); err != nil {
		t.Fatalf("idempotent installCandidate() error: %v", err)
	}
	tampered := fixture.bundle
	tampered.Artifacts = cloneArtifacts(tampered.Artifacts)
	tampered.Artifacts[policystore.ArtifactManifest] = append(
		append([]byte(nil), tampered.Artifacts[policystore.ArtifactManifest]...),
		' ',
	)
	if err := installCandidate(store, fixture.rootRuntime, tampered, fixture.now); !errors.Is(err, errInstallFailed) {
		t.Fatalf("tampered existing install error=%v", err)
	}
}

func TestInstallCandidateRejectsWrongTrustAndCompatibilityBeforeStoreWrite(t *testing.T) {
	fixture := newInstallerFixture(t)
	for _, mutate := range []func(*policyconfig.RuntimeConfig){
		func(config *policyconfig.RuntimeConfig) {
			config.PinnedPublicKey = append(ed25519.PublicKey(nil), config.PinnedPublicKey...)
			config.PinnedPublicKey[0] ^= 0xff
		},
		func(config *policyconfig.RuntimeConfig) {
			config.Installed.TrustedCompilerSHA256 = []string{strings.Repeat("f", 64)}
		},
	} {
		runtime := cloneRuntime(fixture.rootRuntime)
		mutate(&runtime)
		store := &memoryStore{domain: policy.DomainRoot, artifacts: make(map[policystore.ArtifactKind][]byte)}
		if err := installCandidate(store, runtime, fixture.bundle, fixture.now); err == nil {
			t.Fatal("installCandidate() accepted invalid trust boundary")
		}
		if len(store.artifacts) != 0 {
			t.Fatalf("invalid candidate wrote %d artifacts", len(store.artifacts))
		}
	}
}

func TestReadPrivateDirectoryRejectsExtraSymlinkAndLooseMode(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	writePrivateFile(t, filepath.Join(private, "manifest.json"), []byte("{}"))
	owner := uint32(os.Geteuid())
	if _, err := readPrivateDirectory(private, []string{"manifest.json"}, owner); err != nil {
		t.Fatalf("readPrivateDirectory() error: %v", err)
	}
	writePrivateFile(t, filepath.Join(private, "extra.json"), []byte("{}"))
	if _, err := readPrivateDirectory(private, []string{"manifest.json"}, owner); err == nil {
		t.Fatal("readPrivateDirectory() accepted unexpected file")
	}
	if err := os.Remove(filepath.Join(private, "extra.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(private, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(private, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateDirectory(private, []string{"manifest.json"}, owner); err == nil {
		t.Fatal("readPrivateDirectory() accepted symlink")
	}
	if err := os.Remove(filepath.Join(private, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	writePrivateFile(t, filepath.Join(private, "manifest.json"), []byte("{}"))
	if err := os.Chmod(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateDirectory(private, []string{"manifest.json"}, owner); err == nil {
		t.Fatal("readPrivateDirectory() accepted loose directory mode")
	}
}

func TestReadInstalledPolicyConfigUsesSecureDescriptorMetadata(t *testing.T) {
	fixture := newInstallerFixture(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "root-observe.json")
	static := policyconfig.StaticConfig{
		Schema:            policyconfig.StaticConfigSchema,
		Installed:         fixture.rootRuntime.Installed,
		PinnedPublicKey:   base64.RawStdEncoding.EncodeToString(fixture.rootRuntime.PinnedPublicKey),
		SignerFingerprint: policy.SHA256Hex(fixture.rootRuntime.PinnedPublicKey),
	}
	encoded, err := json.Marshal(observerConfig{Schema: rootConfig, PolicyControl: &static})
	if err != nil {
		t.Fatal(err)
	}
	writePrivateFile(t, configPath, encoded)
	runtime, err := readInstalledPolicyConfig(
		configPath,
		rootConfig,
		uint32(os.Geteuid()),
		policy.DomainRoot,
	)
	if err != nil || runtime.Validate() != nil {
		t.Fatalf("readInstalledPolicyConfig() runtime=%+v err=%v", runtime, err)
	}

	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstalledPolicyConfig(configPath, rootConfig, uint32(os.Geteuid()), policy.DomainRoot); err == nil {
		t.Fatal("readInstalledPolicyConfig() accepted loose mode")
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(root, "hardlink.json")
	if err := os.Link(configPath, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstalledPolicyConfig(configPath, rootConfig, uint32(os.Geteuid()), policy.DomainRoot); err == nil {
		t.Fatal("readInstalledPolicyConfig() accepted multiple links")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing.json"), configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstalledPolicyConfig(configPath, rootConfig, uint32(os.Geteuid()), policy.DomainRoot); err == nil {
		t.Fatal("readInstalledPolicyConfig() accepted symlink")
	}
}

func TestRunRejectsUnknownAndInvalidDomain(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"init", "--domain", "cloud"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatalf("Run(%v) code=%d stdout=%q", args, code, stdout.String())
		}
	}
	if code := Run([]string{"--check"}, ioDiscard{}, ioDiscard{}); code != 0 {
		t.Fatalf("--check code=%d", code)
	}
}

type installerFixture struct {
	now         time.Time
	bundle      signedBundle
	rootRuntime policyconfig.RuntimeConfig
}

func newInstallerFixture(t *testing.T) installerFixture {
	t.Helper()
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := policy.DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	compilerDigest := strings.Repeat("b", 64)
	source := policy.OperatorSource{
		Schema: policy.OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 1, ParentBundleGeneration: 0,
		StaticSHA256: staticDigest,
		IssuedAt:     "2026-08-09T00:00:00Z", NotBefore: "2026-08-09T00:00:00Z", ExpiresAt: "2026-08-10T00:00:00Z",
		Root: policy.DomainSource{PolicyGeneration: 1, Rules: []policy.Rule{denyRule("root", "routes")}},
		User: policy.DomainSource{PolicyGeneration: 1, Rules: []policy.Rule{denyRule("user", "pritunl")}},
	}
	candidate, err := policy.CompileBundle(
		source,
		envelope,
		policy.CompilerIdentity{Version: "v0.1.0", SHA256: compilerDigest},
		policy.SHA256Hex(publicKey),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := policy.BuildSemanticDiff(nil, candidate.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	replayReport, err := replay.EvaluatePolicy(candidate.Snapshot, []replay.PolicyCase{
		{Schema: replay.PolicyCaseSchema, Kind: replay.CaseSyntheticInvariant, ID: "root-deny", Domain: policy.DomainRoot, Capability: policy.CapabilityOperatorResume, Target: "routes", Expected: replay.DecisionDeny},
		{Schema: replay.PolicyCaseSchema, Kind: replay.CaseSyntheticInvariant, ID: "user-deny", Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume, Target: "pritunl", Expected: replay.DecisionDeny},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootInstalled := initialCompatibility(policy.DomainRoot, staticDigest, compilerDigest)
	userInstalled := initialCompatibility(policy.DomainUser, staticDigest, compilerDigest)
	review, approval, err := policyapproval.ApproveCandidate(
		candidate,
		nil,
		diff,
		replayReport,
		policyapproval.InstalledDomains{Root: rootInstalled, User: userInstalled},
		testSigner{publicKey: publicKey, privateKey: privateKey},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, reviewJSON, err := policy.CanonicalSHA256(review)
	if err != nil {
		t.Fatal(err)
	}
	_, approvalJSON, err := policy.CanonicalSHA256(approval)
	if err != nil {
		t.Fatal(err)
	}
	return installerFixture{
		now: now,
		bundle: signedBundle{
			Candidate: candidate, Review: review, Approval: approval,
			Artifacts: map[policystore.ArtifactKind][]byte{
				policystore.ArtifactManifest: candidate.ManifestJSON,
				policystore.ArtifactPayload:  candidate.RootJSON,
				policystore.ArtifactReview:   reviewJSON,
				policystore.ArtifactApproval: approvalJSON,
			},
		},
		rootRuntime: policyconfig.RuntimeConfig{Installed: rootInstalled, PinnedPublicKey: publicKey},
	}
}

func initialCompatibility(domain policy.Domain, staticDigest, compilerDigest string) policy.InstalledCompatibility {
	return policy.InstalledCompatibility{
		Domain: domain, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
		CurrentPolicySchema: 1, StaticSHA256: staticDigest,
		TrustedCompilerSHA256: []string{compilerDigest},
	}
}

func denyRule(namespace, target string) policy.Rule {
	return policy.Rule{
		ID:     namespace + ".deny-" + target,
		Effect: policy.EffectDeny,
		Selector: policy.Selector{
			ID: namespace + ".resume-" + target, Kind: policy.SelectorAction,
			Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: target},
		},
	}
}

type testSigner struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func (signer testSigner) PublicKey() (ed25519.PublicKey, error) {
	return append(ed25519.PublicKey(nil), signer.publicKey...), nil
}

func (signer testSigner) Sign(message []byte) ([]byte, error) {
	return ed25519.Sign(signer.privateKey, message), nil
}

type memoryStore struct {
	domain    policy.Domain
	artifacts map[policystore.ArtifactKind][]byte
}

func (store *memoryStore) Domain() policy.Domain { return store.domain }
func (store *memoryStore) Close() error          { return nil }
func (store *memoryStore) InstallArtifact(_ policystore.Generation, kind policystore.ArtifactKind, content []byte) error {
	if _, exists := store.artifacts[kind]; exists {
		return policystore.ErrGenerationExists
	}
	store.artifacts[kind] = append([]byte(nil), content...)
	return nil
}
func (store *memoryStore) ReadArtifact(_ policystore.Generation, kind policystore.ArtifactKind) ([]byte, error) {
	content, exists := store.artifacts[kind]
	if !exists {
		return nil, policystore.ErrGenerationNotFound
	}
	return append([]byte(nil), content...), nil
}

func cloneArtifacts(input map[policystore.ArtifactKind][]byte) map[policystore.ArtifactKind][]byte {
	output := make(map[policystore.ArtifactKind][]byte, len(input))
	for kind, content := range input {
		output[kind] = append([]byte(nil), content...)
	}
	return output
}

func cloneRuntime(input policyconfig.RuntimeConfig) policyconfig.RuntimeConfig {
	output := input
	output.Installed.TrustedCompilerSHA256 = append(
		[]string(nil),
		input.Installed.TrustedCompilerSHA256...,
	)
	output.PinnedPublicKey = append(ed25519.PublicKey(nil), input.PinnedPublicKey...)
	return output
}

func writePrivateFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(content []byte) (int, error) { return len(content), nil }
