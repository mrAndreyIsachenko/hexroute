package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/userdaemon"
)

// The smoke test starts the real daemon and talks to it over the real socket.
//
// Until it existed, nothing in this repository ever ran a daemon. Thirty shell
// gates checked imports, plists, structure and documentation, and every one of
// them passed while `policy status` returned an internal error on the machine —
// because the fault was in a value crossing the IPC boundary, and no test ever
// crossed it.
//
// What this catches that a unit test cannot: the binary failing to start, the
// wiring being wrong, the socket never appearing, a response that does not
// decode, and a daemon that answers too slowly to be usable. Those are exactly
// the failures that reach an operator as "it is installed and nothing works".
//
// What it deliberately does NOT cover: the policy control plane. The policy
// store path is derived from the operator's real home directory by design —
// `user.Current()` ignores HOME on macOS, which was measured rather than
// assumed — so a daemon started with a policy control block would open the
// operator's live store. A test must not touch that.
//
// So this does not catch every fault of the kind it was written for, and the
// limit was measured rather than hoped: reintroducing the lapsed-status defect
// that reached this machine leaves all three tests here passing, and fails
// TestLapsedStatusSurvivesTheOutputBoundary in policycontrol. Assert at the
// boundary a value crosses; running the daemon widens the net, it does not
// replace that.
//
// The root daemon has no counterpart here: it refuses to configure its socket
// unless it is running as root, so a smoke test for it would have to run
// privileged, which a unit gate must not.
//
// There is no platform guard. This daemon is macOS-only, and a skip on another
// platform would print the same green as a passing run — which is the failure
// this repository's hollow-green gate exists to prevent. Somewhere it cannot
// run, it should say so by failing.

const smokeSocketDeadline = 15 * time.Second

func TestDaemonStartsServesAndStops(t *testing.T) {
	daemon := startSmokeDaemon(t)

	for _, request := range []ipc.Request{
		{Action: ipc.ActionStatus},
		{Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{}},
	} {
		t.Run(string(request.Action), func(t *testing.T) {
			response := daemon.do(t, request)
			if !response.OK {
				t.Fatalf("%s was refused: error=%q", request.Action, response.Error)
			}
		})
	}
}

// TestDaemonReportsAValidPolicyStatus is the shape of the bug that got through.
// A status the daemon cannot validate cannot leave it, and the failure appears
// as an unexplained internal error rather than as anything about policy.
func TestDaemonReportsAValidPolicyStatus(t *testing.T) {
	daemon := startSmokeDaemon(t)

	response := daemon.do(t, ipc.Request{
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !response.OK || response.PolicyStatus == nil {
		t.Fatalf("policy status was refused: error=%q", response.Error)
	}
	status := response.PolicyStatus.Status
	if err := status.Validate(); err != nil {
		t.Fatalf("the daemon reported a status it could not have assembled: %v (%+v)", err, status)
	}
	if status.Domain != policy.DomainUser {
		t.Fatalf("status domain = %q, want user", status.Domain)
	}
	if err := response.PolicyStatus.AuthorizationSuspension.Validate(); err != nil {
		t.Fatalf("invalid suspension overlay: %v", err)
	}
}

// TestDaemonAnswersPromptly is the stall detector. A daemon whose loop blocks —
// on a peer that never replies, on a lock it cannot get — keeps its socket and
// stops answering, which reads from outside exactly like a healthy daemon until
// something waits on it.
func TestDaemonAnswersPromptly(t *testing.T) {
	daemon := startSmokeDaemon(t)

	for attempt := 0; attempt < 5; attempt++ {
		start := time.Now()
		response := daemon.do(t, ipc.Request{Action: ipc.ActionStatus})
		elapsed := time.Since(start)
		if !response.OK {
			t.Fatalf("status was refused on attempt %d: %q", attempt, response.Error)
		}
		if elapsed > 2*time.Second {
			t.Fatalf("status took %s on attempt %d; a daemon this slow is a stalled one", elapsed, attempt)
		}
	}
}

// unixSocketPathLimit is sizeof(sockaddr_un.sun_path) on Darwin. t.TempDir()
// alone produces paths past it — the directory it builds from the test name is
// long enough on its own — so the smoke root is taken from a short base.
const unixSocketPathLimit = 104

func smokeRoot(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("/private/tmp", "hexroute-smoke")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	// The daemon refuses paths that are not already clean and real.
	root, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

type smokeDaemon struct {
	socket string
	stdout *strings.Builder
	stderr *strings.Builder
}

func (daemon smokeDaemon) do(t *testing.T, request ipc.Request) ipc.Response {
	t.Helper()
	request.Version = ipc.ProtocolVersion
	request.RequestID = "11111111-1111-4111-8111-11111111111" + string(rune('0'+len(request.Action)%10))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := ipc.Client{Path: daemon.socket, Timeout: 5 * time.Second}.Do(ctx, request)
	if err != nil {
		t.Fatalf("%s round trip: %v\nstdout:\n%s\nstderr:\n%s",
			request.Action, err, daemon.stdout.String(), daemon.stderr.String())
	}
	return response
}

func startSmokeDaemon(t *testing.T) smokeDaemon {
	t.Helper()
	root := smokeRoot(t)
	binary := filepath.Join(root, "hexroute-userd")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build hexroute-userd: %v\n%s", buildErr, output)
	}

	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(stateDir, "userd.sock")
	state := filepath.Join(stateDir, "pritunl-planner.json")
	// A Unix socket path longer than sun_path is refused by bind(2), and the
	// daemon reports that as an invalid configuration like any other — which
	// sends the reader looking at the configuration file. Say it here instead.
	if len(socket) >= unixSocketPathLimit {
		t.Fatalf("socket path is %d bytes, over the %d-byte limit: %s",
			len(socket), unixSocketPathLimit, socket)
	}
	config := writeSmokeConfig(t, root)

	command := exec.Command(binary,
		"--observe",
		"--config", config,
		"--state", state,
		"--socket", socket,
	)
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start hexroute-userd: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	deadline := time.Now().Add(smokeSocketDeadline)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return smokeDaemon{socket: socket, stdout: stdout, stderr: stderr}
		}
		if command.ProcessState != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no socket after %s\nstdout:\n%s\nstderr:\n%s",
		smokeSocketDeadline, stdout.String(), stderr.String())
	return smokeDaemon{}
}

func writeSmokeConfig(t *testing.T, root string) string {
	t.Helper()
	// The path must look like a Pritunl client to the configuration validator;
	// nothing in this test runs it.
	cli := filepath.Join(root, "pritunl-client")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"schema":                       "hexroute.user-observe.v1",
		"mode":                         "observe-only",
		"observation_interval_seconds": 15,
		"expected_uid":                 os.Geteuid(),
		"profile_id":                   "synthetic-profile",
		"pritunl_cli":                  cli,
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
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "user-observe.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	// Name the failure here rather than letting it surface as a daemon that
	// never opened its socket.
	if _, err := userdaemon.LoadConfig(path); err != nil {
		t.Fatalf("the smoke configuration is not one the daemon accepts: %v", err)
	}
	return path
}
