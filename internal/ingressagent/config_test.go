package ingressagent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testEnvironment(t *testing.T) map[string]string {
	t.Helper()
	directory := t.TempDir()
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).
		Public().(ed25519.PublicKey)
	keyPath := filepath.Join(directory, "operator.pub")
	if err := os.WriteFile(keyPath,
		[]byte(base64.RawStdEncoding.EncodeToString(publicKey)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		envStoreEndpoint: "https://store.invalid",
		envStoreRegion:   "fra1",
		envStoreBucket:   "hexroute-config-test",
		envStoreKeyID:    "EXAMPLEKEYIDENTIFIER",
		envStoreSecret:   "not-a-secret-only-a-test-fixture-value",
		envPublicKeyFile: keyPath,
		envTargetKind:    "node",
		envTargetKey:     "ingress-provider-b",
		envStateDir:      filepath.Join(directory, "state"),
		envConfigPath:    filepath.Join(directory, "xray.json"),
		envReloadCommand: "/bin/true",
	}
}

func lookupFrom(environment map[string]string) LookupEnv {
	return func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	}
}

// Nothing here has a default. An agent that guessed at its bucket, its target
// or the key it trusts would be an agent nobody decided the behaviour of.
func TestEveryAgentSettingIsRequired(t *testing.T) {
	environment := testEnvironment(t)
	if _, err := LoadConfig(lookupFrom(environment)); err != nil {
		t.Fatalf("complete environment: %v", err)
	}
	for name := range environment {
		reduced := testEnvironment(t)
		delete(reduced, name)
		if _, err := LoadConfig(lookupFrom(reduced)); !errors.Is(err, ErrAgent) {
			t.Fatalf("accepted an environment without %s", name)
		}
	}
}

func TestTheAgentRefusesWhatItCannotTreatAsAPathOrATarget(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "relative state directory", key: envStateDir, value: "state"},
		{name: "unclean config path", key: envConfigPath, value: "/etc/hexroute/../xray.json"},
		{name: "relative reload command", key: envReloadCommand, value: "systemctl"},
		{name: "unknown target kind", key: envTargetKind, value: "fleet"},
		{name: "an argument that is not one", key: envReloadArgs, value: "restart; rm -rf /"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			environment := testEnvironment(t)
			environment[testCase.key] = testCase.value
			if _, err := LoadConfig(lookupFrom(environment)); !errors.Is(err, ErrAgent) {
				t.Fatalf("accepted %s = %q", testCase.key, testCase.value)
			}
		})
	}
}

// The key is placed on the host when it is built. Missing or unreadable, the
// agent has no opinion about who may change what it runs, and applies nothing.
func TestAMissingOrUnusablePinnedKeyIsRefusedRatherThanDefaulted(t *testing.T) {
	directory := t.TempDir()
	for _, testCase := range []struct {
		name    string
		content []byte
		write   bool
	}{
		{name: "absent"},
		{name: "empty", content: nil, write: true},
		{name: "not a key", content: []byte("operator"), write: true},
		{
			name:    "a private key",
			content: []byte(base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.PrivateKeySize))),
			write:   true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(directory, testCase.name+".pub")
			if testCase.write {
				if err := os.WriteFile(path, testCase.content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ReadPinnedKey(path); !errors.Is(err, ErrAgent) {
				t.Fatalf("accepted %s: %v", testCase.name, err)
			}
		})
	}
}

func TestReloadArgumentsAreAcceptedOnlyAsArguments(t *testing.T) {
	environment := testEnvironment(t)
	environment[envReloadArgs] = "restart hexroute-xray.service"
	config, err := LoadConfig(lookupFrom(environment))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.ReloadArgs) != 2 || config.ReloadArgs[1] != "hexroute-xray.service" {
		t.Fatalf("reload args: %v", config.ReloadArgs)
	}
}
