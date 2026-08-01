package ingressprobe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	listener net.Listener
	done     chan struct{}
	once     sync.Once
	stopped  bool
}

func (process *fakeProcess) Done() <-chan struct{} { return process.done }

func (process *fakeProcess) Stop() {
	process.once.Do(func() {
		process.stopped = true
		if process.listener != nil {
			_ = process.listener.Close()
		}
		close(process.done)
	})
}

func TestAuthenticatedProbeUsesLoopbackPrivateConfigAndCleansUp(t *testing.T) {
	runner := DefaultRunner()
	runner.binaryPath = "/synthetic/sing-box"
	var capturedPath string
	var capturedConfig singBoxConfig
	var process *fakeProcess
	runner.startProcess = func(_ context.Context, binaryPath, configPath string) (probeProcess, error) {
		if binaryPath != runner.binaryPath {
			t.Fatalf("binary path = %q", binaryPath)
		}
		capturedPath = configPath
		info, err := os.Stat(configPath)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %v, error = %v", info, err)
		}
		directoryInfo, err := os.Stat(filepath.Dir(configPath))
		if err != nil || directoryInfo.Mode().Perm() != 0o700 {
			t.Fatalf("directory mode = %v, error = %v", directoryInfo, err)
		}
		encoded, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if err := json.Unmarshal(encoded, &capturedConfig); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(
			capturedConfig.Inbounds[0].Listen,
			formatPort(uint16(capturedConfig.Inbounds[0].ListenPort)),
		))
		if err != nil {
			t.Fatalf("listen fake SOCKS: %v", err)
		}
		process = &fakeProcess{listener: listener, done: make(chan struct{})}
		go acceptAndClose(listener)
		return process, nil
	}
	request := validAuthenticatedFixture()
	runner.socksFetch = func(
		_ context.Context,
		proxyAddress string,
		targetURL string,
		statusMin int,
		statusMax int,
	) error {
		if !strings.HasPrefix(proxyAddress, "127.0.0.1:") ||
			targetURL != request.TargetURL || statusMin != 200 || statusMax != 399 {
			t.Fatalf("unexpected SOCKS fetch contract")
		}
		return nil
	}

	result := runner.Probe(
		context.Background(),
		KindAuthenticated,
		mustJSON(t, request),
	)
	if result.State != StatePass || process == nil || !process.stopped {
		t.Fatalf("result = %+v, process = %+v", result, process)
	}
	if capturedConfig.Inbounds[0].Type != "socks" ||
		capturedConfig.Inbounds[0].Listen != "127.0.0.1" ||
		capturedConfig.Outbounds[0].Type != "vless" ||
		capturedConfig.Outbounds[0].TLS.Reality.PublicKey != request.RealityPublicKey ||
		capturedConfig.Route.Final != "probe-out" {
		t.Fatalf("rendered config contract mismatch: %+v", capturedConfig)
	}
	if _, err := os.Stat(capturedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary config still exists: %v", err)
	}
}

func TestAuthenticatedProbeCleansUpOnDependencyFailureAndTimeout(t *testing.T) {
	request := validAuthenticatedFixture()
	t.Run("missing dependency", func(t *testing.T) {
		runner := DefaultRunner()
		runner.binaryPath = ""
		var directory string
		runner.mkdirTemp = func(root, pattern string) (string, error) {
			created, err := os.MkdirTemp(root, pattern)
			directory = created
			return created, err
		}
		result := runner.Probe(context.Background(), KindAuthenticated, mustJSON(t, request))
		if result.Category != CategoryDependency {
			t.Fatalf("result = %+v", result)
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary directory still exists: %v", err)
		}
	})

	t.Run("readiness timeout", func(t *testing.T) {
		runner := DefaultRunner()
		runner.binaryPath = "/synthetic/sing-box"
		var process *fakeProcess
		runner.startProcess = func(context.Context, string, string) (probeProcess, error) {
			process = &fakeProcess{done: make(chan struct{})}
			return process, nil
		}
		request.TimeoutMS = 100
		result := runner.Probe(context.Background(), KindAuthenticated, mustJSON(t, request))
		if result.Category != CategoryTimeout || process == nil || !process.stopped {
			t.Fatalf("result = %+v, process = %+v", result, process)
		}
	})
}

func TestAuthenticatedProbeFailsWhenPrivateConfigCannotBeRemoved(t *testing.T) {
	runner := DefaultRunner()
	runner.binaryPath = ""
	removeAll := runner.removeAll
	runner.removeAll = func(path string) error {
		_ = removeAll(path)
		return errors.New("synthetic cleanup failure")
	}
	result := runner.Probe(
		context.Background(),
		KindAuthenticated,
		mustJSON(t, validAuthenticatedFixture()),
	)
	if result.Category != CategoryInternal {
		t.Fatalf("result = %+v", result)
	}
}

