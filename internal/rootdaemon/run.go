package rootdaemon

import (
	"context"
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
	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
)

type Cycler interface {
	Observe(context.Context) Summary
}

type HeartbeatPublisher interface {
	Publish(control.Tick) error
}

func Run(args []string, stdout, stderr io.Writer) int {
	infoLog, err := logging.New(stdout, logging.ComponentDaemon)
	if err != nil {
		return 1
	}
	errorLog, err := logging.New(stderr, logging.ComponentDaemon)
	if err != nil {
		return 1
	}

	flags := flag.NewFlagSet("hexrouted", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	check := flags.Bool("check", false, "validate observe-only configuration")
	observeMode := flags.Bool("observe", false, "run the observe-only control loop")
	once := flags.Bool("once", false, "run one observe-only cycle")
	configPath := flags.String("config", "", "observe-only configuration")
	heartbeatPath := flags.String("heartbeat", "", "control-loop heartbeat")
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
		fmt.Fprintf(stdout, "hexrouted version=%s commit=%s\n", buildinfo.Version, buildinfo.Commit)
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
		if *socketPath != "" {
			if *configPath == "" ||
				validateRootSocketPath(*socketPath, config.OperatorUID) != nil {
				return rejected(errorLog, logging.ReasonInvalidConfiguration)
			}
		}
		if *heartbeatPath != "" {
			return rejected(errorLog, logging.ReasonInvalidFlags)
		}
		if err := infoLog.Emit(logging.LevelInfo, logging.EventStartupCheck, logging.ResultOK, ""); err != nil {
			return 1
		}
		return 0
	}
	if !*observeMode {
		if *once || *configPath != "" || *heartbeatPath != "" || *socketPath != "" {
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
	if *configPath == "" || *heartbeatPath == "" {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	publisher, err := heartbeat.OpenPublisher(*heartbeatPath, os.Getpid())
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	network, err := observe.NewMacOSObserver(observe.ExecRunner{})
	if err != nil {
		return 1
	}
	processes, err := observe.NewProcessObserver(observe.ExecRunner{})
	if err != nil {
		return 1
	}
	readiness, err := observe.NewReadinessObserver(observe.DefaultConnector{})
	if err != nil {
		return 1
	}
	cycle, err := NewCycle(config, network, processes, readiness)
	if err != nil {
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := time.Now()
	nowTick := func() control.Tick {
		return publisher.BaseTick() + control.Tick(time.Since(started)/time.Second)
	}
	initial := control.NewSnapshot(control.StateSuspended)
	controller, err := operator.NewController(
		ipc.RoleRoot,
		ipc.ModeObserveOnly,
		[]control.Component{
			control.ComponentNetwork,
			control.ComponentTunnel,
			control.ComponentRoutes,
			control.ComponentCodex,
			control.ComponentTelegram,
		},
		initial,
		control.ReasonNone,
		nil,
		nowTick,
	)
	if err != nil {
		return 1
	}
	var requests <-chan operator.Envelope
	var serverDone <-chan error
	var server *ipc.Server
	if *socketPath != "" {
		if err := validateRootSocketPath(*socketPath, config.OperatorUID); err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		broker, err := operator.NewBroker(runCtx)
		if err != nil {
			return 1
		}
		policyHandler, policyStore, err := openRootPolicyHandler(config.PolicyControl)
		if err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		if policyStore != nil {
			defer policyStore.Close()
		}
		if err := controller.SetResumePolicyEvaluator(policyHandler); err != nil {
			return 1
		}
		dispatcher, err := operator.NewDispatcher(controller, broker, policyHandler)
		if err != nil {
			return 1
		}
		reporter, err := operator.NewRejectionLogger(errorLog)
		if err != nil {
			return 1
		}
		policyReporter := policycontrol.NewRejectionReporter(reporter, policyHandler)
		server, err = ipc.Listen(
			*socketPath,
			uint32(config.OperatorUID),
			uint32(config.OperatorUID),
			dispatcher,
			policyReporter,
		)
		if err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		requests = broker.Requests()
		done := make(chan error, 1)
		serverDone = done
		go func() {
			defer close(done)
			done <- server.Serve(runCtx)
		}()
		defer func() {
			cancel()
			_ = server.Close()
			<-done
		}()
	}
	if err := observeLoop(
		runCtx,
		config.Interval,
		*once,
		nowTick,
		cycle,
		publisher,
		controller,
		requests,
		serverDone,
		infoLog,
	); err != nil {
		return 1
	}
	return 0
}

func openRootPolicyHandler(
	config *policycontrol.RuntimeConfig,
) (*policycontrol.Handler, *policystore.Store, error) {
	if config == nil {
		handler, err := policycontrol.NewUnavailableHandler(policy.DomainRoot)
		return handler, nil, err
	}
	store, err := policystore.OpenRoot()
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

func validateRootSocketPath(path string, operatorUID int) error {
	if os.Geteuid() != 0 ||
		operatorUID <= 0 ||
		path != ipc.RootSocketPath ||
		filepath.Clean(path) != path {
		return ErrInvalidConfig
	}
	return nil
}

func observeLoop(
	ctx context.Context,
	interval time.Duration,
	once bool,
	nowTick func() control.Tick,
	cycler Cycler,
	publisher HeartbeatPublisher,
	controller *operator.Controller,
	requests <-chan operator.Envelope,
	serverDone <-chan error,
	logger *logging.Logger,
) error {
	if ctx == nil ||
		interval <= 0 ||
		nowTick == nil ||
		cycler == nil ||
		publisher == nil ||
		controller == nil ||
		logger == nil {
		return ErrInvalidConfig
	}
	if err := logger.Emit(logging.LevelInfo, logging.EventDaemonStarted, logging.ResultOK, ""); err != nil {
		return err
	}
	operatorSnapshot := control.NewSnapshot(control.StateSuspended)
	for {
		summary := cycler.Observe(ctx)
		if err := emitSummary(logger, summary); err != nil {
			return err
		}
		at := nowTick()
		if err := publisher.Publish(at); err != nil {
			return err
		}
		operatorSnapshot = nextRootOperatorSnapshot(operatorSnapshot, summary, at)
		if err := controller.Update(
			operatorSnapshot,
			rootOperatorReason(summary.State),
		); err != nil {
			return err
		}
		if once {
			return logger.Emit(logging.LevelInfo, logging.EventDaemonStopped, logging.ResultOK, "")
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

func nextRootOperatorSnapshot(
	current control.Snapshot,
	summary Summary,
	at control.Tick,
) control.Snapshot {
	next := current
	switch summary.State {
	case CycleHealthy:
		next.State = control.StateHealthy
	case CycleSuspended:
		next.State = control.StateSuspended
	case CycleDegraded:
		next.State = control.StateDegraded
	}
	next.ConsecutiveFailures = summary.Failures
	next.LastTick = at
	if next != current {
		next.Generation = current.Generation + 1
	}
	return next
}

func rootOperatorReason(state CycleState) control.Reason {
	switch state {
	case CycleHealthy:
		return control.ReasonProbeSucceeded
	case CycleSuspended:
		return control.ReasonIntentionalSleep
	case CycleDegraded:
		return control.ReasonProbeFailed
	default:
		return control.ReasonNone
	}
}

func emitSummary(logger *logging.Logger, summary Summary) error {
	result := logging.ResultDegraded
	switch summary.State {
	case CycleHealthy:
		result = logging.ResultOK
	case CycleSuspended:
		result = logging.ResultSuspended
	case CycleDegraded:
	default:
		return ErrInvalidConfig
	}
	if err := logger.Emit(logging.LevelInfo, logging.EventObservationCycle, result, ""); err != nil {
		return err
	}
	for _, operation := range summary.Plan.Operations {
		event, err := routeEvent(operation)
		if err != nil {
			return err
		}
		if err := logger.Emit(logging.LevelInfo, event, logging.ResultProposed, ""); err != nil {
			return err
		}
	}
	return nil
}

func routeEvent(operation routeplan.Operation) (logging.EventName, error) {
	switch operation.Role {
	case routeplan.RoleIngress:
		return logging.EventIngressRoute, nil
	case routeplan.RoleCorporate:
		return logging.EventCorporateRoute, nil
	case routeplan.RoleGitLabHTTPS:
		return logging.EventGitLabHTTPSRoute, nil
	case routeplan.RoleCodexFallback:
		return logging.EventCodexRoute, nil
	default:
		return "", ErrInvalidConfig
	}
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
