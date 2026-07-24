package cloudruntime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

type heartbeatFunc func(context.Context) error

func (function heartbeatFunc) Run(ctx context.Context) error {
	return function(ctx)
}

func TestWorkerRuntimeRunsImmediatelyWithoutOverlappingJobs(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentIngest)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	var (
		mu            sync.Mutex
		runs          int
		active        int
		maxConcurrent int
	)
	runtime := &workerRuntime{
		heartbeat: heartbeatFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
		logger: logger,
		jobs: []workerJob{{
			event:    logging.EventCloudReconcile,
			interval: 2 * time.Millisecond,
			timeout:  50 * time.Millisecond,
			run: func(context.Context) error {
				mu.Lock()
				runs++
				active++
				if active > maxConcurrent {
					maxConcurrent = active
				}
				mu.Unlock()
				time.Sleep(8 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
				return nil
			},
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	if err := runtime.run(ctx, time.Second); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if runs < 2 || maxConcurrent != 1 {
		t.Fatalf("runs=%d max_concurrent=%d", runs, maxConcurrent)
	}
}

func TestWorkerRuntimeKeepsJobsRetryingButFailsOnHeartbeat(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentIngest)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	jobContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &workerRuntime{
		heartbeat: heartbeatFunc(func(context.Context) error {
			return errors.New("database unavailable")
		}),
		logger: logger,
		jobs: []workerJob{{
			event:    logging.EventCloudAlertQueue,
			interval: time.Hour,
			timeout:  time.Second,
			run: func(context.Context) error {
				return errors.New("outbox unavailable")
			},
		}},
	}
	if err := runtime.run(jobContext, time.Second); !errors.Is(err, ErrWorkerRuntime) {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(output.String(), "cloud_heartbeat") ||
		strings.Contains(output.String(), "database unavailable") ||
		strings.Contains(output.String(), "outbox unavailable") {
		t.Fatalf("worker log = %q", output.String())
	}
}

func TestWorkerRuntimeTreatsUnexpectedHeartbeatCompletionAsFatal(t *testing.T) {
	logger, err := logging.New(&bytes.Buffer{}, logging.ComponentIngest)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	runtime := &workerRuntime{
		heartbeat: heartbeatFunc(func(context.Context) error {
			return nil
		}),
		logger: logger,
	}
	if err := runtime.run(
		context.Background(),
		time.Second,
	); !errors.Is(err, ErrWorkerRuntime) {
		t.Fatalf("run() error = %v", err)
	}
}