func TestCLIOutputNeverContainsAuthenticatedRequestValues(t *testing.T) {
	request := validAuthenticatedFixture()
	runner := DefaultRunner()
	runner.binaryPath = ""
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunCLI(
		[]string{string(KindAuthenticated)},
		bytes.NewReader(mustJSON(t, request)),
		&stdout,
		&stderr,
		runner,
	)
	if exitCode != 1 || !strings.Contains(stdout.String(), string(CategoryDependency)) ||
		stderr.String() != "probe failed\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, sensitive := range []string{
		request.Endpoint.Host,
		request.ServerName,
		request.UserID,
		request.RealityPublicKey,
		request.RealityShortID,
		request.TargetURL,
	} {
		if strings.Contains(stdout.String(), sensitive) || strings.Contains(stderr.String(), sensitive) {
			t.Fatalf("request value leaked: %q", sensitive)
		}
	}
}

func TestUnknownCommandIsNotReflectedInOutput(t *testing.T) {
	secretCommand := "sensitive-command-value"
	runner := DefaultRunner()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunCLI(
		[]string{secretCommand},
		strings.NewReader(`{}`),
		&stdout,
		&stderr,
		runner,
	)
	if exitCode != 1 || strings.Contains(stdout.String(), secretCommand) ||
		!strings.Contains(stdout.String(), `"probe":"unknown"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestSingBoxArgumentsContainOnlyTemporaryConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sing-box.json")
	arguments := singBoxCommandArguments(path)
	if len(arguments) != 4 || arguments[0] != "run" ||
		arguments[1] != "--disable-color" || arguments[2] != "-c" ||
		arguments[3] != path {
		t.Fatalf("arguments = %#v", arguments)
	}
	request := validAuthenticatedFixture()
	joined := strings.Join(arguments, " ")
	for _, value := range []string{
		request.Endpoint.Host,
		request.ServerName,
		request.UserID,
		request.RealityPublicKey,
		request.RealityShortID,
		request.TargetURL,
	} {
		if strings.Contains(joined, value) {
			t.Fatalf("request value appeared in process arguments")
		}
	}
}

func TestRenderedConfigIsAcceptedByInstalledSingBox(t *testing.T) {
	binaryPath, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	encoded, err := renderSingBoxConfig(validAuthenticatedFixture(), 2081)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sing-box.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	command := exec.Command(binaryPath, "check", "--disable-color", "-c", path)
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Run(); err != nil {
		t.Fatalf("sing-box check failed: %v", err)
	}
}

func validAuthenticatedFixture() AuthenticatedRequest {
	publicKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	shortID := hex.EncodeToString(bytes.Repeat([]byte{0x24}, 8))
	return AuthenticatedRequest{
		Endpoint:         Endpoint{Host: "ingress.invalid", Port: 443},
		ServerName:       "fallback.invalid",
		UserID:           "11111111-1111-4111-8111-111111111111",
		RealityPublicKey: publicKey,
		RealityShortID:   shortID,
		TargetURL:        "https://canary.invalid/health",
		TimeoutMS:        1000,
	}
}

func TestAuthenticatedFetchFailureIsRedactedCategory(t *testing.T) {
	runner := DefaultRunner()
	runner.binaryPath = "/synthetic/sing-box"
	runner.startProcess = func(_ context.Context, _ string, configPath string) (probeProcess, error) {
		encoded, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
		var config singBoxConfig
		if err := json.Unmarshal(encoded, &config); err != nil {
			return nil, err
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(
			"127.0.0.1",
			formatPort(uint16(config.Inbounds[0].ListenPort)),
		))
		if err != nil {
			return nil, err
		}
		process := &fakeProcess{listener: listener, done: make(chan struct{})}
		go acceptAndClose(listener)
		return process, nil
	}
	runner.socksFetch = func(context.Context, string, string, int, int) error {
		return errors.New("synthetic secret-bearing dependency detail")
	}
	result := runner.Probe(
		context.Background(),
		KindAuthenticated,
		mustJSON(t, validAuthenticatedFixture()),
	)
	if result.Category != CategoryAuthenticatedTransport {
		t.Fatalf("result = %+v", result)
	}
}

func TestStrictInputRejectsUnknownFields(t *testing.T) {
	runner := DefaultRunner()
	result := runner.Probe(
		context.Background(),
		KindTCP,
		[]byte(`{"endpoint":{"host":"example.invalid","port":443},"timeout_ms":500,"extra":true}`),
	)
	if result.Category != CategoryInvalidInput {
		t.Fatalf("result = %+v", result)
	}
}

func TestResultDurationCannotBecomeNegative(t *testing.T) {
	result := makeResult(KindTCP, CategoryOK, time.Unix(2, 0), time.Unix(1, 0))
	if result.DurationMS != 0 {
		t.Fatalf("duration = %d", result.DurationMS)
	}
}
