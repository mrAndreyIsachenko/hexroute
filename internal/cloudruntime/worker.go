package cloudruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/alertdelivery"
	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/cloudconnectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/cloudhealth"
	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/cutoverfreeze"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/retention"
	"github.com/mrAndreyIsachenko/hexroute/internal/silentnode"
	"github.com/mrAndreyIsachenko/hexroute/internal/slo"
)

const (
	workerStartupTimeout = 15 * time.Second
	workerDatabaseConns  = 8
	workerBatchSize      = 50
	sleepProjectionBatch = 100
	// connectivityProjectionBatch bounds one connectivity read-model pass.
	connectivityProjectionBatch = 100
	// sloLookback bounds how far a run reaches for windows never computed.
	// Detail events are evicted after thirty days, so reaching past that finds
	// nothing; a day keeps a restarted worker from asking anyway.
	sloLookback        = 24 * time.Hour
	retentionBatchSize = 500
)

type heartbeatRunner interface {
	Run(context.Context) error
}

type heartbeatOnce interface {
	Once(context.Context) error
}

type freezeAwareHeartbeat struct {
	writer   heartbeatOnce
	freeze   cutoverfreeze.Reader
	interval time.Duration
}

type workerJob struct {
	event    logging.EventName
	interval time.Duration
	timeout  time.Duration
	run      func(context.Context) error
}

type workerRuntime struct {
	heartbeat heartbeatRunner
	jobs      []workerJob
	logger    *logging.Logger
	freeze    cutoverfreeze.Reader
}

var ErrWorkerRuntime = errors.New("cloud worker runtime unavailable")

func RunWorker(
	ctx context.Context,
	config WorkerConfig,
	logger *logging.Logger,
) error {
	if ctx == nil || logger == nil || config.Validate() != nil {
		return ErrWorkerRuntime
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, workerStartupTimeout)
	defer cancelStartup()
	pool, err := openPool(
		startupContext,
		config.MaintenanceDatabaseURL,
		workerDatabaseConns,
	)
	if err != nil {
		return ErrWorkerRuntime
	}
	defer pool.Close()
	if err := pool.Ping(startupContext); err != nil {
		return ErrWorkerRuntime
	}
	if err := requireExclusiveRole(
		startupContext,
		pool,
		roleMaintenance,
	); err != nil {
		return ErrWorkerRuntime
	}
	cancelStartup()
	runtime, err := buildWorkerRuntime(config, pool, logger, time.Now)
	if err != nil {
		return ErrWorkerRuntime
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventCloudWorkerStarted,
		logging.ResultOK,
		"",
	); err != nil {
		return ErrWorkerRuntime
	}
	if err := runtime.run(ctx, config.ShutdownTimeout); err != nil {
		return ErrWorkerRuntime
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventCloudWorkerStopped,
		logging.ResultOK,
		"",
	); err != nil {
		return ErrWorkerRuntime
	}
	return nil
}

