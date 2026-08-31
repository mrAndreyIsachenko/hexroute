package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitysoak"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
)

// Every diagnosis that mattered on this host was made by hand-writing a script
// to read root-owned JSON, because nothing would say what the read model held.
// This is that question, asked of the store rather than of the daemon —
// because the moment it is needed most is the moment the daemon will not
// answer.
func TestStateAnswersFromAStoreWhoseDaemonWouldNotStart(t *testing.T) {
	cases := []struct {
		fault    connectivitytrace.Fault
		provable bool
	}{
		{connectivitytrace.FaultDuplicate, true},
		{connectivitytrace.FaultConflict, true},
		// Nothing in this one can be proven, which is the case an operator is
		// looking at when a daemon refuses to come up.
		{connectivitytrace.FaultDepthExhaustion, false},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.fault), func(t *testing.T) {
			trace, err := connectivitytrace.For(testCase.fault)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(t.TempDir(), "store")
			if _, err := connectivitysoak.Run(trace, root); err != nil {
				t.Fatalf("build a store: %v", err)
			}

			var stdout, stderr bytes.Buffer
			if code := run([]string{"--state", "--store", root}, &stdout, &stderr); code != 0 {
				t.Fatalf("code %d: %s", code, stderr.String())
			}
			var reported report
			if err := json.Unmarshal(stdout.Bytes(), &reported); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if reported.State == nil || reported.State.Resume == "" {
				t.Fatal("the state said nothing about the lineage")
			}
			if !testCase.provable {
				// Absence has to read as absence. A zeroed summary prints as
				// nothing failing and nothing stale, which is what a healthy
				// host looks like.
				if reported.State.Summary != nil {
					t.Fatalf("an unprovable lineage reported a summary: %+v",
						reported.State.Summary)
				}
				if len(reported.State.Components) != 0 {
					t.Fatal("an unprovable lineage reported components")
				}
				return
			}
			if reported.State.Summary == nil {
				t.Fatal("a provable lineage reported no summary")
			}
			if len(reported.State.Components) == 0 || len(reported.State.Sources) == 0 {
				t.Fatal("the state named no components or no sources")
			}
		})
	}
}

// The flag reads a store; without one there is nothing to read.
func TestStateWithoutAStoreIsRefused(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state"}, &stdout, &stderr); code == 0 {
		t.Fatal("--state was accepted with no store to read")
	}
}
