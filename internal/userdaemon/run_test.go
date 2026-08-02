package userdaemon

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/notification"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

type fixedUserCycler struct {
	summary Summary
}

type fakeIncidentNotifier struct {
	calls   []notification.Input
	outcome notification.Outcome
	err     error
}

func (notifier *fakeIncidentNotifier) Dispatch(
	_ context.Context,
	input notification.Input,
	_ time.Time,
) (notification.Outcome, error) {
	notifier.calls = append(notifier.calls, input)
	return notifier.outcome, notifier.err
}

func (cycler fixedUserCycler) Observe(
	context.Context,
	control.Tick,
	int64,
) Summary {
	return cycler.summary
}

func TestRunCheckValidatesConfigWithoutReadingCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-observe.json")
	if err := os.WriteFile(path, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--check", "--config", path}, &stdout, &stderr)

	if code != 0 ||
		stderr.Len() != 0 ||
		!strings.Contains(stdout.String(), `"event":"startup_check"`) ||
		!strings.Contains(stdout.String(), `"mutation_authority":"none"`) {
		t.Fatalf(
			"Run() code=%d stdout=%q stderr=%q",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunRejectsInvalidConfigWithoutEchoingPath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	canary := "HEXROUTE_CANARY_PRIVATE_CONFIG"

	code := Run(
		[]string{
			"--observe",
			"--config",
			canary,
			"--state",
			"/private/tmp/pritunl-planner.json",
		},
		&stdout,
		&stderr,
	)

	if code != 2 ||
		strings.Contains(stderr.String(), canary) ||
		!strings.Contains(stderr.String(), `"reason":"invalid_configuration"`) {
		t.Fatalf(
			"Run() code=%d stdout=%q stderr=%q",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestObserveLoopPersistsCandidateStateAndEmitsRedactedProposal(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	statePath := filepath.Join(stateDir, stateFileName)
	store, _, err := openSnapshotStore(statePath)
	if err != nil {
		t.Fatalf("openSnapshotStore() error: %v", err)
	}
	snapshot := control.NewSnapshot(control.StateRecovering)
	snapshot.Generation = 3
	snapshot.Attempts = 1
	cycler := fixedUserCycler{summary: Summary{
		Plan: pritunlplan.Plan{
			ObserveOnly: true,
			State:       control.StateRecovering,
			Action:      pritunlplan.ActionReconnect,
			Reason:      pritunlplan.ReasonReconnectAllowed,
			Snapshot:    snapshot,
		},
	}}
	controller, err := operator.NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		control.NewSnapshot(control.StateHealthy),
		control.ReasonNone,
		nil,
		func() control.Tick { return 0 },
	)
	if err != nil {
		t.Fatalf("operator.NewController() error: %v", err)
	}

	if err := observeLoop(
		context.Background(),
		time.Minute,
		true,
		func() control.Tick { return 0 },
		cycler,
		store,
		controller,
		&fakeIncidentNotifier{},
		nil,
		nil,
		logger,
	); err != nil {
		t.Fatalf("observeLoop() error: %v", err)
	}
	logged := output.String()
	if strings.Contains(logged, "synthetic-profile") ||
		!strings.Contains(logged, `"event":"pritunl_reconnect_proposed"`) ||
		!strings.Contains(logged, `"result":"proposed"`) {
		t.Fatalf("observeLoop() output = %q", logged)
	}
	persisted, err := control.LoadSnapshot(statePath)
	if err != nil {
		t.Fatalf("control.LoadSnapshot() error: %v", err)
	}
	if persisted != snapshot {
		t.Fatalf("persisted snapshot = %+v, want %+v", persisted, snapshot)
	}
}

func TestPritunlSafeModeEmitsOneRedactedLocalNotification(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	notifier := &fakeIncidentNotifier{
		outcome: notification.Outcome{
			LocalDelivery: notification.LocalDelivered,
		},
	}
	snapshot := control.NewSnapshot(control.StateSafeMode)
	snapshot.Generation = 7

	dispatchPritunlNotification(
		context.Background(),
		notifier,
		control.StateRecovering,
		snapshot,
		time.Now(),
		logger,
	)
	dispatchPritunlNotification(
		context.Background(),
		notifier,
		control.StateSafeMode,
		snapshot,
		time.Now(),
		logger,
	)

	if len(notifier.calls) != 1 {
		t.Fatalf("notification calls = %d, want 1", len(notifier.calls))
	}
	incident := notifier.calls[0].Incident
	if incident.Status != event.IncidentOpened ||
		incident.Category != event.IncidentRecoveryBudget ||
		incident.Component != control.ComponentPritunl ||
		incident.Generation != 7 {
		t.Fatalf("incident = %+v", incident)
	}
	logged := output.String()
	if !strings.Contains(logged, `"event":"local_notification"`) ||
		!strings.Contains(logged, `"result":"reported"`) ||
		strings.Contains(logged, incident.IncidentID) {
		t.Fatalf("notification log = %q", logged)
	}
}

func TestNotificationFailureDoesNotEscapeOrExposeAdapterError(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	notifier := &fakeIncidentNotifier{
		err: errors.New("HEXROUTE_CANARY_SENSITIVE_VALUE"),
	}
	snapshot := control.NewSnapshot(control.StateSafeMode)
	snapshot.Generation = 3

	dispatchPritunlNotification(
		context.Background(),
		notifier,
		control.StateDegraded,
		snapshot,
		time.Now(),
		logger,
	)

	logged := output.String()
	if !strings.Contains(logged, `"event":"local_notification"`) ||
		!strings.Contains(logged, `"result":"degraded"`) ||
		strings.Contains(logged, "HEXROUTE_CANARY_SENSITIVE_VALUE") {
		t.Fatalf("notification failure log = %q", logged)
	}
}

func TestOpenSnapshotStoreRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	path := filepath.Join(directory, stateFileName)
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink() error: %v", err)
	}

	if _, _, err := openSnapshotStore(path); err == nil {
		t.Fatal("openSnapshotStore() accepted a symlink")
	}
}

func TestUserDaemonProvidesFailClosedPolicyStatusWithoutStaticTrust(t *testing.T) {
	handler, store, err := openUserPolicyHandler(nil)
	if err != nil || store != nil {
		t.Fatalf("openUserPolicyHandler() store=%v error=%v", store, err)
	}
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "user-policy-status",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !response.OK || response.PolicyStatus == nil ||
		response.PolicyStatus.Status.Domain != policy.DomainUser ||
		response.PolicyStatus.Status.State != policy.PolicyNone {
		t.Fatalf("policy status = %+v", response)
	}
}

func TestUserSocketMustRemainBesidePrivateState(t *testing.T) {
	directory := t.TempDir()
	statePath := filepath.Join(directory, stateFileName)
	socketPath := filepath.Join(directory, socketFileName)
	if err := validateUserSocketPath(socketPath, statePath, os.Geteuid()); err != nil {
		t.Fatalf("validateUserSocketPath() error: %v", err)
	}
	for _, path := range []string{
		filepath.Join(t.TempDir(), socketFileName),
		filepath.Join(directory, "arbitrary.sock"),
	} {
		if err := validateUserSocketPath(path, statePath, os.Geteuid()); err == nil {
			t.Fatalf("validateUserSocketPath(%q) accepted", path)
		}
	}
}
