package cloudhealth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Database interface {
	Ping(context.Context) error
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	database Database
}

func NewPostgresStore(database Database) (*PostgresStore, error) {
	if database == nil {
		return nil, ErrInvalidHealthConfig
	}
	return &PostgresStore{database: database}, nil
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.database.Ping(ctx)
}

func (store *PostgresStore) WriteHeartbeat(
	ctx context.Context,
	heartbeat Heartbeat,
) error {
	if err := validateHeartbeat(heartbeat); err != nil {
		return err
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO worker_heartbeats (
			worker_name,
			instance_id,
			application_version,
			started_at,
			heartbeat_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (worker_name) DO UPDATE
		SET instance_id = EXCLUDED.instance_id,
		    application_version = EXCLUDED.application_version,
		    started_at = EXCLUDED.started_at,
		    heartbeat_at = EXCLUDED.heartbeat_at
		WHERE worker_heartbeats.heartbeat_at <= EXCLUDED.heartbeat_at
	`,
		heartbeat.WorkerName,
		string(heartbeat.InstanceID),
		heartbeat.ApplicationVersion,
		heartbeat.StartedAt,
		heartbeat.HeartbeatAt,
	)
	return err
}

func (store *PostgresStore) ReadHeartbeat(
	ctx context.Context,
	workerName string,
) (Heartbeat, error) {
	if !validWorkerName(workerName) {
		return Heartbeat{}, ErrInvalidHealthConfig
	}
	var (
		instanceID         string
		applicationVersion string
		startedAt          time.Time
		heartbeatAt        time.Time
	)
	err := store.database.QueryRow(ctx, `
		SELECT
			instance_id::text,
			application_version,
			started_at,
			heartbeat_at
		FROM worker_heartbeats
		WHERE worker_name = $1
	`, workerName).Scan(
		&instanceID,
		&applicationVersion,
		&startedAt,
		&heartbeatAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Heartbeat{}, ErrWorkerNotFound
	}
	if err != nil {
		return Heartbeat{}, err
	}
	parsedInstanceID, err := metadata.ParseUUID(instanceID)
	if err != nil {
		return Heartbeat{}, ErrHeartbeatInvalid
	}
	heartbeat := Heartbeat{
		WorkerName:         workerName,
		InstanceID:         parsedInstanceID,
		ApplicationVersion: applicationVersion,
		StartedAt:          startedAt.UTC(),
		HeartbeatAt:        heartbeatAt.UTC(),
	}
	if err := validateHeartbeat(heartbeat); err != nil {
		return Heartbeat{}, err
	}
	return heartbeat, nil
}
