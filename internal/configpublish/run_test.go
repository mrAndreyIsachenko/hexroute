package configpublish

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func testEnvironment() map[string]string {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).
		Public().(ed25519.PublicKey)
	return map[string]string{
		envStoreEndpoint: "https://store.invalid",
		envStoreRegion:   "fra1",
		envStoreBucket:   "hexroute-config-test",
		envStoreKeyID:    "EXAMPLEKEYIDENTIFIER",
		envStoreSecret:   "not-a-secret-only-a-test-fixture-value",
		envDatabaseURL:   "postgres://hexroute_publisher@127.0.0.1:5432/postgres?sslmode=disable",
		envPublicKey:     base64.RawStdEncoding.EncodeToString(publicKey),
	}
}

func lookupFrom(environment map[string]string) LookupEnv {
	return func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	}
}

// Every variable is required. A publisher that defaulted its bucket or its
// database would publish somewhere nobody chose.
func TestEveryPublisherSettingIsRequired(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"-check"},
		lookupFrom(testEnvironment()), &stdout, &stderr); code != 0 {
		t.Fatalf("complete environment code = %d, stderr = %s", code, stderr.String())
	}
	for name := range testEnvironment() {
		environment := testEnvironment()
		delete(environment, name)
		stdout.Reset()
		stderr.Reset()
		if code := Run(context.Background(), []string{"-check"},
			lookupFrom(environment), &stdout, &stderr); code != 1 {
			t.Fatalf("without %s code = %d", name, code)
		}
	}
}

func TestPublishingWithoutAVersionIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), nil,
		lookupFrom(testEnvironment()), &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d", code)
	}
	stderr.Reset()
	if code := Run(context.Background(), []string{"-version", "/nonexistent/version.json"},
		lookupFrom(testEnvironment()), &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "unreadable_version") {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
}

// The tool that holds the store credential and the database must not also be
// able to sign. This is that claim as a fact about the package rather than as
// a promise in a comment.
func TestThePublishingPathCannotSign(t *testing.T) {
	forbidden := []string{"internal/policyapproval", "internal/signing"}
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, parsed := range packages {
		for name, file := range parsed.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
				for _, banned := range forbidden {
					if strings.HasSuffix(path, banned) {
						t.Fatalf("%s imports %s, which is a signing path", name, path)
					}
				}
			}
		}
	}
	// ed25519 is imported, and only for a public key: the package holds no
	// private key type and calls nothing that produces a signature.
	source := readPackageSource(t)
	for _, call := range []string{"ed25519.Sign(", "ed25519.NewKeyFromSeed(", "ed25519.GenerateKey("} {
		if strings.Contains(source, call) {
			t.Fatalf("the publishing path calls %s", call)
		}
	}
}

func readPackageSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var builder strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		builder.Write(content)
	}
	return builder.String()
}
