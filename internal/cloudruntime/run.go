package cloudruntime

import (
	"context"
	"io"

	"github.com/mrAndreyIsachenko/hexroute/internal/command"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

type apiRunner func(context.Context, APIConfig, *logging.Logger) error
type workerRunner func(context.Context, WorkerConfig, *logging.Logger) error
type migrationRunner func(context.Context, MigrationConfig, *logging.Logger) error

func Run(
	ctx context.Context,
	args []string,
	environment Environment,
	stdout io.Writer,
	stderr io.Writer,
) int {
	return run(ctx, args, environment, stdout, stderr, RunAPI, RunWorker, RunMigration)
}

func run(
	ctx context.Context,
	args []string,
	environment Environment,
	stdout io.Writer,
	stderr io.Writer,
	runAPI apiRunner,
	runWorker workerRunner,
	runMigration migrationRunner,
) int {
	if len(args) == 0 {
		component := environmentValue(environment, "HEXROUTE_COMPONENT")
		if component == "" {
			return command.Run(
				"hexroute-ingest", []string{"--check"}, stdout, stderr,
			)
		}
		args = []string{component}
	}
	if len(args) == 1 && (args[0] == "--check" || args[0] == "--version") {
		return command.Run("hexroute-ingest", args, stdout, stderr)
	}
	if len(args) != 1 ||
		(args[0] != "api" && args[0] != "worker" && args[0] != "migrate") {
		errorLogger, err := logging.New(stderr, logging.ComponentIngest)
		if err != nil {
			return 2
		}
		_ = errorLogger.Emit(
			logging.LevelWarn,
			logging.EventArgumentRejected,
			logging.ResultRejected,
			logging.ReasonInvalidFlags,
		)
		return 2
	}
	infoLogger, err := logging.New(stdout, logging.ComponentIngest)
	if err != nil {
		return 1
	}
	errorLogger, err := logging.New(stderr, logging.ComponentIngest)
	if err != nil {
		return 1
	}
	switch args[0] {
	case "api":
		config, err := LoadAPIConfig(environment)
		if err != nil {
			return emitInvalidConfiguration(errorLogger)
		}
		if runAPI == nil || runAPI(ctx, config, infoLogger) != nil {
			_ = errorLogger.Emit(
				logging.LevelWarn,
				logging.EventCloudAPIStopped,
				logging.ResultDegraded,
				"",
			)
			return 1
		}
	case "worker":
		config, err := LoadWorkerConfig(environment)
		if err != nil {
			return emitInvalidConfiguration(errorLogger)
		}
		if runWorker == nil || runWorker(ctx, config, infoLogger) != nil {
			_ = errorLogger.Emit(
				logging.LevelWarn,
				logging.EventCloudWorkerStopped,
				logging.ResultDegraded,
				"",
			)
			return 1
		}
	case "migrate":
		config, err := LoadMigrationConfig(environment)
		if err != nil {
			return emitInvalidConfiguration(errorLogger)
		}
		if runMigration == nil || runMigration(ctx, config, infoLogger) != nil {
			_ = errorLogger.Emit(
				logging.LevelWarn,
				logging.EventCloudMigration,
				logging.ResultDegraded,
				"",
			)
			return 1
		}
	}
	return 0
}

func emitInvalidConfiguration(logger *logging.Logger) int {
	_ = logger.Emit(
		logging.LevelWarn,
		logging.EventArgumentRejected,
		logging.ResultRejected,
		logging.ReasonInvalidConfiguration,
	)
	return 1
}
