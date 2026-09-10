package configreduction

import (
	"strings"
	"testing"
)

// The shapes here are the shapes of the two files involved on 2026-09-10, with
// nothing real in them. What the comparison reads is the presence of keys, so
// the values are placeholders on purpose: a fixture carrying a real pinned key
// would be the leak this repository exists to prevent.
const installedRoot = `{
  "schema": "hexroute.root-observe.v1",
  "mode": "observe-only",
  "operator_uid": 501,
  "pritunl_service_label": "com.example.service",
  "routes": [{"name": "one"}, {"name": "two"}],
  "policy_control": {
    "schema": "hexroute.policy-daemon-static.v1",
    "installed": {"domain": "root", "current_policy_schema": 1},
    "pinned_public_key": "placeholder",
    "signer_fingerprint": "placeholder"
  }
}`

// The stale working copy: valid on its own, and missing the two settings that
// carry authority.
const staleRoot = `{
  "schema": "hexroute.root-observe.v1",
  "mode": "observe-only",
  "operator_uid": 501,
  "routes": [{"name": "one"}]
}`

func TestAStaleConfigurationIsRefusedForWhatItDrops(t *testing.T) {
	lost, err := Reductions([]byte(installedRoot), []byte(staleRoot))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	for _, want := range []string{
		"pritunl_service_label",
		"policy_control",
		"policy_control.pinned_public_key",
		"policy_control.signer_fingerprint",
		"policy_control.installed",
		"policy_control.installed.domain",
	} {
		if !contains(lost, want) {
			t.Errorf("a candidate dropping %q was not reported; lost = %v", want, lost)
		}
	}
}

// A shorter route list is a reduction and a legitimate one. Reporting it would
// make the refusal noisy enough to be turned off, which is worse than not
// having it.
func TestAShorterListIsNotAReduction(t *testing.T) {
	lost, err := Reductions([]byte(installedRoot), []byte(installedRoot))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("a configuration lost something against itself: %v", lost)
	}
	shorter := strings.Replace(installedRoot, `[{"name": "one"}, {"name": "two"}]`, `[]`, 1)
	lost, err = Reductions([]byte(installedRoot), []byte(shorter))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("an emptied list was reported as a lost setting: %v", lost)
	}
}

func TestGainingASettingIsNotLosingOne(t *testing.T) {
	richer := strings.Replace(installedRoot,
		`"mode": "observe-only",`,
		`"mode": "observe-only",
  "upstream_probe_address": "203.0.113.53",`, 1)
	lost, err := Reductions([]byte(installedRoot), []byte(richer))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("adding a setting was reported as losing one: %v", lost)
	}
}

// A value that changed is not a setting that went away. Every install changes
// values; refusing on that would refuse every install.
func TestAChangedValueIsNotAReduction(t *testing.T) {
	changed := strings.Replace(installedRoot, `"operator_uid": 501`, `"operator_uid": 502`, 1)
	lost, err := Reductions([]byte(installedRoot), []byte(changed))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("a changed value was reported as a lost setting: %v", lost)
	}
}

func TestSomethingThatIsNotAConfigurationIsRefused(t *testing.T) {
	for _, testCase := range []struct{ name, installed, candidate string }{
		{"the installed one is not an object", `[1,2]`, installedRoot},
		{"the candidate is not an object", installedRoot, `"text"`},
		{"the candidate is not JSON", installedRoot, `{`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Reductions(
				[]byte(testCase.installed), []byte(testCase.candidate),
			); err == nil {
				t.Fatal("Reductions() accepted something that is not a configuration")
			}
		})
	}
}

// The parent survives and a setting inside it does not. This is the shape a
// rotated-but-truncated configuration takes, and it is the case the outer
// comparison alone cannot see.
func TestASettingLostInsideAKeptObjectIsReported(t *testing.T) {
	trimmed := strings.Replace(installedRoot,
		`    "pinned_public_key": "placeholder",
`, "", 1)
	lost, err := Reductions([]byte(installedRoot), []byte(trimmed))
	if err != nil {
		t.Fatalf("Reductions() error: %v", err)
	}
	if len(lost) != 1 || lost[0] != "policy_control.pinned_public_key" {
		t.Fatalf("lost = %v, want exactly policy_control.pinned_public_key", lost)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
