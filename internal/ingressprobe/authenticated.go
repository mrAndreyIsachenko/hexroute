package ingressprobe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	defaultStatusMin = 200
	defaultStatusMax = 399
	processStopGrace = 2 * time.Second
)

type singBoxConfig struct {
	Log       singBoxLog        `json:"log"`
	Inbounds  []singBoxInbound  `json:"inbounds"`
	Outbounds []singBoxOutbound `json:"outbounds"`
	Route     singBoxRoute      `json:"route"`
}

type singBoxLog struct {
	Level     string `json:"level"`
	Timestamp bool   `json:"timestamp"`
}

type singBoxInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type singBoxOutbound struct {
	Type       string     `json:"type"`
	Tag        string     `json:"tag"`
	Server     string     `json:"server"`
	ServerPort int        `json:"server_port"`
	UUID       string     `json:"uuid"`
	Flow       string     `json:"flow"`
	Network    string     `json:"network"`
	TLS        singBoxTLS `json:"tls"`
}

type singBoxTLS struct {
	Enabled    bool           `json:"enabled"`
	ServerName string         `json:"server_name"`
	MinVersion string         `json:"min_version"`
	UTLS       singBoxUTLS    `json:"utls"`
	Reality    singBoxReality `json:"reality"`
}

type singBoxUTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}

type singBoxReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type singBoxRoute struct {
	Final               string `json:"final"`
	AutoDetectInterface bool   `json:"auto_detect_interface"`
}

type commandProcess struct {
	command  *exec.Cmd
	done     chan struct{}
	stopOnce sync.Once
}

func (runner *Runner) probeAuthenticated(
	parent context.Context,
	raw []byte,
) (category Category) {
	var request AuthenticatedRequest
	if decodeStrict(raw, &request) != nil || !validAuthenticatedRequest(request) ||
		runner.mkdirTemp == nil || runner.removeAll == nil ||
		runner.startProcess == nil || runner.socksFetch == nil {
		return CategoryInvalidInput
	}
	timeout, _ := timeoutDuration(request.TimeoutMS)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	port, err := unusedLoopbackPort()
	if err != nil {
		return CategoryInternal
	}
	encoded, err := renderSingBoxConfig(request, port)
	if err != nil {
		return CategoryInvalidInput
	}
	directory, err := runner.mkdirTemp("", "hexroute-ingress-probe-*")
	if err != nil {
		return CategoryInternal
	}
	category = CategoryInternal
	defer func() {
		if err := runner.removeAll(directory); err != nil {
			category = CategoryInternal
		}
	}()
	if err := os.Chmod(directory, 0o700); err != nil {
		return category
	}
	configPath := filepath.Join(directory, "sing-box.json")
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return category
	}
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o600 {
		return category
	}
	if runner.binaryPath == "" {
		return CategoryDependency
	}
	process, err := runner.startProcess(ctx, runner.binaryPath, configPath)
	if err != nil || process == nil {
		return CategoryDependency
	}
	defer process.Stop()
	address := net.JoinHostPort("127.0.0.1", formatPort(uint16(port)))
	if !waitForLoopback(ctx, address, process.Done()) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return CategoryTimeout
		}
		return CategoryAuthenticatedTransport
	}
	statusMin, statusMax := expectedStatusRange(request)
	if err := runner.socksFetch(
		ctx,
		address,
		request.TargetURL,
		statusMin,
		statusMax,
	); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			return CategoryTimeout
		}
		return CategoryAuthenticatedTransport
	}
	return CategoryOK
}

func validAuthenticatedRequest(request AuthenticatedRequest) bool {
	if !validEndpoint(request.Endpoint) || !validServerName(request.ServerName) ||
		!validUserID(request.UserID) ||
		!validRealityPublicKey(request.RealityPublicKey) ||
		!validRealityShortID(request.RealityShortID) ||
		!validHTTPSURL(request.TargetURL) {
		return false
	}
	if _, ok := timeoutDuration(request.TimeoutMS); !ok {
		return false
	}
	minimum, maximum := expectedStatusRange(request)
	return minimum >= 100 && maximum <= 599 && minimum <= maximum
}

