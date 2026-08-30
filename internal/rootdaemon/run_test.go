package rootdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
)

type fixedCycler struct {
	summary Summary
}

func (cycler fixedCycler) Observe(context.Context) Summary {
	return cycler.summary
}

type fixedHeartbeat struct {
	ticks []control.Tick
}

func (heartbeat *fixedHeartbeat) Publish(at control.Tick) error {
	heartbeat.ticks = append(heartbeat.ticks, at)
	return nil
}

func TestRunCheckValidatesConfigWithoutObservingLiveNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root-observe.json")
	if err := os.WriteFile(path, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--check", "--config", path}, &stdout, &stderr)

	if code != 0 || stderr.Len() != 0 ||
		!strings.Contains(stdout.String(), `"event":"startup_check"`) ||
		!strings.Contains(stdout.String(), `"mutation_authority":"none"`) {
		t.Fatalf("Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidConfigWithoutEchoingPath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	canary := "HEXROUTE_CANARY_PRIVATE_CONFIG"

	code := Run([]string{"--observe", "--config", canary}, &stdout, &stderr)

	if code != 2 || strings.Contains(stderr.String(), canary) ||
		!strings.Contains(stderr.String(), `"reason":"invalid_configuration"`) {
		t.Fatalf("Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestObserveLoopEmitsOnlyRedactedProposals(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentDaemon)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	cycler := fixedCycler{summary: Summary{
		State: CycleDegraded,
		Plan: routeplan.Plan{
			ObserveOnly: true,
			Operations: []routeplan.Operation{{
				Kind:        routeplan.OperationEnsureHostRoute,
				Target:      "private-target-name",
				Role:        routeplan.RoleCodexFallback,
				Destination: netip.MustParseAddr("203.0.113.20"),
			}},
		},
	}}
	heartbeat := &fixedHeartbeat{}
	controller, err := operator.NewController(
		ipc.RoleRoot,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentTunnel},
		control.NewSnapshot(control.StateHealthy),
		control.ReasonNone,
		nil,
		func() control.Tick { return 7 },
	)
	if err != nil {
		t.Fatalf("operator.NewController() error: %v", err)
	}

	if err := observeLoop(
		context.Background(),
		time.Minute,
		true,
		func() control.Tick { return 7 },
		cycler,
		heartbeat,
		controller,
		nil,
		nil,
		logger,
		nil,
	); err != nil {
		t.Fatalf("observeLoop() error: %v", err)
	}
	logged := output.String()
	for _, privateValue := range []string{"private-target-name", "203.0.113.20"} {
		if strings.Contains(logged, privateValue) {
			t.Fatalf("observeLoop() leaked %q in %q", privateValue, logged)
		}
	}
	if !strings.Contains(logged, `"event":"codex_fallback_route_proposed"`) ||
		!strings.Contains(logged, `"result":"proposed"`) {
		t.Fatalf("observeLoop() output = %q", logged)
	}
	if len(heartbeat.ticks) != 1 || heartbeat.ticks[0] < 7 {
		t.Fatalf("heartbeat ticks = %v", heartbeat.ticks)
	}
}

func TestRootOperatorSnapshotContainsOnlyBoundedState(t *testing.T) {
	current := control.NewSnapshot(control.StateHealthy)
	next := nextRootOperatorSnapshot(current, Summary{
		State:    CycleDegraded,
		Failures: 2,
	}, 15)
	if next.State != control.StateDegraded ||
		next.ConsecutiveFailures != 2 ||
		next.LastTick != 15 ||
		next.Generation != 1 {
		t.Fatalf("nextRootOperatorSnapshot() = %+v", next)
	}
}

func TestRootDaemonProvidesFailClosedPolicyStatusWithoutStaticTrust(t *testing.T) {
	handler, store, err := openRootPolicyHandler(nil)
	if err != nil || store != nil {
		t.Fatalf("openRootPolicyHandler() store=%v error=%v", store, err)
	}
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "root-policy-status",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !response.OK || response.PolicyStatus == nil ||
		response.PolicyStatus.Status.Domain != policy.DomainRoot ||
		response.PolicyStatus.Status.State != policy.PolicyNone {
		t.Fatalf("policy status = %+v", response)
	}
}

func TestEnsureRootSocketDirectoryCreatesVolatileParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "var-run", "hexroute-observe")

	if err := ensureRootSocketDirectory(path); err != nil {
		t.Fatalf("ensureRootSocketDirectory() error: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat() error: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o711 {
		t.Fatalf("socket directory mode = %v %o", info.Mode(), info.Mode().Perm())
	}
}

