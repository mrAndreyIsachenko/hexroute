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
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
	"github.com/mrAndreyIsachenko/hexroute/internal/reconciler"
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
	// Off unless a root is given. Without one the daemon runs exactly the path
	// it ran before the read model existed.
	readModelRoot := flags.String(
		"connectivity-read-model", "", "observe-only connectivity read model root")
	// Off unless both are given. Qualification is a decision someone records,
	// and a chain without a session identity would accumulate two runs into a
	// number about neither.
	qualificationRoot := flags.String(
		"connectivity-qualification", "", "soak qualification evidence chain root")
	qualificationSession := flags.String(
		"connectivity-qualification-session", "", "soak qualification session UUID")

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
		// A check that passes what the run would refuse is worse than no
		// check: the daemon is bootstrapped with KeepAlive, so a session the
		// observer cannot parse becomes a restart loop rather than a message
		// to whoever installed it.
		if err := connectivityhost.ValidateQualification(
			*qualificationRoot, *qualificationSession); err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
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
	// Opened before the operator socket: the dispatcher needs the publisher,
	// and a root that cannot receive user facts should fail at startup rather
	// than accept publications it will drop.
	reader, err := connectivityhost.Open(*readModelRoot, bootIdentity())
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	// A misconfigured chain is refused at startup rather than at the first
	// sample. A soak that ran for hours and then turned out to have been
	// recording nothing is worse than one that never started.
	if err := reader.AttachQualifier(*qualificationRoot, *qualificationSession); err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	// The reconciler's shadow store is opened so its status is answerable. It
	// is synthetic-only and exports no execution path.
	//
	// A store that will not open is reported and left absent. It must not stop
	// the daemon: observation, the read model and the operator surface do not
	// depend on it, and a host that stops watching its own network because a
	// status surface could not open has traded something that matters for
	// something that does not.
	shadowStore, shadowErr := reconciler.OpenShadowStore(
		reconciler.RootShadowStoreConfig(uint32(config.OperatorUID)))
	if shadowErr != nil {
		shadowStore = nil
		if err := infoLog.Emit(
			logging.LevelWarn,
			logging.EventReconcilerShadowUnavailable,
			logging.ResultDegraded,
			"",
		); err != nil {
			return 1
		}
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
		if err := ensureRootSocketDirectory(filepath.Dir(*socketPath)); err != nil {
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
		dispatcher, err := operator.NewDispatcher(
			controller, broker, policyHandler,
			connectivityPublisher{reader: reader}, shadowHandler(shadowStore))
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
		reader,
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

func ensureRootSocketDirectory(path string) error {
	if err := os.MkdirAll(path, 0o711); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidConfig
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0o022 != 0 {
		return ErrInvalidConfig
	}
	if info.Mode().Perm() != 0o711 {
		return os.Chmod(path, 0o711)
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
	reader *connectivityhost.Reader,
) error {
	// The gate keeps the log a record of what happened rather than of how
	// often it was checked. Liveness lives in the heartbeat file.
	gate := logging.NewChangeGate()
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
		if err := emitSummary(logger, gate, summary); err != nil {
			return err
		}
		at := nowTick()
		// The read model runs after the cycle and changes nothing about it.
		// Its failure is reported and dropped: a daemon that stops observing
		// because a description of its observations failed would be worse than
		// one with no read model at all.
		if err := connectivityhost.Fold(
			reader, summary.Observed, plannerIntents(summary.Plan), logger); err != nil {
			return err
		}
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

func emitSummary(logger *logging.Logger, gate *logging.ChangeGate, summary Summary) error {
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
	if gate.Changed(logging.EventObservationCycle, string(result)) {
		if err := logger.Emit(
			logging.LevelInfo, logging.EventObservationCycle, result, ""); err != nil {
			return err
		}
	}
	// A route proposal that has not changed is the same proposal, not a new
	// one. Each route event carries its own state so a plan that gains or
	// loses one operation is still reported.
	for _, operation := range summary.Plan.Operations {
		event, err := routeEvent(operation)
		if err != nil {
			return err
		}
		if !gate.Changed(event, string(logging.ResultProposed)) {
			continue
		}
		if err := logger.Emit(logging.LevelInfo, event, logging.ResultProposed, ""); err != nil {
			return err
		}
	}
	// A route that stopped being proposed has to clear, or the next proposal
	// for it would be suppressed as a repeat of one that has since lapsed.
	proposed := make(map[logging.EventName]struct{}, len(summary.Plan.Operations))
	for _, operation := range summary.Plan.Operations {
		if event, err := routeEvent(operation); err == nil {
			proposed[event] = struct{}{}
		}
	}
	for _, event := range routeEvents() {
		if _, still := proposed[event]; !still {
			gate.Changed(event, "")
		}
	}
	return nil
}

// routeEvents is every route event a plan can propose.
func routeEvents() []logging.EventName {
	return []logging.EventName{
		logging.EventIngressRoute, logging.EventCorporateRoute,
		logging.EventGitLabHTTPSRoute, logging.EventCodexRoute,
	}
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
