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
	"time"
)

func DefaultRunner() *Runner {
	binaryPath, _ := exec.LookPath("sing-box")
	return &Runner{
		now:         time.Now,
		dialContext: (&net.Dialer{}).DialContext,
		tlsConfig: func(serverName string) *tls.Config {
			return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
		},
		httpClient: func(timeout time.Duration) *http.Client {
			return &http.Client{
				Timeout: timeout,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
		},
		binaryPath:   binaryPath,
		startProcess: startSingBox,
		mkdirTemp:    os.MkdirTemp,
		removeAll:    os.RemoveAll,
		socksFetch:   fetchThroughSOCKS,
	}
}

func (runner *Runner) Probe(ctx context.Context, kind Kind, raw []byte) Result {
	if runner == nil || ctx == nil || runner.now == nil {
		return makeResult(kind, CategoryInternal, time.Now(), time.Now())
	}
	started := runner.now()
	category := CategoryInvalidInput
	switch kind {
	case KindTCP:
		category = runner.probeTCP(ctx, raw)
	case KindTLSFallback:
		category = runner.probeTLSFallback(ctx, raw)
	case KindAuthenticated:
		category = runner.probeAuthenticated(ctx, raw)
	case KindHeartbeat:
		category = runner.probeHeartbeat(ctx, raw)
	default:
		kind = KindUnknown
	}
	return makeResult(kind, category, started, runner.now())
}

func (runner *Runner) probeTCP(parent context.Context, raw []byte) Category {
	var request TCPRequest
	if decodeStrict(raw, &request) != nil || !validEndpoint(request.Endpoint) {
		return CategoryInvalidInput
	}
	timeout, ok := timeoutDuration(request.TimeoutMS)
	if !ok || runner.dialContext == nil {
		return CategoryInvalidInput
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	connection, err := runner.dialContext(ctx, "tcp", endpointAddress(request.Endpoint))
	if err != nil {
		return networkFailure(ctx, err, CategoryUnreachable)
	}
	_ = connection.Close()
	return CategoryOK
}

func (runner *Runner) probeTLSFallback(parent context.Context, raw []byte) Category {
	var request TLSFallbackRequest
	if decodeStrict(raw, &request) != nil || !validEndpoint(request.Endpoint) ||
		!validServerName(request.ServerName) || runner.tlsConfig == nil {
		return CategoryInvalidInput
	}
	timeout, ok := timeoutDuration(request.TimeoutMS)
	if !ok {
		return CategoryInvalidInput
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		Config:    runner.tlsConfig(request.ServerName),
	}
	connection, err := dialer.DialContext(ctx, "tcp", endpointAddress(request.Endpoint))
	if err != nil {
		return networkFailure(ctx, err, CategoryTLS)
	}
	_ = connection.Close()
	return CategoryOK
}

func networkFailure(ctx context.Context, err error, fallback Category) Category {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded) {
		return CategoryTimeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return CategoryTimeout
	}
	return fallback
}

func makeResult(kind Kind, category Category, started, finished time.Time) Result {
	duration := finished.Sub(started)
	if duration < 0 {
		duration = 0
	}
	state := StateFail
	if category == CategoryOK {
		state = StatePass
	}
	return Result{
		Schema:     ResultSchema,
		Probe:      kind,
		State:      state,
		Category:   category,
		DurationMS: duration.Milliseconds(),
	}
}

func RunCLI(args []string, stdin io.Reader, stdout, stderr io.Writer, runner *Runner) int {
	if stdin == nil || stdout == nil || stderr == nil || runner == nil {
		return 1
	}
	kind := Kind("")
	if len(args) == 1 {
		kind = Kind(args[0])
	}
	limited, err := io.ReadAll(io.LimitReader(stdin, RequestMaxBytes+1))
	if err != nil || len(limited) > RequestMaxBytes {
		limited = nil
	}
	result := runner.Probe(context.Background(), kind, limited)
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	if result.State == StatePass {
		return 0
	}
	_, _ = io.WriteString(stderr, "probe failed\n")
	return 1
}
