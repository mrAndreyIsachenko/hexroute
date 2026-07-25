package cloudruntime

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/databasemigrate"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

const migrationTimeout = 2 * time.Minute

var ErrMigrationRuntime = errors.New("cloud migration runtime unavailable")

func RunMigration(
	ctx context.Context,
	config MigrationConfig,
	logger *logging.Logger,
) error {
	if ctx == nil || logger == nil || config.Validate() != nil {
		return ErrMigrationRuntime
	}
	migrationContext, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.MigratorDatabaseURL)
	if err != nil {
		return ErrMigrationRuntime
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 0
	pool, err := pgxpool.NewWithConfig(migrationContext, poolConfig)
	if err != nil {
		return ErrMigrationRuntime
	}
	defer pool.Close()
	if err = pool.Ping(migrationContext); err != nil {
		return ErrMigrationRuntime
	}
	runner, err := databasemigrate.New(pool, nil)
	if err != nil {
		return ErrMigrationRuntime
	}
	if err = runner.Apply(
		migrationContext,
		config.BootstrapUsername,
		config.BootstrapDisplayName,
	); err != nil {
		return ErrMigrationRuntime
	}
	if err = logger.Emit(
		logging.LevelInfo,
		logging.EventCloudMigration,
		logging.ResultOK,
		"",
	); err != nil {
		return ErrMigrationRuntime
	}
	return nil
}
