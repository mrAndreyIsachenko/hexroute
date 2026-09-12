package rootdaemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// The memory has to survive a restart, or a runtime that restarts would
// rebuild the tunnel on its first cycle every time.
func TestWhatACycleLeavesSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel-state.json")
	store, err := newTunnelStateStore(path)
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	left := tunnelplan.State{
		Known:           true,
		Carrier:         tunnelplan.Signature("203.0.113.20=en0"),
		LinkPresent:     true,
		PayloadFailures: 1,
	}
	if err := store.Save(left); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reopened, err := newTunnelStateStore(path)
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	if got := reopened.Load(); got != left {
		t.Fatalf("loaded %+v, want %+v", got, left)
	}
}

// A cycle that cannot remember decides from what it can see. Refusing to decide
// over a file would stop the causes that need no memory — the process, the wake
// gap, the payload path — and those are the ones that matter most.
func TestAnUnreadableMemoryIsAnUnknownOneRatherThanAFailure(t *testing.T) {
	directory := t.TempDir()
	for _, testCase := range []struct {
		name     string
		contents string
	}{
		{name: "absent", contents: ""},
		{name: "not JSON", contents: "{"},
		{name: "another schema", contents: `{"schema":"something.else","state":{"known":true}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(directory, testCase.name+".json")
			if testCase.contents != "" {
				if err := os.WriteFile(path, []byte(testCase.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := newTunnelStateStore(path)
			if err != nil {
				t.Fatalf("newTunnelStateStore: %v", err)
			}
			if got := store.Load(); got.Known {
				t.Fatalf("an unreadable memory answered a known state: %+v", got)
			}
		})
	}
}

// An unknown state is what a first cycle has, and the planner must not read a
// carrier change out of it.
func TestAnUnknownMemoryDoesNotBecomeACause(t *testing.T) {
	store, err := newTunnelStateStore(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	plan, next, err := tunnelplan.Decide(
		tunnelplan.Policy{WakeThreshold: 90_000_000_000, PayloadFailures: 2},
		store.Load(),
		tunnelplan.Observed{
			ProcessRunning: true,
			Carrier:        tunnelplan.Signature("203.0.113.20=en0"),
			LinkPresent:    true,
			PayloadOK:      true,
		})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	for _, cause := range plan.Causes {
		if cause == tunnelplan.CauseCarrierChanged ||
			cause == tunnelplan.CauseLinkReturned {
			t.Fatalf("a first cycle read %q out of an absent memory", cause)
		}
	}
	if !next.Known {
		t.Fatal("the first cycle left nothing for the second")
	}
}
