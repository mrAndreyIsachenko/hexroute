package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestStatusQueriesBothFixedRoleSockets(t *testing.T) {
	var calls []string
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		calls = append(calls, path+":"+string(request.Action))
		role := ipc.RoleRoot
		if path == "/safe/user.sock" {
			role = ipc.RoleUser
		}
		status := ipc.Status{
			Role:       role,
			Mode:       ipc.ModeObserveOnly,
			State:      control.StateHealthy,
			Generation: 4,
			Policy:     policyStatusForRole(role),
		}
		return ipc.Response{
			Version:   ipc.ProtocolVersion,
			RequestID: request.RequestID,
			OK:        true,
			Status:    &status,
		}, nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"status"}, &stdout, &stderr, config)

	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("Run() code=%d stderr=%q", code, stderr.String())
	}
	if len(calls) != 2 ||
		calls[0] != "/safe/root.sock:status" ||
		calls[1] != "/safe/user.sock:status" {
		t.Fatalf("calls = %#v", calls)
	}
	var output commandOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if output.Schema != outputSchema ||
		output.Command != "status" ||
		len(output.Results) != 2 ||
		output.Results[0].Role != ipc.RoleRoot ||
		output.Results[1].Role != ipc.RoleUser {
		t.Fatalf("output = %+v", output)
	}
	if strings.Count(stdout.String(), `"policy"`) != 2 ||
		!strings.Contains(stdout.String(), `"manifest_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("status omitted redacted policy projection: %s", stdout.String())
	}
}

func policyStatusForRole(role ipc.DaemonRole) *ipc.PolicyStatusResult {
	domain := policy.DomainRoot
	if role == ipc.RoleUser {
		domain = policy.DomainUser
	}
	return &ipc.PolicyStatusResult{
		Status: policy.Status{
			Schema: policy.PolicyStatusSchema, Domain: domain,
			State: policy.PolicyActive, BundleGeneration: 7,
			PolicyGeneration: 5,
			ManifestSHA256:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ActivatedAt:      "2030-01-01T00:00:00Z", Reason: policy.ReasonNone,
		},
		AuthorizationSuspension: policy.AuthorizationSuspension{
			Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
		},
	}
}

func TestDiagnosticsOutputIsTypedAndRedacted(t *testing.T) {
	canary := "HEXROUTE_CANARY_TOTP_SEED"
	config := testConfig(func(
		_ context.Context,
		_ string,
		request ipc.Request,
	) (ipc.Response, error) {
		if request.Action != ipc.ActionExportDiagnostics {
			t.Fatalf("action = %q, want diagnostics", request.Action)
		}
		diagnostics := ipc.Diagnostics{
			Status: ipc.Status{
				Role:       ipc.RoleUser,
				Mode:       ipc.ModeObserveOnly,
				State:      control.StateSafeMode,
				Generation: 8,
				SafeMode:   true,
			},
			ConsecutiveFailures: 3,
			Attempts:            2,
			LastTick:            100,
			SafeUntil:           700,
			LastReason:          control.ReasonRecoveryBudget,
		}
		return ipc.Response{
			Version:     ipc.ProtocolVersion,
			RequestID:   request.RequestID,
			OK:          true,
			Diagnostics: &diagnostics,
		}, nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(
		[]string{"diagnostics", "--scope", "user"},
		&stdout,
		&stderr,
		config,
	)

	if code != 0 {
		t.Fatalf("Run() code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), canary) ||
		strings.Contains(strings.ToLower(stdout.String()), "profile_id") ||
		strings.Contains(strings.ToLower(stdout.String()), "server_name") {
		t.Fatalf("diagnostics leaked forbidden data: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"safe_mode":true`) ||
		!strings.Contains(stdout.String(), `"last_reason":"recovery_budget_exhausted"`) {
		t.Fatalf("diagnostics output = %s", stdout.String())
	}
}

func TestResumeRequiresExplicitScopeTargetAndGeneration(t *testing.T) {
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		if path != "/safe/user.sock" ||
			request.Action != ipc.ActionResumeTarget ||
			request.Target != control.ComponentPritunl ||
			request.ExpectedGeneration != 12 {
			t.Fatalf("resume request path=%q request=%+v", path, request)
		}
		result := ipc.ResumeResult{
			Role:               ipc.RoleUser,
			Target:             control.ComponentPritunl,
			PreviousState:      control.StateSafeMode,
			State:              control.StateDegraded,
			PreviousGeneration: 12,
			Generation:         13,
		}
		return ipc.Response{
			Version:   ipc.ProtocolVersion,
			RequestID: request.RequestID,
			OK:        true,
			Resume:    &result,
		}, nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(
		[]string{
			"resume",
			"--scope", "user",
			"--target", "pritunl",
			"--generation", "12",
		},
		&stdout,
		&stderr,
		config,
	)
	if code != 0 || !strings.Contains(stdout.String(), `"generation":13`) {
		t.Fatalf("Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	for _, args := range [][]string{
		{"resume", "--scope", "user", "--target", "pritunl"},
		{"resume", "--scope", "user", "--target", "tunnel", "--generation", "12"},
		{"resume", "--scope", "all", "--target", "pritunl", "--generation", "12"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(args, &stdout, &stderr, config); code != 2 {
			t.Fatalf("Run(%v) code=%d, want 2", args, code)
		}
	}
}

func TestTransportErrorsAndRejectedArgumentsDoNotLeakInput(t *testing.T) {
	canary := "HEXROUTE_CANARY_REALITY_PRIVATE_KEY"
	config := testConfig(func(
		context.Context,
		string,
		ipc.Request,
	) (ipc.Response, error) {
		return ipc.Response{}, errors.New(canary)
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(
		[]string{"status", "--scope", "root"},
		&stdout,
		&stderr,
		config,
	)
	if code != 1 ||
		strings.Contains(stdout.String(), canary) ||
		strings.Contains(stderr.String(), canary) {
		t.Fatalf("transport output code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{canary}, &stdout, &stderr, config)
	if code != 2 ||
		strings.Contains(stdout.String(), canary) ||
		strings.Contains(stderr.String(), canary) {
		t.Fatalf("argument output code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func testConfig(roundTrip RoundTripFunc) Config {
	sequence := 0
	return Config{
		RootSocket: "/safe/root.sock",
		UserSocket: "/safe/user.sock",
		RoundTrip:  roundTrip,
		RequestID: func() (string, error) {
			sequence++
			return "request-" + string(rune('0'+sequence)), nil
		},
	}
}
