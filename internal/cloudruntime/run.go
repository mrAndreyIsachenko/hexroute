package cloudruntime

import (
	"context"
	"io"

	"github.com/mrAndreyIsachenko/hexroute/internal/command"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

type apiRunner func(context.Context, APIConfig, *logging.Logger) error

func Run(
	ctx context.Context,
	args []string,
	environment Environment,
	stdout io.Writer,
	stderr io.Writer,
) int {
	return run(ctx, args, environment, stdout, stderr, RunAPI)
}

func run(
	ctx context.Context,
	args []string,
	environment Environment,
	stdout io.Writer,
	stderr io.Writer,
	runAPI apiRunner,
) int {
	if len(args) == 1 && (args[0] == "--check" || args[0] == "--version") {
		return command.Run("hexroute-ingest", args, stdout, stderr)
	}
	if len(args) != 1 || args[0] != "api" {
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
	config, err := LoadAPIConfig(environment)
	if err != nil {
		_ = errorLogger.Emit(
			logging.LevelWarn,
			logging.EventArgumentRejected,
			logging.ResultRejected,
			logging.ReasonInvalidConfiguration,
		)
		return 1
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
	return 0
}
