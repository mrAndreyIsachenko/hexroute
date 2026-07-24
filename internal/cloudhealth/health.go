package cloudhealth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	MaxWorkerNameBytes         = 64
	MaxApplicationVersionBytes = 128
)

type Status string

const (
	StatusReady    Status = "ready"
	StatusNotReady Status = "not_ready"
)

type Heartbeat struct {
	WorkerName         string
	InstanceID         metadata.UUID
	ApplicationVersion string
	StartedAt          time.Time
	HeartbeatAt        time.Time
}

type Store interface {
	Ping(context.Context) error
	WriteHeartbeat(context.Context, Heartbeat) error
	ReadHeartbeat(context.Context, string) (Heartbeat, error)
}

type Writer struct {
	store     Store
	heartbeat Heartbeat
	interval  time.Duration
	now       func() time.Time
}

type Checker struct {
	store           Store
	workerName      string
	staleAfter      time.Duration
	futureTolerance time.Duration
	now             func() time.Time
}

type Handler struct {
	checker *Checker
}

var (
	ErrInvalidHealthConfig = errors.New("invalid cloud health configuration")
	ErrWorkerNotFound      = errors.New("worker heartbeat not found")
	ErrNotReady            = errors.New("cloud API is not ready")
	ErrHeartbeatInvalid    = errors.New("worker heartbeat is invalid")

	safeReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	safeWorker    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

func NewWriter(
	store Store,
	workerName string,
	instanceID metadata.UUID,
	applicationVersion string,
	startedAt time.Time,
	interval time.Duration,
	now func() time.Time,
) (*Writer, error) {
	if store == nil || interval <= 0 || startedAt.IsZero() ||
		!validWorkerName(workerName) || !validVersion(applicationVersion) {
		return nil, ErrInvalidHealthConfig
	}
	if _, err := metadata.ParseUUID(string(instanceID)); err != nil {
		return nil, ErrInvalidHealthConfig
	}
	if now == nil {
		now = time.Now
	}
	return &Writer{
		store: store,
		heartbeat: Heartbeat{
			WorkerName:         workerName,
			InstanceID:         instanceID,
			ApplicationVersion: applicationVersion,
			StartedAt:          startedAt.UTC(),
		},
		interval: interval,
		now:      now,
	}, nil
}

func (writer *Writer) Once(ctx context.Context) error {
	heartbeat := writer.heartbeat
	heartbeat.HeartbeatAt = writer.now().UTC()
	if heartbeat.HeartbeatAt.Before(heartbeat.StartedAt) {
		return ErrHeartbeatInvalid
	}
	return writer.store.WriteHeartbeat(ctx, heartbeat)
}

func (writer *Writer) Run(ctx context.Context) error {
	if err := writer.Once(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(writer.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := writer.Once(ctx); err != nil {
				return err
			}
		}
	}
}

func NewChecker(
	store Store,
	workerName string,
	staleAfter time.Duration,
	futureTolerance time.Duration,
	now func() time.Time,
) (*Checker, error) {
	if store == nil || !validWorkerName(workerName) ||
		staleAfter <= 0 || futureTolerance < 0 {
		return nil, ErrInvalidHealthConfig
	}
	if now == nil {
		now = time.Now
	}
	return &Checker{
		store:           store,
		workerName:      workerName,
		staleAfter:      staleAfter,
		futureTolerance: futureTolerance,
		now:             now,
	}, nil
}

func (checker *Checker) Check(ctx context.Context) (Status, error) {
	if err := checker.store.Ping(ctx); err != nil {
		return StatusNotReady, ErrNotReady
	}
	heartbeat, err := checker.store.ReadHeartbeat(ctx, checker.workerName)
	if err != nil {
		return StatusNotReady, ErrNotReady
	}
	now := checker.now().UTC()
	if err := validateHeartbeat(heartbeat); err != nil ||
		heartbeat.WorkerName != checker.workerName ||
		heartbeat.HeartbeatAt.Before(now.Add(-checker.staleAfter)) ||
		heartbeat.HeartbeatAt.After(now.Add(checker.futureTolerance)) {
		return StatusNotReady, ErrNotReady
	}
	return StatusReady, nil
}

func NewHandler(checker *Checker) (*Handler, error) {
	if checker == nil {
		return nil, ErrInvalidHealthConfig
	}
	return &Handler{checker: checker}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		response.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(response, `{"status":"not_ready"}`+"\n")
		return
	}
	status, err := handler.checker.Check(request.Context())
	if err != nil || status != StatusReady {
		response.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(response, `{"status":"not_ready"}`+"\n")
		return
	}
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, `{"status":"ready"}`+"\n")
}

func validateHeartbeat(heartbeat Heartbeat) error {
	if !validWorkerName(heartbeat.WorkerName) ||
		!validVersion(heartbeat.ApplicationVersion) ||
		heartbeat.StartedAt.IsZero() ||
		heartbeat.HeartbeatAt.IsZero() ||
		heartbeat.HeartbeatAt.Before(heartbeat.StartedAt) {
		return ErrHeartbeatInvalid
	}
	if _, err := metadata.ParseUUID(string(heartbeat.InstanceID)); err != nil {
		return ErrHeartbeatInvalid
	}
	return nil
}

func validWorkerName(value string) bool {
	return len(value) <= MaxWorkerNameBytes && safeWorker.MatchString(value)
}

func validVersion(value string) bool {
	return len(value) <= MaxApplicationVersionBytes && safeReference.MatchString(value)
}