func TestEnsureRootSocketDirectoryRejectsWritableParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hexroute-observe")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}

	if err := ensureRootSocketDirectory(path); err != ErrInvalidConfig {
		t.Fatalf("ensureRootSocketDirectory() error = %v, want %v", err, ErrInvalidConfig)
	}
}

// The daemon is bootstrapped with KeepAlive, so an argument only the run
// rejects becomes a restart loop instead of a message to whoever installed it.
// The installer runs --check before bootstrapping for exactly this reason.
func observeConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root-observe.json")
	if err := os.WriteFile(path, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	return path
}

func TestCheckRefusesAQualificationTheRunWouldRefuse(t *testing.T) {
	config := observeConfigFile(t)
	cases := []struct {
		name  string
		chain string
		// session is what an operator typed.
		session string
	}{
		{"a session that is not a UUID", t.TempDir(), "soak-1"},
		{"a chain with no session at all", t.TempDir(), ""},
		{"a session with no chain to record into", "", "49ad4f4c-33e1-42f8-a752-b31be7745836"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := []string{"--check", "--config", config}
			if testCase.chain != "" {
				args = append(args, "--connectivity-qualification", testCase.chain)
			}
			if testCase.session != "" {
				args = append(args,
					"--connectivity-qualification-session", testCase.session)
			}
			if code := Run(args, &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
				t.Fatal("the check passed a configuration the run would refuse")
			}
		})
	}
}

func TestCheckAcceptsAWellFormedQualification(t *testing.T) {
	if code := Run([]string{
		"--check", "--config", observeConfigFile(t),
		"--connectivity-qualification", t.TempDir(),
		"--connectivity-qualification-session", "49ad4f4c-33e1-42f8-a752-b31be7745836",
	}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("the check refused a well-formed qualification: %d", code)
	}
}

// A daemon under KeepAlive that reports one reason for every startup failure
// leaves whoever installed it bisecting by hand. That is not hypothetical:
// diagnosing a crash loop on this host took three rounds of running the binary
// with arguments removed one at a time, because the config, the heartbeat, the
// read model, the qualification chain and the operator socket all refused the
// same way in the log.
//
// Each reason names a subsystem and nothing else. No path, no identity, no
// value — knowing which door was shut is not knowing where it is.
func TestEachStartupRefusalNamesItsOwnSubsystem(t *testing.T) {
	reasonOf := func(t *testing.T, args []string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code == 0 {
			t.Fatalf("expected a refusal, got success: %s", stdout.String())
		}
		// A refusal is reported on the error stream; success is on the other.
		for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
			var event struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Reason != "" {
				return event.Reason
			}
		}
		t.Fatalf("no reason was reported: %s", stderr.String())
		return ""
	}

	config := observeConfigFile(t)

	// A heartbeat whose file is there and cannot be read. This refusal
	// happens before any observer is built, which is what makes it testable
	// on a machine that is not the one being observed.
	corrupt := filepath.Join(t.TempDir(), "control-loop.heartbeat.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reason := reasonOf(t, []string{
		"--observe", "--once", "--config", config, "--heartbeat", corrupt,
	}); reason != string(logging.ReasonHeartbeatUnavailable) {
		t.Fatalf("a broken heartbeat reported %q", reason)
	}

	if reason := reasonOf(t, []string{
		"--check", "--config", config,
		"--connectivity-qualification", t.TempDir(),
		"--connectivity-qualification-session", "not-a-uuid",
	}); reason != string(logging.ReasonQualificationUnavailable) {
		t.Fatalf("a broken qualification reported %q", reason)
	}

	if reason := reasonOf(t, []string{
		"--check", "--config", config,
		"--socket", filepath.Join(t.TempDir(), "nowhere", "hexrouted.sock"),
	}); reason != string(logging.ReasonSocketUnavailable) {
		t.Fatalf("an unusable socket reported %q", reason)
	}

	// And the config itself keeps the reason that was always its own.
	if reason := reasonOf(t, []string{
		"--check", "--config", filepath.Join(t.TempDir(), "absent.json"),
	}); reason != string(logging.ReasonInvalidConfiguration) {
		t.Fatalf("an unreadable config reported %q", reason)
	}
}
