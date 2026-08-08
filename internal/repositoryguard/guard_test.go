package repositoryguard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackedStructuredArtifactsRespectPublicRepositoryBoundary(t *testing.T) {
	repository := repositoryRoot(t)
	output := gitOutput(t, repository, "ls-files", "-z")
	for _, rawPath := range bytes.Split(output, []byte{0}) {
		path := string(rawPath)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			// The index can still list files deleted by the working-tree change
			// currently being checked. There is no artifact content to validate.
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := ValidateArtifact(path, data); err != nil {
			t.Fatalf("tracked artifact rejected: %v", err)
		}
	}
}

func TestRepositoryHistoryContainsNoForbiddenPolicyArtifactNames(t *testing.T) {
	repository := repositoryRoot(t)
	output := gitOutput(t, repository, "log", "--all", "--format=", "--name-only", "-z")
	for _, rawPath := range bytes.Split(output, []byte{0}) {
		path := strings.TrimSpace(string(rawPath))
		if path != "" && unsafePath(filepath.ToSlash(path)) {
			t.Fatalf("repository history contains forbidden policy artifact path %q", path)
		}
	}
}

func TestLivePolicyMaterialIsRejected(t *testing.T) {
	tests := []struct {
		path string
		data string
	}{
		{"operator-policy/live.yaml", "schema: hexroute.operator-policy.v1\n"},
		{"deploy/live.yaml", "schema: hexroute.operator-policy.v1\n"},
		{"deploy/live.json", `{"schema":"hexroute.policy-manifest.v1"}`},
		{"deploy/live.json", `{"signer_fingerprint":"0123456789abcdef"}`},
		{"deploy/live.json", `{"source_path":"/Users/operator/private/policy.yaml"}`},
		{"deploy/live.json", `{"profile_id":"corporate-profile"}`},
		{"deploy/live.json", `{"address":"3.68.185.131"}`},
		{"deploy/live.json", `{"credential":{"reference":"keychain-live-account"}}`},
		{"deploy/live.json", `{"value":"HEXROUTE_CANARY_TOTP_SEED"}`},
		{"candidate.policy.yaml", "schema: harmless.example.v1\n"},
		{"candidate.signed-policy.json", `{}`},
	}
	for _, test := range tests {
		if err := ValidateArtifact(test.path, []byte(test.data)); !errors.Is(err, ErrUnsafeArtifact) {
			t.Errorf("ValidateArtifact(%q) error = %v, want %v", test.path, err, ErrUnsafeArtifact)
		}
	}
}

func TestSyntheticRepositoryExamplesAreAccepted(t *testing.T) {
	tests := []struct {
		path string
		data string
	}{
		{"deploy/example.json", `{"profile_id":"synthetic-profile","address":"192.0.2.8:443","server_name":"edge.example.invalid"}`},
		{"testdata/policy/v1/example.yaml", "schema: hexroute.operator-policy.v1\n"},
		{secretCanaryFixture, `{"value":"HEXROUTE_CANARY_TOTP_SEED"}`},
	}
	for _, test := range tests {
		if err := ValidateArtifact(test.path, []byte(test.data)); err != nil {
			t.Errorf("ValidateArtifact(%q) error = %v", test.path, err)
		}
	}
}

func TestLivePolicyPatternsAreIgnored(t *testing.T) {
	repository := repositoryRoot(t)
	for _, path := range []string{
		"operator-policy/live.yaml",
		"policy-runtime/active.json",
		"policy-evidence/session.json",
		"candidate.policy.yaml",
		"candidate.policy-bundle.json",
		"candidate.signed-policy.json",
		"candidate.policy-approval.json",
		"candidate.policy-receipt.json",
		"candidate.policy-fingerprint",
	} {
		command := exec.Command("git", "check-ignore", "--quiet", "--", path)
		command.Dir = repository
		if err := command.Run(); err != nil {
			t.Errorf("%s is not covered by .gitignore: %v", path, err)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func gitOutput(t *testing.T, repository string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return output
}
