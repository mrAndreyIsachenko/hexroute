package userdaemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

type fixedUserCycler struct {
	summary Summary
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
