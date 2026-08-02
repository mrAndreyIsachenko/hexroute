package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOperatorSource(t *testing.T) {
	file, err := os.Open(policyFixture("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	source, err := DecodeOperatorSource(file)
	if err != nil {
		t.Fatalf("decode valid source: %v", err)
	}
	if source.BundleGeneration != 2 || source.Root.PolicyGeneration != 1 || source.User.PolicyGeneration != 2 {
		t.Fatalf("unexpected generations: %#v", source)
	}
	user, err := source.DomainPayload(DomainUser)
	if err != nil || user.Domain != DomainUser || len(user.Rules) != 1 || len(user.Leases) != 1 {
		t.Fatalf("unexpected user payload: %#v, %v", user, err)
	}
}

func TestDecodeOperatorSourceRejectsMalformedFixtures(t *testing.T) {
	for _, name := range []string{
		"duplicate-key.yaml",
		"anchor-alias.yaml",
		"unknown-field.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			file, err := os.Open(policyFixture(name))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := DecodeOperatorSource(file); !errors.Is(err, ErrInvalidOperatorSource) {
				t.Fatalf("malformed fixture should be rejected, got %v", err)
			}
		})
	}
}

func TestDecodeOperatorSourceRejectsMultipleDocumentsAndSize(t *testing.T) {
	valid, err := os.ReadFile(policyFixture("valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeOperatorSource(strings.NewReader(string(valid) + "\n---\n{}\n")); !errors.Is(err, ErrInvalidOperatorSource) {
		t.Fatalf("multiple documents should be rejected, got %v", err)
	}
	oversized := strings.Repeat("x", MaxOperatorSourceSize+1)
	if _, err := DecodeOperatorSource(strings.NewReader(oversized)); !errors.Is(err, ErrInvalidOperatorSource) {
		t.Fatalf("oversized source should be rejected, got %v", err)
	}
}

func policyFixture(name string) string {
	return filepath.Join("..", "..", "testdata", "policy", "v1", name)
}
