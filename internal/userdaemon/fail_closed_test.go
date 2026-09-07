package userdaemon

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAnUnusablePolicyControlBlockRefusesToStart is the user half of the
// fail-closed startup. See the root daemon's copy for why degrading instead is
// the wrong repair: a daemon that quietly runs without policy control is how
// this machine went a month with its control plane absent and nothing saying so.
func TestAnUnusablePolicyControlBlockRefusesToStart(t *testing.T) {
	var config map[string]any
	if err := json.Unmarshal([]byte(validConfig), &config); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeConfig(strings.NewReader(validConfig)); err != nil {
		t.Fatalf("the fixture is not a valid configuration: %v", err)
	}

	config["policy_control"] = map[string]any{
		"schema": "hexroute.policy-daemon-static.v1",
		"installed": map[string]any{
			"domain":                  "user",
			"minimum_policy_schema":   1,
			"maximum_policy_schema":   1,
			"current_policy_schema":   1,
			"static_sha256":           strings.Repeat("a", 64),
			"trusted_compiler_sha256": []string{strings.Repeat("b", 64)},
		},
		"pinned_public_key":  "not base64",
		"signer_fingerprint": strings.Repeat("c", 64),
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := DecodeConfig(strings.NewReader(string(encoded)))
	if err == nil {
		t.Fatalf("started with an unusable policy control block: %+v", runtime.PolicyControl)
	}
}
