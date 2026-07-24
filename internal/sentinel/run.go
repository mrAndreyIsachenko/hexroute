package sentinel

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type Cycler interface {
	Observe(context.Context, control.Tick) Summary
}

func Run(args []string, stdout, stderr io.Writer) int {
	infoLog, err := logging.New(stdout, logging.ComponentSentinel)
	if err != nil {
		return 1
	}
	errorLog, err := logging.New(stderr, logging.ComponentSentinel)
	if err != nil {
		return 1
	}

	flags := flag.NewFlagSet("hexroute-sentinel", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	check := flags.Bool("check", false, "validate observe-only configuration")
	observeMode := flags.Bool("observe", false, "run the observe-only control loop")
	once := flags.Bool("once", false, "run one observe-only cycle")
	configPath := flags.String("config", "", "observe-only configuration")

	if err := flags.Parse(args); err != nil {
		return rejected(errorLog, logging.ReasonInvalidFlags)
	}
	if flags.NArg() != 0 {
		return rejected(errorLog, logging.ReasonUnexpectedArguments)
	}
	if *showVersion {
		if err := infoLog.Emit(
			logging.LevelInfo,
			logging.EventVersionRequested,
			logging.ResultReported,
			"",
		); err != nil {
			return 1
		}
		fmt.Fprintf(
			stdout,
			"hexroute-sentinel version=%s commit=%s\n",
			buildinfo.Version,
			buildinfo.Commit,
		)
		return 0
	}
	if *check {
		if *configPath != "" {
			if _, err := LoadConfig(*configPath); err != nil {
				return rejected(errorLog, logging.ReasonInvalidConfiguration)
			}
		}
		if err := infoLog.Emit(
			logging.LevelInfo,
			logging.EventStartupCheck,
			logging.ResultOK,
			"",
		); err != nil {
			return 1
		}
		return 0
	}
	if !*observeMode {
		if *once || *configPath != "" {
			return rejected(errorLog, logging.ReasonInvalidFlags)
		}
		if err := infoLog.Emit(
			logging.LevelInfo,
			logging.EventCommandStatus,
			logging.ResultSkeleton,
			"",
		); err != nil {
			return 1
		}
		return 0
	}
	if *configPath == "" {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	readiness, err := observe.NewReadinessObserver(observe.DefaultConnector{})
	if err != nil {
		return 1
	}
	tracker, err := NewTracker(config.StaleThreshold)
	if err != nil {
		return 1
	}
	cycle, err := NewCycle(config, FileHeartbeatReader{}, readiness, tracker)
	if err != nil {
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := observeLoop(
		ctx,
		config.Interval,
		*once,
		cycle,
		infoLog,
	); err != nil {
		return 1
	}
	return 0
}

func observeLoop(
	ctx context.Context,
	interval time.Duration,
	once bool,
	cycler Cycler,
	logger *logging.Logger,
) error {
	if ctx == nil || interval <= 0 || cycler == nil || logger == nil {
		return ErrInvalidConfig
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventDaemonStarted,
		logging.ResultOK,
		"",
	); err != nil {
		return err
	}
	started := time.Now()
	for {
		at := control.Tick(time.Since(started) / time.Second)
		summary := cycler.Observe(ctx, at)
		if err := emitSummary(logger, summary); err != nil {
			return err
		}
		if once {
			return logger.Emit(
				logging.LevelInfo,
				logging.EventDaemonStopped,
				logging.ResultOK,
				"",
			)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return logger.Emit(
				logging.LevelInfo,
				logging.EventDaemonStopped,
				logging.ResultOK,
				"",
			)
		case <-timer.C:
		}
	}
}

func emitSummary(logger *logging.Logger, summary Summary) error {
	result := logging.ResultDegraded
	if summary.Failures == 0 &&
		summary.HeartbeatFound &&
		summary.DataPathReady &&
		!summary.Decision.HeartbeatStale {
		result = logging.ResultOK
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventObservationCycle,
		result,
		"",
	); err != nil {
		return err
	}
	if summary.Decision.EvidenceReady {
		return logger.Emit(
			logging.LevelWarn,
			logging.EventSentinelEvidence,
			logging.ResultReported,
			"",
		)
	}
	return nil
}

func rejected(logger *logging.Logger, reason logging.Reason) int {
	if err := logger.Emit(
		logging.LevelWarn,
		logging.EventArgumentRejected,
		logging.ResultRejected,
		reason,
	); err != nil {
		return 1
	}
	return 2
}
