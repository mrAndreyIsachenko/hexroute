package reconciler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyntheticPrerequisiteFixtureIsFailClosedAndContainsNoLiveValues(t *testing.T) {
	var fixture struct {
		Schema          string                 `json:"schema"`
		ExpectedBinding GenerationBinding      `json:"expected_binding"`
		Evidence        []PrerequisiteEvidence `json:"evidence"`
	}
	loadFixture(t, "synthetic-prerequisites.json", &fixture)
	if fixture.Schema != PrerequisiteSchema {
		t.Fatalf("schema = %q", fixture.Schema)
	}
	assertNoLiveFixtureValues(t, fixture)
	gate, err := EvaluatePrerequisites(fixture.ExpectedBinding, fixture.Evidence)
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	if gate.Ready() || gate.Reason() != GateReasonIncompletePrerequisite ||
		gate.Prerequisite() != PrerequisiteObservableConnectivityStateMachine {
		t.Fatalf("fixture gate = ready:%v reason:%s prerequisite:%s", gate.Ready(), gate.Reason(), gate.Prerequisite())
	}
}

func TestSyntheticCapabilityFixtureMatchesRegistry(t *testing.T) {
	var fixture struct {
		Schema       string                 `json:"schema"`
		Capabilities []CapabilityDescriptor `json:"capabilities"`
	}
	loadFixture(t, "synthetic-capabilities.json", &fixture)
	if fixture.Schema != "hexroute.synthetic-reconciler-capabilities.v1" {
		t.Fatalf("schema = %q", fixture.Schema)
	}
	assertNoLiveFixtureValues(t, fixture)
	registry, err := NewRegistry(fixture.Capabilities)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if !registry.SyntheticOnly() {
		t.Fatal("fixture registry is not synthetic-only")
	}
	if got, want := registry.IDs(), DefaultSyntheticRegistry().IDs(); strings.Join(capabilityStrings(got), ",") != strings.Join(capabilityStrings(want), ",") {
		t.Fatalf("fixture ids = %v, want %v", got, want)
	}
}

func loadFixture(t *testing.T, name string, destination any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reconciler", "v1", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}

func assertNoLiveFixtureValues(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"3.68.", "52.27.", "164.92.", "172.31.", "192.168.",
		"pritunl", "keychain", "totp", "otp", "pin", "vless", "reality",
		"twilight", "adguard", "sing-box", "xray", "gitlab.smart-dev",
		"access.medvidi", "session_id", "profile",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("synthetic fixture contains forbidden live value fragment %q", forbidden)
		}
	}
}

func capabilityStrings(ids []CapabilityID) []string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return values
}
