package policycli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"go.yaml.in/yaml/v3"
)

func TestRunExposesOfflineSubcommands(t *testing.T) {
	for _, command := range []string{
		"compile", "diff", "replay", "sign", "rollback",
		"provision-key", "export-public-key", "verify-key",
	} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{command}, &stdout, &stderr); code != 1 {
			t.Fatalf("%s without flags code = %d", command, code)
		}
	}
	if code := Run([]string{"--check"}, ioDiscard{}, ioDiscard{}); code != 0 {
		t.Fatalf("--check code = %d", code)
	}
}

func TestFailureCodeAllowsOnlyBoundedKeychainReasons(t *testing.T) {
	tests := map[error]string{
		policyapproval.ErrKeychainDuplicate:          "keychain_item_exists",
		policyapproval.ErrKeychainInteractionDenied:  "keychain_user_presence_denied",
		policyapproval.ErrKeychainMissingEntitlement: "keychain_entitlement_required",
		policyapproval.ErrKeychainAccessControl:      "keychain_access_control_unavailable",
		policyapproval.ErrKeychainAccess:             "keychain_unavailable",
		errors.New("synthetic private detail"):       "failed",
	}
	for err, expected := range tests {
		if actual := failureCode(err); actual != expected {
			t.Fatalf("failureCode(%v)=%q, want %q", err, actual, expected)
		}
	}
}

func TestCompileWritesOnlyPrivateCanonicalCandidate(t *testing.T) {
	envelope := policy.DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	source := policy.OperatorSource{
		Schema: policy.OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 1, ParentBundleGeneration: 0,
		StaticSHA256: staticDigest,
		IssuedAt:     "2026-08-02T09:00:00Z", NotBefore: "2026-08-02T09:00:00Z", ExpiresAt: "2026-08-02T10:00:00Z",
		Root: policy.DomainSource{PolicyGeneration: 1, Rules: []policy.Rule{{
			ID: "root.synthetic-deny", Effect: policy.EffectDeny,
			Selector: policy.Selector{ID: "root.synthetic-target", Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "routes"}},
		}}},
		User: policy.DomainSource{PolicyGeneration: 1},
	}
	encoded, err := yaml.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.yaml")
	if err := os.WriteFile(sourcePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "candidate")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"compile", "--source", sourcePath, "--out", outPath,
		"--compiler-version", "v0.1.0", "--compiler-sha256", strings.Repeat("b", 64),
		"--signer-fingerprint", strings.Repeat("c", 64),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("compile code=%d stderr=%s", code, stderr.String())
	}
	entries, err := os.ReadDir(outPath)
	if err != nil || len(entries) != 3 {
		t.Fatalf("candidate artifacts: entries=%d err=%v", len(entries), err)
	}
	for _, name := range []string{"manifest.json", "root.json", "user.json"} {
		info, err := os.Stat(filepath.Join(outPath, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v err=%v", name, info.Mode().Perm(), err)
		}
	}
	if _, err := loadBundle(outPath); err != nil {
		t.Fatalf("load compiled bundle: %v", err)
	}
}

func TestRollbackWritesMonotonicCandidateFromHistoricalBundle(t *testing.T) {
	envelope := policy.DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity := policy.CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)}
	signerFingerprint := strings.Repeat("c", 64)
	targetSource := cliRollbackSource(staticDigest, 1, 0, 1, policy.EffectDeny, "routes")
	target, err := policy.CompileBundle(targetSource, envelope, identity, signerFingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSource := cliRollbackSource(staticDigest, 2, 1, 2, policy.EffectDeny, "network")
	current, err := policy.CompileBundle(currentSource, envelope, identity, signerFingerprint, &target.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), "target")
	currentPath := filepath.Join(t.TempDir(), "current")
	if err := writeBundle(targetPath, target); err != nil {
		t.Fatal(err)
	}
	if err := writeBundle(currentPath, current); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "rollback")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"rollback", "--target", targetPath, "--current", currentPath, "--out", outPath,
		"--issued-at", "2026-08-02T09:10:00Z",
		"--not-before", "2026-08-02T09:10:00Z",
		"--expires-at", "2026-08-02T10:00:00Z",
		"--compiler-version", identity.Version, "--compiler-sha256", identity.SHA256,
		"--signer-fingerprint", signerFingerprint,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rollback code=%d stderr=%s", code, stderr.String())
	}
	candidate, err := loadBundle(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Manifest.BundleGeneration != 3 || candidate.Manifest.ParentBundleGeneration != 2 ||
		candidate.Root.PolicyGeneration != 3 {
		t.Fatalf("rollback generations: manifest=%+v root=%+v", candidate.Manifest, candidate.Root)
	}
	if len(candidate.Root.Rules) != 1 || candidate.Root.Rules[0].Selector.Action.Target != "routes" {
		t.Fatalf("rollback did not use historical effective content: %+v", candidate.Root.Rules)
	}
}

func cliRollbackSource(
	staticDigest string,
	bundle uint64,
	parent uint64,
	rootGeneration uint64,
	effect policy.Effect,
	target string,
) policy.OperatorSource {
	return policy.OperatorSource{
		Schema: policy.OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: bundle, ParentBundleGeneration: parent,
		StaticSHA256: staticDigest,
		IssuedAt:     "2026-08-02T09:00:00Z", NotBefore: "2026-08-02T09:00:00Z", ExpiresAt: "2026-08-02T10:00:00Z",
		Root: policy.DomainSource{PolicyGeneration: rootGeneration, Rules: []policy.Rule{{
			ID: "root.rollback-rule", Effect: effect,
			Selector: policy.Selector{ID: "root.rollback-selector", Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: target}},
		}}},
		User: policy.DomainSource{PolicyGeneration: 1},
	}
}

func TestWriteArtifactsCreatesPrivateParentHierarchy(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "private", "nested")
	outPath := filepath.Join(parent, "artifacts")
	if err := writeArtifacts(outPath, map[string][]byte{"public-key": []byte("synthetic")}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "private"), parent, outPath} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("private directory %s mode=%v err=%v", path, info.Mode().Perm(), err)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(content []byte) (int, error) { return len(content), nil }
