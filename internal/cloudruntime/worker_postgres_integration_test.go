package cloudruntime

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

func TestPostgresWorkerRuntimeHeartbeatsAndShutsDown(t *testing.T) {
	adminURL := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceURL := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminURL == "" || maintenanceURL == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	setupContext, cancelSetup := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancelSetup()
	admin, err := pgxpool.New(setupContext, adminURL)
	if err != nil {
		t.Fatalf("admin pgxpool.New() error = %v", err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(setupContext, `
		TRUNCATE TABLE nodes, worker_heartbeats CASCADE
	`); err != nil {
		t.Fatalf("reset worker data: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, cleanupErr := admin.Exec(cleanupContext, `
			TRUNCATE TABLE nodes, worker_heartbeats CASCADE
		`); cleanupErr != nil {
			t.Errorf("cleanup worker data: %v", cleanupErr)
		}
	})

	config := WorkerConfig{
		MaintenanceDatabaseURL: maintenanceURL,
		TelegramBotToken:       "12345678:abcdefghijklmnop",
		TelegramChatID:         "-123456789",
		WorkerName:             "primary",
		Location:               time.UTC,
		HeartbeatInterval:      5 * time.Second,
		ReconcileInterval:      5 * time.Second,
		AlertQueueInterval:     5 * time.Second,
		DeliveryInterval:       5 * time.Second,
		RetentionInterval:      time.Minute,
		JobTimeout:             time.Second,
		ShutdownTimeout:        time.Second,
	}
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentIngest)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	runContext, cancelRun := context.WithTimeout(
		context.Background(),
		250*time.Millisecond,
	)
	defer cancelRun()
	if err := RunWorker(runContext, config, logger); err != nil {
		t.Fatalf("RunWorker() error = %v log=%q", err, output.String())
	}
	var (
		workerName string
		version    string
		startedAt  time.Time
		heartbeat  time.Time
	)
	if err := admin.QueryRow(setupContext, `
		SELECT worker_name, application_version, started_at, heartbeat_at
		FROM worker_heartbeats
		WHERE worker_name = 'primary'
	`).Scan(&workerName, &version, &startedAt, &heartbeat); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if workerName != "primary" ||
		version == "" ||
		heartbeat.Before(startedAt) ||
		!strings.Contains(output.String(), "cloud_worker_started") ||
		!strings.Contains(output.String(), "cloud_worker_stopped") {
		t.Fatalf(
			"worker=%q version=%q started=%v heartbeat=%v log=%q",
			workerName,
			version,
			startedAt,
			heartbeat,
			output.String(),
		)
	}
}