func buildWorkerRuntime(
	config WorkerConfig,
	pool *pgxpool.Pool,
	logger *logging.Logger,
	now func() time.Time,
) (*workerRuntime, error) {
	if pool == nil || logger == nil || now == nil || config.Validate() != nil {
		return nil, ErrWorkerRuntime
	}
	instanceID, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	healthStore, err := cloudhealth.NewPostgresStore(pool)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	freezeStore, err := cutoverfreeze.NewStore(pool)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	startedAt := now().UTC()
	heartbeatWriter, err := cloudhealth.NewWriter(
		healthStore,
		config.WorkerName,
		instanceID,
		buildinfo.Version,
		startedAt,
		config.HeartbeatInterval,
		now,
	)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	heartbeat := &freezeAwareHeartbeat{
		writer:   heartbeatWriter,
		freeze:   freezeStore,
		interval: config.HeartbeatInterval,
	}
	sleepStore, err := silentnode.NewPostgresStore(pool)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	incidentStore, err := cloudincident.NewPostgresStore(pool, rand.Reader)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	silentPolicy := silentnode.Policy{
		MissedHeartbeats: 3,
		MinimumGrace:     time.Minute,
		FutureTolerance:  15 * time.Second,
	}
	alertPolicy := alertdelivery.Policy{
		NightStartHour: 23,
		NightEndHour:   8,
		Location:       config.Location,
		LeaseDuration:  2 * time.Minute,
		RetryMinimum:   time.Minute,
		RetryMaximum:   time.Hour,
	}
	alertStore, err := alertdelivery.NewPostgresStore(
		pool,
		alertPolicy,
		rand.Reader,
	)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	telegram, err := alertdelivery.NewTelegramClient(
		&http.Client{Timeout: 10 * time.Second},
		config.TelegramBotToken,
		config.TelegramChatID,
	)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	processor, err := alertdelivery.NewProcessor(
		alertStore,
		telegram,
		instanceID,
		now,
		workerBatchSize,
	)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	retentionWorker, err := retention.NewWorker(pool, retentionBatchSize)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	connectivityStore, err := cloudconnectivity.NewPostgresStore(pool)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	sloStore, err := slo.NewPostgresStore(pool, rand.Reader)
	if err != nil {
		return nil, ErrWorkerRuntime
	}
	sloWorker, err := slo.NewWorker(sloStore, sloLookback)
	if err != nil {
		return nil, ErrWorkerRuntime
	}

	bundlePass, err := newBundlePass(config, pool, logger, instanceID, now)
	if err != nil {
		return nil, ErrWorkerRuntime
	}

	jobs := []workerJob{
		{
			event:    logging.EventCloudReconcile,
			interval: config.ReconcileInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				if _, err := sleepStore.ProjectPendingSleepEvents(
					jobContext,
					sleepProjectionBatch,
				); err != nil {
					return err
				}
				at := now().UTC()
				decisions, err := sleepStore.Decisions(
					jobContext,
					silentPolicy,
					at,
				)
				if err != nil {
					return err
				}
				for _, decision := range decisions {
					if _, err := incidentStore.ReconcileSilent(
						jobContext,
						decision,
					); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			event:    logging.EventCloudAlertQueue,
			interval: config.AlertQueueInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				_, err := alertStore.DrainOutbox(
					jobContext,
					instanceID,
					now().UTC(),
					workerBatchSize,
				)
				return err
			},
		},
		{
			event:    logging.EventCloudAlertDelivery,
			interval: config.DeliveryInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				_, err := processor.RunOnce(jobContext)
				return err
			},
		},
		{
			// Folding uploaded projections is a pure read-model pass. It
			// derives nothing a host will ever read back, so a failure here
			// costs the dashboard a sample and costs the host nothing.
			event:    logging.EventCloudConnectivity,
			interval: config.ReconcileInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				_, err := connectivityStore.ProjectPending(
					jobContext,
					connectivityProjectionBatch,
				)
				return err
			},
		},
		{
			// The dashboard has rendered an empty SLO section since it was
			// written. Availability is measured from evidence already stored,
			// so this adds a calculation rather than a collection.
			event:    logging.EventCloudSLO,
			interval: config.ReconcileInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				_, err := sloWorker.RunOnce(jobContext, now().UTC())
				return err
			},
		},
		{
			event:    logging.EventCloudRetention,
			interval: config.RetentionInterval,
			timeout:  config.JobTimeout,
			run: func(jobContext context.Context) error {
				_, err := retentionWorker.RunOnce(jobContext, now().UTC())
				return err
			},
		},
		bundlePass,
	}
	return &workerRuntime{
		heartbeat: heartbeat,
		jobs:      jobs,
		logger:    logger,
		freeze:    freezeStore,
	}, nil
}

func (heartbeat *freezeAwareHeartbeat) Run(ctx context.Context) error {
	if heartbeat == nil || heartbeat.writer == nil ||
		heartbeat.freeze == nil || heartbeat.interval <= 0 {
		return ErrWorkerRuntime
	}
	run := func() error {
		state, err := heartbeat.freeze.Read(ctx)
		if err != nil || state.Frozen {
			return nil
		}
		return heartbeat.writer.Once(ctx)
	}
	if err := run(); err != nil {
		return err
	}
	ticker := time.NewTicker(heartbeat.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := run(); err != nil {
				return err
			}
		}
	}
}

func (runtime *workerRuntime) run(
	ctx context.Context,
	shutdownTimeout time.Duration,
) error {
	if runtime == nil ||
		runtime.heartbeat == nil ||
		runtime.logger == nil ||
		ctx == nil ||
		!durationBetween(shutdownTimeout, time.Second, time.Minute) {
		return ErrWorkerRuntime
	}
	for _, job := range runtime.jobs {
		if job.run == nil || job.interval <= 0 || job.timeout <= 0 {
			return ErrWorkerRuntime
		}
	}
	workContext, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	heartbeatResult := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		heartbeatResult <- runtime.heartbeat.Run(workContext)
	}()
	for _, job := range runtime.jobs {
		workers.Add(1)
		go func(current workerJob) {
			defer workers.Done()
			runtime.runJob(workContext, current)
		}(job)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case <-heartbeatResult:
		if workContext.Err() == nil {
			_ = runtime.logger.Emit(
				logging.LevelWarn,
				logging.EventCloudHeartbeat,
				logging.ResultDegraded,
				"",
			)
			runErr = ErrWorkerRuntime
		}
	}
	cancel()
	stopped := make(chan struct{})
	go func() {
		workers.Wait()
		close(stopped)
	}()
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case <-stopped:
		return runErr
	case <-timer.C:
		return ErrWorkerRuntime
	}
}

func (runtime *workerRuntime) runJob(
	ctx context.Context,
	job workerJob,
) {
	run := func() {
		jobContext, cancel := context.WithTimeout(ctx, job.timeout)
		defer cancel()
		if runtime.freeze != nil {
			state, err := runtime.freeze.Read(jobContext)
			if err != nil || state.Frozen {
				return
			}
		}
		if err := job.run(jobContext); err != nil && ctx.Err() == nil {
			_ = runtime.logger.Emit(
				logging.LevelWarn,
				job.event,
				logging.ResultDegraded,
				"",
			)
		}
	}
	run()
	ticker := time.NewTicker(job.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
