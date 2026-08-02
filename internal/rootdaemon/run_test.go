package rootdaemon

import (
	"bytes"
	"context"
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
