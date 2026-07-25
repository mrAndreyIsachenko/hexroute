package cloudruntime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

func TestRunDispatchesValidatedAPIWithoutLoggingConfiguration(t *testing.T) {
	values := validAPIEnvironment()
	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
		called bool
	)
	exitCode := run(
		context.Background(),
		[]string{"api"},
		mapEnvironment(values),
		&stdout,
		&stderr,
		func(
			_ context.Context,
			config APIConfig,
			_ *logging.Logger,
		) error {
			called = true
			if config.ExpectedHost != "status.example" {
				t.Fatalf("config = %+v", config)
			}
			return nil
		},
		nil,
		nil,
	)
	if exitCode != 0 || !called || stderr.Len() != 0 {
		t.Fatalf(
			"run(api) exit=%d called=%t stderr=%q",
			exitCode,
			called,
			stderr.String(),
		)
	}
	for _, value := range values {
		if strings.Contains(stdout.String(), value) {
			t.Fatalf("stdout leaked environment value %q", value)
		}
	}
}

func TestRunRejectsInvalidConfigAndRuntimeWithBoundedLogs(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name      string
		values    map[string]string
		runner    apiRunner
		eventName string
	}{
		{
			name: "configuration",
			values: map[string]string{
				"HEXROUTE_BOOTSTRAP_SECRET": secret,
			},
			runner:    func(context.Context, APIConfig, *logging.Logger) error { return nil },
			eventName: "argument_rejected",
		},
		{
			name:   "runtime",
			values: validAPIEnvironment(),
			runner: func(context.Context, APIConfig, *logging.Logger) error {
				return errors.New("postgres://secret@database/internal")
			},
			eventName: "cloud_api_stopped",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := run(
				context.Background(),
				[]string{"api"},
				mapEnvironment(test.values),
				&stdout,
				&stderr,
				test.runner,
				nil,
				nil,
			)
			if exitCode != 1 ||
				!strings.Contains(stderr.String(), test.eventName) ||
				strings.Contains(stderr.String(), secret) ||
				strings.Contains(stderr.String(), "postgres://") {
				t.Fatalf(
					"run(%s) exit=%d stderr=%q",
					test.name,
					exitCode,
					stderr.String(),
				)
			}
		})
	}
}

func TestRunDispatchesValidatedWorkerWithoutLoggingConfiguration(t *testing.T) {
	values := validWorkerEnvironment()
	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
		called bool
	)
	exitCode := run(
		context.Background(),
		[]string{"worker"},
		mapEnvironment(values),
		&stdout,
		&stderr,
		nil,
		func(
			_ context.Context,
			config WorkerConfig,
			_ *logging.Logger,
		) error {
			called = true
			if config.WorkerName != "primary" {
				t.Fatalf("config = %+v", config)
			}
			return nil
		},
		nil,
	)
	if exitCode != 0 || !called || stderr.Len() != 0 {
		t.Fatalf(
			"run(worker) exit=%d called=%t stderr=%q",
			exitCode,
			called,
			stderr.String(),
		)
	}
	for _, value := range values {
		if strings.Contains(stdout.String(), value) {
			t.Fatalf("stdout leaked environment value %q", value)
		}
	}
}

func TestRunDispatchesAppPlatformComponentWithoutOverridingEntrypoint(t *testing.T) {
	values := validAPIEnvironment()
	values["HEXROUTE_COMPONENT"] = "api"
	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
		called bool
	)
	exitCode := run(
		context.Background(),
		nil,
		mapEnvironment(values),
		&stdout,
		&stderr,
		func(
			_ context.Context,
			config APIConfig,
			_ *logging.Logger,
		) error {
			called = true
			return config.Validate()
		},
		nil,
		nil,
	)
	if exitCode != 0 || !called || stderr.Len() != 0 {
		t.Fatalf(
			"run(app-platform api) exit=%d called=%t stderr=%q",
			exitCode,
			called,
			stderr.String(),
		)
	}
}

func TestRunPreservesContainerCheckAndRejectsUnsupportedModes(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		values map[string]string
		exit   int
	}{
		{name: "explicit check", args: []string{"--check"}, exit: 0},
		{name: "default check", args: nil, exit: 0},
		{name: "unsupported argument", args: []string{"unsupported"}, exit: 2},
		{
			name:   "unsupported component",
			values: map[string]string{"HEXROUTE_COMPONENT": "unsupported"},
			exit:   2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := run(
				context.Background(),
				test.args,
				mapEnvironment(test.values),
				&stdout,
				&stderr,
				nil,
				nil,
				nil,
			)
			if exitCode != test.exit {
				t.Fatalf("run(%v) exit=%d, want %d", test.args, exitCode, test.exit)
			}
		})
	}
}

func TestRunDispatchesValidatedMigrationWithoutLoggingSecrets(t *testing.T) {
	values := validMigrationEnvironment()
	var stdout, stderr bytes.Buffer
	called := false
	exitCode := run(
		context.Background(),
		[]string{"migrate"},
		mapEnvironment(values),
		&stdout,
		&stderr,
		nil,
		nil,
		func(
			_ context.Context,
			config MigrationConfig,
			_ *logging.Logger,
		) error {
			called = true
			if config.BootstrapUsername != "operator" ||
				config.BootstrapDisplayName != "Operator" {
				t.Fatalf("config = %+v", config)
			}
			return nil
		},
	)
	if exitCode != 0 || !called || stderr.Len() != 0 {
		t.Fatalf("run(migrate) exit=%d called=%t stderr=%q", exitCode, called, stderr.String())
	}
	if strings.Contains(stdout.String(), values["HEXROUTE_MIGRATOR_DATABASE_URL"]) {
		t.Fatalf("stdout leaked migration database URL")
	}
}
