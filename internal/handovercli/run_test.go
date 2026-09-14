package handovercli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
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
	// Each one named, rather than the line as a whole: the usage gains
	// subcommands, and a test that pinned the exact string would fail for a
	// correct change while saying nothing about whether the usage is right.
	for _, subcommand := range []string{"begin", "check", "rehearse", "release", "check-release", "abort"} {
		if !strings.Contains(stderr.String(), subcommand) {
			t.Fatalf("the usage does not offer %q: %q", subcommand, stderr.String())
		}
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

// Checking touches nothing.
//
// This is the whole point of it: an operator asks whether the handover would
// complete, and the answer must not be that it half happened. A begin that
// refuses has already placed the claim, and the machine has no tunnel until
// somebody aborts.
func TestCheckingClaimsNothingAndStartsNothing(t *testing.T) {
	claim := filepath.Join(t.TempDir(), "claim.json")
	session := filepath.Join(t.TempDir(), "handover.json")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	// Nothing is configured, so every precondition it can ask about refuses.
	code := Run([]string{"--session", session, "--claim", claim, "check"}, stdout, stderr)
	if code == 0 {
		t.Fatalf("check passed with nothing configured: %s", stdout.String())
	}
	if _, err := os.Stat(claim); err == nil {
		t.Fatal("check placed a claim")
	}
	if _, err := os.Stat(session); err == nil {
		t.Fatal("check left a session record")
	}
}

// It reports every precondition rather than stopping at the first.
//
// Each round of one-fault-at-a-time is another run of a ceremony that needs the
// operator present. What they asked is whether the handover is ready, which is
// a statement about all of them.
func TestCheckReportsEveryPreconditionAtOnce(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", filepath.Join(t.TempDir(), "claim.json"),
		"check",
	}, stdout, stderr)

	for _, precondition := range []string{
		"signed version", "tunnel binary", "payload probe",
		"nothing in flight", "tunnel unclaimed", "tunnel process",
		"previous owner reads claims",
	} {
		if !strings.Contains(stdout.String(), precondition) {
			t.Fatalf("check did not report %q:\n%s", precondition, stdout.String())
		}
	}
}

// A claim already in place is a refusal, not a detail.
func TestCheckRefusesWhenTheTunnelIsAlreadyClaimed(t *testing.T) {
	directory := t.TempDir()
	claim := filepath.Join(directory, "claim.json")
	claims, err := tunnelclaim.Open(claim)
	if err != nil {
		t.Fatal(err)
	}
	claims.WithProcessConfig(tunnelclaim.HexrouteContent)
	if err := claims.Place("handover-earlier"); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", claim, "check",
	}, stdout, stderr)
	if code == 0 {
		t.Fatal("check passed with the tunnel already claimed")
	}
	if !strings.Contains(stdout.String(), "handover-earlier") {
		t.Fatalf("it did not name what holds the tunnel:\n%s", stdout.String())
	}
}

