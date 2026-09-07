package policycheck

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/userdaemon"
)

func soundPolicyControl() map[string]any {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).
		Public().(ed25519.PublicKey)
	return map[string]any{
		"schema": "hexroute.policy-daemon-static.v1",
		"installed": map[string]any{
			"domain":                  "user",
			"minimum_policy_schema":   1,
			"maximum_policy_schema":   1,
			"current_policy_schema":   1,
			"static_sha256":           strings.Repeat("a", 64),
			"trusted_compiler_sha256": []string{strings.Repeat("b", 64)},
		},
		"pinned_public_key":  base64.RawStdEncoding.EncodeToString(publicKey),
		"signer_fingerprint": policy.SHA256Hex(publicKey),
	}
}

func writePreparedUserConfig(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	config := map[string]any{
		"schema":                       "hexroute.user-observe.v1",
		"mode":                         "observe-only",
		"observation_interval_seconds": 15,
		"expected_uid":                 501,
		"profile_id":                   "synthetic-profile",
		"pritunl_cli":                  "/usr/local/bin/pritunl-client",
		"outer_endpoint": map[string]any{
			"transport":          "direct_tls",
			"certificate_policy": "handshake_only",
			"address":            "198.51.100.30:443",
			"server_name":        "outer.example.invalid",
			"timeout_seconds":    4,
		},
		"policy": map[string]any{
			"failure_threshold":           2,
			"action_budget":               3,
			"base_backoff_seconds":        15,
			"max_backoff_seconds":         120,
			"verification_window_seconds": 30,
			"cooldown_seconds":            600,
			"wake_settle_seconds":         30,
			"connecting_grace_seconds":    120,
			"otp_period_seconds":          30,
			"otp_min_valid_seconds":       8,
		},
	}
	if mutate != nil {
		mutate(config)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "user-observe.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCheckAnswersForTheDaemon is the property that makes the check worth
// having: whatever it accepts, the daemon accepts, because both ask the same
// function. The accepted cases matter as much as the refused ones — without
// them this would pass for a checker that refuses everything.
func TestCheckAnswersForTheDaemon(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"no policy control block":  nil,
		"malformed policy control": func(config map[string]any) { config["policy_control"] = map[string]any{"schema": "wrong"} },
		"unknown field":            func(config map[string]any) { config["unexpected"] = 1 },
		"a sound policy control block": func(config map[string]any) {
			config["policy_control"] = soundPolicyControl()
		},
		"a fingerprint that is not the key's": func(config map[string]any) {
			control := soundPolicyControl()
			control["signer_fingerprint"] = strings.Repeat("e", 64)
			config["policy_control"] = control
		},
		"a block for the other domain": func(config map[string]any) {
			control := soundPolicyControl()
			control["installed"].(map[string]any)["domain"] = "root"
			config["policy_control"] = control
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := writePreparedUserConfig(t, mutate)
			_, checkErr := Installed(policy.DomainUser, path)
			runtime, daemonErr := userdaemon.LoadConfig(path)

			daemonAccepts := daemonErr == nil && runtime.PolicyControl != nil
			if (checkErr == nil) != daemonAccepts {
				t.Fatalf("check accepted=%v but the daemon accepted=%v; the checker has drifted",
					checkErr == nil, daemonAccepts)
			}
		})
	}
}

func TestCheckReportsWhatItFoundAndNothingElse(t *testing.T) {
	path := writePreparedUserConfig(t, func(config map[string]any) {
		config["policy_control"] = soundPolicyControl()
	})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--domain", "user", "--config", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("check refused a sound configuration: %s", stderr.String())
	}
	var reported result
	if err := json.Unmarshal(stdout.Bytes(), &reported); err != nil {
		t.Fatal(err)
	}
	if reported.Domain != policy.DomainUser || reported.StaticSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("reported = %+v", reported)
	}
}

func TestCheckRefusesWithoutOutput(t *testing.T) {
	path := writePreparedUserConfig(t, nil)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--domain", "user", "--config", path}, &stdout, &stderr); code == 0 {
		t.Fatal("a configuration with no policy control block was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("a refused check produced output: %q", stdout.String())
	}
}

// TestCheckDoesNotRestateTheDaemonRules is the boundary the whole check depends
// on. A checker that validates with its own copy of the rules drifts, and a
// drifted checker hands out confidence just before an irreversible step.
func TestCheckDoesNotRestateTheDaemonRules(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Installed" {
			continue
		}
		loads := 0
		ast.Inspect(function, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "LoadConfig" {
				loads++
			}
			return true
		})
		if loads != 2 {
			t.Fatalf("Installed calls LoadConfig %d times, want once per domain", loads)
		}
		return
	}
	t.Fatal("Installed not found")
}
