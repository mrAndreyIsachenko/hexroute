package operator

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

func TestControllerReportsBoundedStatusAndDiagnostics(t *testing.T) {
	snapshot := safeModeSnapshot()
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			t.Fatal("read-only request invoked resume")
			return control.Snapshot{}, nil
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatalf("NewController() error: %v", err)
	}

	status := controller.Handle(ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "status-1",
		Action:    ipc.ActionStatus,
	})
	if !status.OK ||
		status.Status == nil ||
		status.Status.Role != ipc.RoleUser ||
		!status.Status.SafeMode ||
		status.Status.Generation != snapshot.Generation {
		t.Fatalf("status response = %+v", status)
	}

	diagnostics := controller.Handle(ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "diagnostics-1",
		Action:    ipc.ActionExportDiagnostics,
	})
	if !diagnostics.OK ||
		diagnostics.Diagnostics == nil ||
		diagnostics.Diagnostics.Attempts != snapshot.Attempts ||
		diagnostics.Diagnostics.LastReason != control.ReasonRecoveryBudget {
		t.Fatalf("diagnostics response = %+v", diagnostics)
	}
	assertNoSecretBearingFields(t, reflect.TypeOf(*diagnostics.Diagnostics))
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	for _, forbidden := range []string{
		"pin",
		"otp",
		"secret",
		"private_key",
		"profile_id",
		"server_name",
		"address",
		"command",
	} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("diagnostics contains forbidden field %q: %s", forbidden, encoded)
		}
	}
}

func TestControllerResumeRequiresRoleSafeModeAndExactGeneration(t *testing.T) {
	tests := []struct {
		name       string
		role       ipc.DaemonRole
		targets    []control.Component
		request    ipc.Request
		state      control.State
		wantError  ipc.ErrorCode
		wantResume bool
	}{
		{
			name:      "stale generation",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentPritunl, 6),
			state:     control.StateSafeMode,
			wantError: ipc.ErrorStaleGeneration,
		},
		{
			name:      "not in safe mode",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentPritunl, 7),
			state:     control.StateHealthy,
			wantError: ipc.ErrorPrecondition,
		},
		{
			name:      "root target through user socket",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentTunnel, 7),
			state:     control.StateSafeMode,
			wantError: ipc.ErrorUnauthorized,
		},
		{
			name:       "explicit user resume",
			role:       ipc.RoleUser,
			targets:    []control.Component{control.ComponentPritunl},
			request:    resumeRequest(control.ComponentPritunl, 7),
			state:      control.StateSafeMode,
			wantResume: true,
		},
		{
			name:       "explicit root resume",
			role:       ipc.RoleRoot,
			targets:    []control.Component{control.ComponentTunnel},
			request:    resumeRequest(control.ComponentTunnel, 7),
			state:      control.StateSafeMode,
			wantResume: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := safeModeSnapshot()
			snapshot.State = test.state
			if test.state != control.StateSafeMode {
				snapshot.SafeUntil = 0
			}
			resumeCalls := 0
			controller, err := NewController(
				test.role,
				ipc.ModeObserveOnly,
				test.targets,
				snapshot,
				control.ReasonRecoveryBudget,
				func(expected uint64, at control.Tick) (control.Snapshot, error) {
					resumeCalls++
					if expected != snapshot.Generation || at != 100 {
						t.Fatalf("resume(%d, %d)", expected, at)
					}
					updated := snapshot
					updated.State = control.StateDegraded
					updated.Generation++
					updated.Attempts = 0
					updated.RecoveringSince = 0
					updated.NextActionAt = at
					updated.SafeUntil = 0
					return updated, nil
				},
				func() control.Tick { return 100 },
			)
			if err != nil {
				t.Fatalf("NewController() error: %v", err)
			}

			response := controller.Handle(test.request)
			if response.Error != test.wantError {
				t.Fatalf("response error = %q, want %q", response.Error, test.wantError)
			}
			if test.wantResume {
				if !response.OK ||
					response.Resume == nil ||
					response.Resume.PreviousState != control.StateSafeMode ||
					response.Resume.State != control.StateDegraded ||
					response.Resume.Generation != 8 ||
					resumeCalls != 1 {
					t.Fatalf("resume response = %+v calls=%d", response, resumeCalls)
				}
				status := controller.Handle(ipc.Request{
					Version:   ipc.ProtocolVersion,
					RequestID: "status-after",
					Action:    ipc.ActionStatus,
				})
				if status.Status == nil || status.Status.SafeMode {
					t.Fatalf("status after resume = %+v", status)
				}
			} else if resumeCalls != 0 {
				t.Fatalf("rejected request invoked resume %d times", resumeCalls)
			}
		})
	}
}

func TestControllerRejectsStaleUpdatesAndResumeFailures(t *testing.T) {
	snapshot := safeModeSnapshot()
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			return control.Snapshot{}, control.ErrStaleGeneration
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatalf("NewController() error: %v", err)
	}

	stale := snapshot
	stale.Generation--
	if err := controller.Update(stale, control.ReasonProbeFailed); !errors.Is(
		err,
		control.ErrStaleGeneration,
	) {
		t.Fatalf("Update() error = %v, want stale generation", err)
	}
	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation))
	if response.Error != ipc.ErrorStaleGeneration {
		t.Fatalf("resume error = %q, want %q", response.Error, ipc.ErrorStaleGeneration)
	}
}

func safeModeSnapshot() control.Snapshot {
	return control.Snapshot{
		SchemaVersion:       control.SnapshotSchemaVersion,
		Generation:          7,
		State:               control.StateSafeMode,
		ConsecutiveFailures: 5,
		Attempts:            3,
		LastTick:            90,
		RecoveringSince:     80,
		NextActionAt:        95,
		SafeUntil:           700,
	}
}

func resumeRequest(target control.Component, generation uint64) ipc.Request {
	return ipc.Request{
		Version:            ipc.ProtocolVersion,
		RequestID:          "resume-1",
		Action:             ipc.ActionResumeTarget,
		Target:             target,
		ExpectedGeneration: generation,
	}
}

func assertNoSecretBearingFields(t *testing.T, value reflect.Type) {
	t.Helper()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, forbidden := range []string{
			"pin",
			"otp",
			"secret",
			"key",
			"profile",
			"address",
			"server",
			"command",
			"hostname",
		} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("diagnostics exposes secret-bearing field %s", field.Name)
			}
		}
		if field.Type.Kind() == reflect.Struct {
			assertNoSecretBearingFields(t, field.Type)
		}
	}
}