func expectedStatusRange(request AuthenticatedRequest) (int, int) {
	minimum := int(request.ExpectedStatusMin)
	maximum := int(request.ExpectedStatusMax)
	if minimum == 0 {
		minimum = defaultStatusMin
	}
	if maximum == 0 {
		maximum = defaultStatusMax
	}
	return minimum, maximum
}

func renderSingBoxConfig(request AuthenticatedRequest, port int) ([]byte, error) {
	if !validAuthenticatedRequest(request) || port < 1 || port > 65535 {
		return nil, errors.New("invalid authenticated probe")
	}
	return json.Marshal(singBoxConfig{
		Log: singBoxLog{Level: "error", Timestamp: false},
		Inbounds: []singBoxInbound{{
			Type:       "socks",
			Tag:        "probe-in",
			Listen:     "127.0.0.1",
			ListenPort: port,
		}},
		Outbounds: []singBoxOutbound{{
			Type:       "vless",
			Tag:        "probe-out",
			Server:     request.Endpoint.Host,
			ServerPort: int(request.Endpoint.Port),
			UUID:       request.UserID,
			Flow:       "xtls-rprx-vision",
			Network:    "tcp",
			TLS: singBoxTLS{
				Enabled:    true,
				ServerName: request.ServerName,
				MinVersion: "1.2",
				UTLS: singBoxUTLS{
					Enabled:     true,
					Fingerprint: "chrome",
				},
				Reality: singBoxReality{
					Enabled:   true,
					PublicKey: request.RealityPublicKey,
					ShortID:   request.RealityShortID,
				},
			},
		}},
		Route: singBoxRoute{Final: "probe-out", AutoDetectInterface: true},
	})
}

func unusedLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func startSingBox(
	ctx context.Context,
	binaryPath string,
	configPath string,
) (probeProcess, error) {
	command := exec.CommandContext(
		ctx,
		binaryPath,
		singBoxCommandArguments(configPath)...,
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	process := &commandProcess{command: command, done: make(chan struct{})}
	if err := command.Start(); err != nil {
		return nil, err
	}
	go func() {
		_ = command.Wait()
		close(process.done)
	}()
	return process, nil
}

func singBoxCommandArguments(configPath string) []string {
	return []string{"run", "--disable-color", "-c", configPath}
}

func (process *commandProcess) Done() <-chan struct{} {
	return process.done
}

func (process *commandProcess) Stop() {
	process.stopOnce.Do(func() {
		if process.command == nil || process.command.Process == nil {
			return
		}
		_ = process.command.Process.Signal(os.Interrupt)
		select {
		case <-process.done:
			return
		case <-time.After(processStopGrace):
		}
		_ = process.command.Process.Kill()
		<-process.done
	})
}

func waitForLoopback(ctx context.Context, address string, done <-chan struct{}) bool {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := (&net.Dialer{Timeout: 50 * time.Millisecond}).DialContext(
			ctx,
			"tcp",
			address,
		)
		if err == nil {
			_ = connection.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-done:
			return false
		case <-ticker.C:
		}
	}
}

func fetchThroughSOCKS(
	ctx context.Context,
	proxyAddress string,
	targetURL string,
	statusMin int,
	statusMax int,
) error {
	dialer, err := proxy.SOCKS5(
		"tcp",
		proxyAddress,
		nil,
		&net.Dialer{Timeout: 5 * time.Second},
	)
	if err != nil {
		return err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return errors.New("SOCKS dialer has no context support")
	}
	transport := &http.Transport{
		DialContext: contextDialer.DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < statusMin || response.StatusCode > statusMax {
		return errors.New("unexpected canary status")
	}
	return nil
}
