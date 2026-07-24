package userdaemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

const stateFileName = "pritunl-planner.json"

type Cycler interface {
	Observe(context.Context, control.Tick, int64) Summary
}

type StateStore interface {
	Save(control.Snapshot) error
}

type snapshotStore struct {
	path               string
	expectedGeneration uint64
}

func Run(args []string, stdout, stderr io.Writer) int {
	infoLog, err := logging.New(stdout, logging.ComponentUser)
	if err != nil {
		return 1
	}
	errorLog, err := logging.New(stderr, logging.ComponentUser)
	if err != nil {
		return 1
	}

	flags := flag.NewFlagSet("hexroute-userd", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	check := flags.Bool("check", false, "validate observe-only configuration")
	observeMode := flags.Bool("observe", false, "run the observe-only control loop")
	once := flags.Bool("once", false, "run one observe-only cycle")
	configPath := flags.String("config", "", "observe-only configuration")
	statePath := flags.String("state", "", "candidate state snapshot")

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
			"hexroute-userd version=%s commit=%s\n",
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
		if *once || *configPath != "" || *statePath != "" {
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
	if *configPath == "" || *statePath == "" {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	store, snapshot, err := openSnapshotStore(*statePath)
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	session, err := userobserve.NewMacOSObserver(observe.ExecRunner{})
	if err != nil {
		return 1
	}
	pritunl, err := userobserve.NewPritunlObserver(observe.ExecRunner{}, config.PritunlCLI)
	if err != nil {
		return 1
	}
	readiness, err := observe.NewReadinessObserver(observe.DefaultConnector{})
	if err != nil {
		return 1
	}
	planner, err := pritunlplan.NewPlanner(config.Policy, snapshot)
	if err != nil {
		return 1
	}
	cycle, err := NewCycle(config, session, pritunl, readiness, planner)
	if err != nil {
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := observeLoop(
		ctx,
		config.Interval,
		*once,
		snapshot.LastTick,
		cycle,
		store,
		infoLog,
	); err != nil {
		return 1
	}
	return 0
}

func openSnapshotStore(path string) (*snapshotStore, control.Snapshot, error) {
	if err := validateStatePath(path); err != nil {
		return nil, control.Snapshot{}, err
	}
	snapshot, err := control.LoadSnapshot(path)
	if errors.Is(err, control.ErrSnapshotNotFound) {
		return &snapshotStore{path: path}, control.NewSnapshot(control.StateHealthy), nil
	}
	if err != nil {
		return nil, control.Snapshot{}, err
	}
	return &snapshotStore{
		path:               path,
		expectedGeneration: snapshot.Generation,
	}, snapshot, nil
}

func validateStatePath(path string) error {
	if !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Base(path) != stateFileName {
		return ErrInvalidConfig
	}
	if err := validatePrivateOwner(filepath.Dir(path), true); err != nil {
		return err
	}
	if err := validatePrivateOwner(path, false); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validatePrivateOwner(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 ||
		(directory && !info.IsDir()) ||
		(!directory && !info.Mode().IsRegular()) {
		return ErrInvalidConfig
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return ErrInvalidConfig
	}
	return nil
}

func (store *snapshotStore) Save(snapshot control.Snapshot) error {
	if store == nil {
		return ErrInvalidConfig
	}
	if err := control.SaveSnapshot(
		store.path,
		store.expectedGeneration,
		snapshot,
	); err != nil {
		return err
	}
	store.expectedGeneration = snapshot.Generation
	return nil
}

func observeLoop(
	ctx context.Context,
	interval time.Duration,
	once bool,
	baseTick control.Tick,
	cycler Cycler,
	store StateStore,
	logger *logging.Logger,
) error {
	if ctx == nil ||
		interval <= 0 ||
		baseTick < 0 ||
		cycler == nil ||
		store == nil ||
		logger == nil {
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
		now := time.Now()
		at := baseTick + control.Tick(now.Sub(started)/time.Second)
		summary := cycler.Observe(ctx, at, now.Unix())
		if err := store.Save(summary.Plan.Snapshot); err != nil {
			return err
		}
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
	switch {
	case summary.Plan.State == control.StateSuspended:
		result = logging.ResultSuspended
	case summary.Failures == 0 &&
		summary.Plan.State == control.StateHealthy &&
		summary.Plan.Action == pritunlplan.ActionNone:
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
	if summary.Plan.Action == pritunlplan.ActionReconnect {
		return logger.Emit(
			logging.LevelInfo,
			logging.EventPritunlReconnect,
			logging.ResultProposed,
			"",
		)
	}
	if summary.Plan.Action != pritunlplan.ActionNone {
		return ErrInvalidConfig
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