// The preflight asks about the runtime it is taking the tunnel from.
//
// On 2026-09-14 every precondition about this side passed and the handover
// still went wrong: the previous owner's installed program was older than the
// claim, so it could not read one. It kept starting its own tunnel against the
// interface this runtime had taken, and launchd restarted it every eighteen
// seconds. A preflight that inspects only the side it was written for reports
// ready for a handover the other side cannot honour.
func TestThePreviousOwnerMustBeAbleToReadAClaim(t *testing.T) {
	directory := t.TempDir()

	aware := filepath.Join(directory, "aware.sh")
	if err := os.WriteFile(aware, []byte(
		"#!/bin/bash\nf=\"${X:-"+tunnelclaim.DefaultPath+"}\"\n[[ -f \"$f\" ]]\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := supervisorReadsClaims(aware); err != nil {
		t.Fatalf("a program that reads the claim was refused: %v", err)
	}

	// The revision that was actually installed during the handover: a
	// supervisor with no notion of a claim at all.
	unaware := filepath.Join(directory, "unaware.sh")
	if err := os.WriteFile(unaware, []byte(
		"#!/bin/bash\nstart_singbox() { exec sing-box run; }\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := supervisorReadsClaims(unaware); err == nil {
		t.Fatal("a supervisor that cannot read a claim was accepted")
	}

	// Absent is not permission either, and it is not the same answer as old.
	//
	// A missing program is a machine this preflight cannot make a statement
	// about; an old one is a machine whose supervisor will fight for the
	// tunnel. Both refuse, and reporting them alike would send the reader to
	// upgrade a file that is not there.
	err := supervisorReadsClaims(filepath.Join(directory, "nothing.sh"))
	if err == nil {
		t.Fatal("a program that is not there was accepted")
	}
	if !strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("an absent program was reported as an old one: %v", err)
	}
}

// The preflight names what it does not cover.
//
// An empty slot in a written list is visible; an absent thought is not. On
// 2026-09-14 this command reported six preconditions holding, every one a
// question about this runtime, and the handover then failed on the other
// runtime's installed revision — which nothing here had asked about. The
// obligation to print what is skipped is what turns that into a blank line
// somebody can read.
func TestCheckNamesWhatItDoesNotCover(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", filepath.Join(t.TempDir(), "claim.json"),
		"check",
	}, stdout, stderr)

	if !strings.Contains(stdout.String(), "not checked:") {
		t.Fatalf("check does not say what it skips:\n%s", stdout.String())
	}
	uncovered := strings.SplitN(stdout.String(), "not checked:", 2)[1]
	if strings.Count(uncovered, "\n  - ") < 3 {
		t.Fatalf("the list of what is not covered is too thin to be honest:\n%s", uncovered)
	}
}

// Releasing without what a restore would need refuses before it touches anything.
//
// A release that stopped this runtime's tunnel and then had nothing to start it
// again from would leave the machine with a claim and no tunnel.
func TestReleasingWithoutItsFlagsRefusesEarly(t *testing.T) {
	claim := filepath.Join(t.TempDir(), "claim.json")
	claims, err := tunnelclaim.Open(claim)
	if err != nil {
		t.Fatal(err)
	}
	claims.WithProcessConfig(tunnelclaim.HexrouteContent)
	if err := claims.Place("handover-held"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(claim)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := Run([]string{
		"--session", filepath.Join(t.TempDir(), "handover.json"),
		"--claim", claim,
		"release",
	}, stdout, stderr)
	if code != 2 {
		t.Fatalf("release without its flags returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--tunnel-version") {
		t.Fatalf("it refused for some other reason: %q", stderr.String())
	}
	after, err := os.ReadFile(claim)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the claim changed under a refused release: %v", err)
	}
}

// There is nothing to release when this runtime does not hold the tunnel.
func TestReleasingWhatIsNotHeldIsRefused(t *testing.T) {
	claims, err := tunnelclaim.Open(filepath.Join(t.TempDir(), "claim.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownTunnel(claims); err == nil || !strings.Contains(err.Error(), "does not hold") {
		t.Fatalf("ownTunnel with no claim = %v, want a refusal", err)
	}
}

// Checking a release reports every precondition, about both runtimes, and
// gives back nothing.
func TestCheckingAReleaseAsksAboutBothRuntimesAndTouchesNothing(t *testing.T) {
	claim := filepath.Join(t.TempDir(), "claim.json")
	session := filepath.Join(t.TempDir(), "handover.json")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	code := Run([]string{"--session", session, "--claim", claim, "check-release"}, stdout, stderr)
	if code == 0 {
		t.Fatalf("check-release passed with nothing configured:\n%s", stdout.String())
	}
	for _, precondition := range []string{
		"signed version", "payload probe", "nothing in flight", "this runtime holds it",
		"previous owner reads claims", "previous owner's configuration", "previous owner is running",
		"not checked:",
	} {
		if !strings.Contains(stdout.String(), precondition) {
			t.Fatalf("check-release did not report %q:\n%s", precondition, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "does not hold") {
		t.Fatalf("with no claim it did not refuse on holding nothing:\n%s", stdout.String())
	}
	if _, err := os.Stat(claim); err == nil {
		t.Fatal("check-release wrote a claim")
	}
	if _, err := os.Stat(session); err == nil {
		t.Fatal("check-release left a session record")
	}
}
