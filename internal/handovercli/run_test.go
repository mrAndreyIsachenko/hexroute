package handovercli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The command runs.
//
// It shipped with two flags named version — one for the binary's, one for the
// signed configuration's — which panics the moment the flag set is built. It
// reached the machine because the package was compiled and never invoked: this
// test invokes it.
func TestTheCommandBuildsItsFlagsWithoutColliding(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"--version"}, stdout, stderr); code != 0 {
		t.Fatalf("--version returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hexroute-handover version=") {
		t.Fatalf("--version printed %q", stdout.String())
	}
}

// Aborting when nothing is in flight says so, and does not report success.
//
// The exit code is not zero on purpose: "there was nothing to abort" and "the
// handover completed" are different answers, and a script that treated them
// alike would call a machine handed over when it never was.
func TestAbortingNothingSaysSoAndIsNotSuccess(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", filepath.Join(t.TempDir(), "claim.json"),
		"abort",
	}, stdout, stderr)
	if code == 0 {
		t.Fatalf("aborting nothing reported success: %s", stdout.String())
	}
	var outcome struct {
		Completed bool   `json:"Completed"`
		Reason    string `json:"Reason"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("the report is not readable: %q (%v)", stdout.String(), err)
	}
	if outcome.Completed {
		t.Fatal("aborting nothing reported a completed handover")
	}
	if outcome.Reason == "" {
		t.Fatal("aborting nothing gave no reason")
	}
}

// A subcommand nobody wrote is refused, with the usage rather than a crash.
func TestAnUnknownSubcommandIsRefused(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"take-the-tunnel"}, stdout, stderr); code != 2 {
		t.Fatalf("an unknown subcommand returned %d", code)
	}
	if !strings.Contains(stderr.String(), "begin|rehearse|abort") {
		t.Fatalf("it did not say what it accepts: %q", stderr.String())
	}
}

// Beginning without what it needs refuses before it touches anything.
//
// A misconfigured attempt must not get as far as claiming the tunnel: that
// would stop the supervisor from starting it and leave nothing started in its
// place.
func TestBeginningWithoutItsFlagsRefusesEarly(t *testing.T) {
	claim := filepath.Join(t.TempDir(), "claim.json")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", claim,
		"begin",
	}, stdout, stderr)
	if code != 2 {
		t.Fatalf("begin without its flags returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--tunnel-version") {
		t.Fatalf("it refused for some other reason than the missing flags: %q",
			stderr.String())
	}
	if _, err := os.Stat(claim); err == nil {
		t.Fatal("it claimed the tunnel before finding it had nothing to start")
	}
}

// Neither does it accept a stray argument as a subcommand.
func TestExtraArgumentsAreRefused(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"abort", "now"}, stdout, stderr); code != 2 {
		t.Fatalf("two subcommands returned %d", code)
	}
}
