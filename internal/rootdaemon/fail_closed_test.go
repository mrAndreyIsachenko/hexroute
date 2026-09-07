package rootdaemon

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAnUnusablePolicyControlBlockRefusesToStart keeps startup fail-closed.
//
// The tempting repair for a daemon that will not start is to let it start
// without policy control and report the fault instead. That is precisely how
// this machine sat dormant for a month: the control plane was absent and
// nothing about the running system said so. A daemon that cannot establish its
// static authority must refuse — and the offline configuration check exists so
// that the refusal is met before installation rather than after.
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
			"domain":                  "root",
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
