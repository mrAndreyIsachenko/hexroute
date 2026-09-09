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
	"github.com/mrAndreyIsachenko/hexroute/internal/policyexpiry"
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
	// The root aggregate's socket. Given one, this daemon publishes what it
	// observed; without one it publishes nothing and nothing else changes.
	rootSocketPath := flags.String(
		"publish-connectivity-to", "", "root socket to publish connectivity facts to")

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
		// Beside the other state this daemon already writes every cycle. An
		// announcement made for a generation stays made across a restart;
		// without this the same crossing was announced again on every start.
		notification.DeliveryRecordAt(filepath.Join(
			filepath.Dir(*statePath), "notification-deliveries.json")),
	)
	if err != nil {
		return 1
	}
	var requests <-chan operator.Envelope
	var serverDone <-chan error
	var server *ipc.Server
	// The policy handler outlives the operator socket. Authority to act comes
	// from the active generation whether or not anybody is connected to ask
	// this daemon a question.
	var policyHandler *policycontrol.Handler
	if *socketPath != "" {
		if err := validateUserSocketPath(*socketPath, *statePath, config.ExpectedUID); err != nil {
			return rejected(errorLog, logging.ReasonInvalidConfiguration)
		}
		broker, err := operator.NewBroker(ctx)
		if err != nil {
			return 1
		}
		handler, policyStore, err := openUserPolicyHandler(config.PolicyControl)
		policyHandler = handler
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
			// The user daemon publishes facts; it never receives them, and it
			// holds no shadow store of its own in this build.
			controller, broker, policyHandler, nil, nil)
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
			uint32(config.ExpectedUID),
			uint32(config.ExpectedUID),
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
			done <- server.Serve(ctx)
		}()
		defer func() {
			cancel()
			_ = server.Close()
			<-done
		}()
	}
	// Off unless a root socket is given, matching the root gate: a user daemon
	// started without one behaves exactly as it did before this existed.
	// The stream memory sits beside the candidate state, in the directory
	// this daemon already owns and already writes to.
	publisher, err := newFactPublisher(bootIdentity(), *rootSocketPath,
		filepath.Join(filepath.Dir(*statePath), "connectivity-stream.json"),
		config.Interval)
	if err != nil {
		return rejected(errorLog, logging.ReasonInvalidConfiguration)
	}
	// The authority to act comes from the active policy generation and from
	// nothing else. Before the ownership cutover no generation grants it, so
	// this executor answers "proposed" to everything and the daemon behaves
	// exactly as it did before it existed.
	executor := newRecovery(policyHandler, config.Recovery, *rootSocketPath)
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
		publisher,
		executor,
		policyHandler,
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
	publisher *factPublisher,
	executor *recovery,
	policyExpiry ExpiryAnnouncer,
) error {
	// The log records what happened; the state file records that the loop ran.
	gate := logging.NewChangeGate()
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
		// What the planner decided is acted on only if something authorizes
		// it. Before the ownership cutover nothing does, and every decision
		// comes back proposed — which is the same loop this daemon has always
		// run, with the answer written down instead of assumed.
		summary.Outcome = executor.perform(ctx, summary.Plan)
		// Publishing happens before the daemon acts on its own conclusions and
		// cannot change them: a root that is unreachable, refusing or absent
		// leaves this loop exactly as it was.
		if err := publisher.Publish(ctx, summary.Observed, logger); err != nil {
			return err
		}
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
		dispatchPolicyExpiryNotification(
			ctx,
			notifications,
			policyExpiry,
			now,
			logger,
		)
		lastState = summary.Plan.Snapshot.State
		if err := emitSummary(logger, gate, summary); err != nil {
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

// ExpiryAnnouncer is the policy handler, narrowed to the one question this
// loop asks it. The loop does not decide anything about policy; it carries an
// answer to the notification path.
type ExpiryAnnouncer interface {
	ExpiryAnnouncement(time.Time) (policyexpiry.Stage, uint64, bool)
}

// dispatchPolicyExpiryNotification announces that the active generation is near
// or past the end of its validity.
//
// It exists because nothing announced it. Expiry was surfaced in no status, no
// tooling and no script, so the only way to know a generation had days left was
// to read a private manifest by hand — and this system removed the one loud
// signal there was when it stopped treating a lapse as a suspension. A passive
// field would not close that: nobody was looking, because nothing suggested
// looking.
//
// Delivery deduplicates on the incident identity and the generation, so a stage
// announces once per generation however many cycles run through it.
func dispatchPolicyExpiryNotification(
	ctx context.Context,
	notifications IncidentNotifier,
	announcer ExpiryAnnouncer,
	at time.Time,
	logger *logging.Logger,
) {
	if ctx == nil || notifications == nil || announcer == nil || logger == nil {
		return
	}
	stage, generation, ok := announcer.ExpiryAnnouncement(at)
	if !ok || !stage.Announces() || generation == 0 {
		return
	}
	notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	outcome, err := notifications.Dispatch(
		notifyCtx,
		notification.Input{
			Incident: event.Incident{
				IncidentID: stage.IncidentID(),
				Status:     event.IncidentOpened,
				// Deliberately not critical: critical bypasses the night
				// window, and a deadline two days out is not worth waking
				// anyone for.
				Severity:   event.SeverityWarning,
				Category:   event.IncidentPolicyExpiry,
				Component:  control.ComponentRuntime,
				Generation: generation,
			},
			External: notification.ExternalNotRequired,
			// A policy generation is the same number in the next process, so a
			// crossing announced for it must not be announced again.
			DurableGeneration: true,
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

func emitSummary(
	logger *logging.Logger,
	gate *logging.ChangeGate,
	summary Summary,
) error {
	result := logging.ResultDegraded
	switch {
	case summary.Plan.State == control.StateSuspended:
		result = logging.ResultSuspended
	case summary.Failures == 0 &&
		summary.Plan.State == control.StateHealthy &&
		summary.Plan.Action == pritunlplan.ActionNone:
		result = logging.ResultOK
	}
	if gate.Changed(logging.EventObservationCycle, string(result)) {
		if err := logger.Emit(
			logging.LevelInfo,
			logging.EventObservationCycle,
			result,
			"",
		); err != nil {
			return err
		}
	}
	if summary.Plan.Action != pritunlplan.ActionNone {
		// A standing proposal is one proposal. It is reported when it appears
		// and again if it lapses and returns, not once a minute for as long as
		// the condition holds. An act, by contrast, is reported every time it
		// happens: it is an event and not a state.
		key := string(summary.Plan.Action) + ":" + string(summary.Outcome)
		if summary.Outcome == recoveryProposed &&
			!gate.Changed(logging.EventPritunlReconnect, key) {
			return nil
		}
		gate.Changed(logging.EventPritunlReconnect, key)
		return logger.Emit(
			logging.LevelInfo,
			logging.EventPritunlReconnect,
			outcomeResult(summary.Outcome),
			// A rejected event carries a reason or the logger refuses it, and
			// an error here ends the observe loop. The one outcome this daemon
			// exists to record — the other side looked and disagreed — must not
			// be the one it cannot write down.
			outcomeReason(summary.Outcome),
		)
	}
	gate.Changed(logging.EventPritunlReconnect, "")
	return nil
}

// outcomeResult reports what became of a decision.
//
// An authorized act that could not be performed is degraded rather than
// proposed: an authority that cannot be exercised is a fault in the deployment,
// and reporting it as a proposal would hide it behind the pre-cutover state.
// outcomeReason names a rejected outcome, and nothing else.
//
// The logger pairs them strictly: a rejected result without a reason is refused,
// and so is any other result carrying one.
func outcomeReason(outcome recoveryOutcome) logging.Reason {
	switch outcome {
	case recoveryRefusedAuthority:
		return logging.ReasonUnsignedAuthority
	case recoveryRefusedStale:
		return logging.ReasonGenerationConflict
	case recoveryRefusedRequest:
		return logging.ReasonMalformedRequest
	case recoveryRefused, recoveryRefusedPrecondition:
		// Root's own checks were not satisfied and the code cannot say which.
		// Its log names the one that refused, beside the same refusal.
		return logging.ReasonRecoveryRefused
	case recoveryUnequipped:
		// An authority this runtime was granted and cannot exercise. That is a
		// fault in the deployment, and it is not the same as an attempt that
		// did not work — they send the reader to different places.
		return logging.ReasonRecoveryUnequipped
	case recoveryFailed:
		return logging.ReasonRecoveryFailed
	default:
		return ""
	}
}

func outcomeResult(outcome recoveryOutcome) logging.Result {
	switch outcome {
	case recoveryDone:
		return logging.ResultOK
	case recoveryRefused, recoveryRefusedAuthority, recoveryRefusedStale,
		recoveryRefusedPrecondition, recoveryRefusedRequest:
		return logging.ResultRejected
	case recoveryFailed, recoveryUnequipped:
		return logging.ResultDegraded
	default:
		return logging.ResultProposed
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
