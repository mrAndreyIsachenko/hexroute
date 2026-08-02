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
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/notification"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

const (
	stateFileName  = "pritunl-planner.json"
	socketFileName = "userd.sock"
)

type Cycler interface {
	Observe(context.Context, control.Tick, int64) Summary
}

type StateStore interface {
	Save(control.Snapshot) error
}

type IncidentNotifier interface {
	Dispatch(
		context.Context,
		notification.Input,
		time.Time,
	) (notification.Outcome, error)
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
	socketPath := flags.String("socket", "", "typed local operator socket")

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
		var config RuntimeConfig
		if *configPath != "" {
			config, err = LoadConfig(*configPath)
			if err != nil {
				return rejected(errorLog, logging.ReasonInvalidConfiguration)
			}
		}
		if (*socketPath == "") != (*statePath == "") {
			return rejected(errorLog, logging.ReasonInvalidFlags)
		}
		if *socketPath != "" {
			if *configPath == "" ||
				validateUserSocketPath(*socketPath, *statePath, config.ExpectedUID) != nil {
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
		if *once || *configPath != "" || *statePath != "" || *socketPath != "" {
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

	signalCtx, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	started := time.Now()
	nowTick := func() control.Tick {
		return snapshot.LastTick + control.Tick(time.Since(started)/time.Second)
	}
	controller, err := operator.NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonNone,
		func(expected uint64, at control.Tick) (control.Snapshot, error) {
			return planner.Resume(expected, at, store.Save)
		},
		nowTick,
	)
	if err != nil {
		return 1
	}
	macOSNotifier, err := notification.NewDefaultMacOSNotifier()
	if err != nil {
		return 1
	}
	notifications, err := notification.NewService(
		notification.Policy{
			NightStartHour: 23,
			NightEndHour:   8,
		},
		macOSNotifier,
	)
	if err != nil {
		return 1
	}
	var requests <-chan operator.Envelope
	var serverDone <-chan error
	var server *ipc.Server
	if *socketPath != "" {
		if err := validateUserSocketPath(*socketPath, *statePath, config.ExpectedUID); err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		broker, err := operator.NewBroker(ctx)
		if err != nil {
			return 1
		}
		policyHandler, policyStore, err := openUserPolicyHandler(config.PolicyControl)
		if err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		if policyStore != nil {
			defer policyStore.Close()
		}
		dispatcher, err := operator.NewDispatcher(controller, broker, policyHandler)
		if err != nil {
			return 1
		}
		reporter, err := operator.NewRejectionLogger(errorLog)
		if err != nil {
			return 1
		}
		server, err = ipc.Listen(
			*socketPath,
			uint32(config.ExpectedUID),
			uint32(config.ExpectedUID),
			dispatcher,
			reporter,
		)
		if err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		requests = broker.Requests()
		done := make(chan error, 1)
		serverDone = done
		go func() {
			defer close(done)
			done <- server.Serve(ctx)
		}()
		defer func() {
			cancel()
			_ = server.Close()
			<-done
		}()
	}
	if err := observeLoop(
		ctx,
		config.Interval,
		*once,
		nowTick,
		cycle,
		store,
		controller,
		notifications,
		requests,
		serverDone,
		infoLog,
	); err != nil {
		return 1
	}
	return 0
}

func openUserPolicyHandler(
	config *policycontrol.RuntimeConfig,
) (*policycontrol.Handler, *policystore.Store, error) {
	if config == nil {
		handler, err := policycontrol.NewUnavailableHandler(policy.DomainUser)
		return handler, nil, err
	}
	store, err := policystore.OpenCurrentUser()
	if err != nil {
		return nil, nil, err
	}
	handler, err := policycontrol.NewHandler(store, *config, time.Now)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return handler, store, nil
}

func validateUserSocketPath(path string, statePath string, expectedUID int) error {
	if expectedUID != os.Geteuid() ||
		!filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Base(path) != socketFileName ||
		filepath.Dir(path) != filepath.Dir(statePath) {
		return ErrInvalidConfig
	}
	return nil
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
	nowTick func() control.Tick,
	cycler Cycler,
	store StateStore,
	controller *operator.Controller,
	notifications IncidentNotifier,
	requests <-chan operator.Envelope,
	serverDone <-chan error,
	logger *logging.Logger,
) error {
	if ctx == nil ||
		interval <= 0 ||
		nowTick == nil ||
		cycler == nil ||
		store == nil ||
		controller == nil ||
		notifications == nil ||
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
	lastState := control.State("")
	for {
		now := time.Now()
		at := nowTick()
		summary := cycler.Observe(ctx, at, now.Unix())
		if err := store.Save(summary.Plan.Snapshot); err != nil {
			return err
		}
		if err := controller.Update(
			summary.Plan.Snapshot,
			operatorReason(summary.Plan.Reason),
		); err != nil {
			return err
		}
		dispatchPritunlNotification(
			ctx,
			notifications,
			lastState,
			summary.Plan.Snapshot,
			now,
			logger,
		)
		lastState = summary.Plan.Snapshot.State
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
	wait:
		for {
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
			case err := <-serverDone:
				if err != nil {
					return err
				}
				return ErrInvalidConfig
			case envelope := <-requests:
				if envelope.Active() {
					envelope.Respond(controller.Handle(envelope.Request))
				}
			case <-timer.C:
				break wait
			}
		}
	}
}

func dispatchPritunlNotification(
	ctx context.Context,
	notifications IncidentNotifier,
	previous control.State,
	snapshot control.Snapshot,
	at time.Time,
	logger *logging.Logger,
) {
	if ctx == nil ||
		notifications == nil ||
		logger == nil ||
		previous == snapshot.State {
		return
	}

	status := event.IncidentStatus("")
	severity := event.SeverityInfo
	switch {
	case snapshot.State == control.StateSafeMode:
		status = event.IncidentOpened
		severity = event.SeverityWarning
	case previous == control.StateSafeMode:
		status = event.IncidentResolved
	default:
		return
	}
	notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	outcome, err := notifications.Dispatch(
		notifyCtx,
		notification.Input{
			Incident: event.Incident{
				IncidentID: "pritunl-safe-mode",
				Status:     status,
				Severity:   severity,
				Category:   event.IncidentRecoveryBudget,
				Component:  control.ComponentPritunl,
				Generation: snapshot.Generation,
			},
			External: notification.ExternalNotRequired,
		},
		at,
	)
	if err != nil {
		_ = logger.Emit(
			logging.LevelWarn,
			logging.EventLocalNotification,
			logging.ResultDegraded,
			"",
		)
		return
	}
	if outcome.LocalDelivery == notification.LocalDelivered {
		_ = logger.Emit(
			logging.LevelInfo,
			logging.EventLocalNotification,
			logging.ResultReported,
			"",
		)
	}
}

func operatorReason(reason pritunlplan.Reason) control.Reason {
	switch reason {
	case pritunlplan.ReasonProfileConnected,
		pritunlplan.ReasonRecoveryVerifying:
		return control.ReasonProbeSucceeded
	case pritunlplan.ReasonProfileNotConnected:
		return control.ReasonProbeFailed
	case pritunlplan.ReasonReconnectAllowed:
		return control.ReasonRecoveryAllowed
	case pritunlplan.ReasonRecoveryBudget:
		return control.ReasonRecoveryBudget
	case pritunlplan.ReasonSessionInactive,
		pritunlplan.ReasonLidClosed,
		pritunlplan.ReasonDarkWake,
		pritunlplan.ReasonWakeUnknown,
		pritunlplan.ReasonWakeSettling:
		return control.ReasonIntentionalSleep
	case pritunlplan.ReasonOuterNotReady:
		return control.ReasonDependenciesNotReady
	default:
		return control.ReasonNone
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
