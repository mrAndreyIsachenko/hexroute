package cloudruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/incidentbundle"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/objectstore"
)

// bundleBatch bounds both halves of one pass: how many closed incidents may be
// bundled and how many expired bundles may be claimed for deletion.
const bundleBatch = 16

// newBundlePass builds the pass that assembles evidence for closed incidents
// and removes bundles that have reached their recorded expiry.
//
// The pass exists whether or not storage was configured. Without storage it
// creates nothing and says so on every interval, because a deployment that was
// never finished and a deployment with nothing to bundle otherwise produce the
// same logs — none — and the first is a mistake while the second is the normal
// state of a quiet week.
func newBundlePass(
	config WorkerConfig,
	pool *pgxpool.Pool,
	logger *logging.Logger,
	instanceID metadata.UUID,
	now func() time.Time,
) (workerJob, error) {
	if pool == nil || logger == nil || now == nil {
		return workerJob{}, ErrWorkerRuntime
	}
	if !config.BundleStorageConfigured() {
		return workerJob{
			event:    logging.EventCloudIncidentBundleUnconfigured,
			interval: config.RetentionInterval,
			timeout:  config.JobTimeout,
			run: func(context.Context) error {
				return logger.Emit(
					logging.LevelInfo,
					logging.EventCloudIncidentBundleUnconfigured,
					logging.ResultSuspended,
					"",
				)
			},
		}, nil
	}
	storage, err := objectstore.New(objectstore.Config{
		Endpoint:    config.BundleEndpoint,
		Region:      config.BundleRegion,
		Bucket:      config.BundleBucket,
		AccessKeyID: config.BundleAccessKeyID,
		SecretKey:   config.BundleSecretKey,
	})
	if err != nil {
		return workerJob{}, ErrWorkerRuntime
	}
	store, err := incidentbundle.NewPostgresStore(pool)
	if err != nil {
		return workerJob{}, ErrWorkerRuntime
	}
	creator, err := incidentbundle.NewCreator(pool, storage, rand.Reader)
	if err != nil {
		return workerJob{}, ErrWorkerRuntime
	}
	expiry, err := incidentbundle.NewExpiryWorker(
		store,
		storage,
		instanceID,
		now,
		bundleBatch,
	)
	if err != nil {
		return workerJob{}, ErrWorkerRuntime
	}
	return workerJob{
		event:    logging.EventCloudIncidentBundle,
		interval: config.RetentionInterval,
		timeout:  config.JobTimeout,
		run: func(jobContext context.Context) error {
			return runBundlePass(jobContext, store, creator, expiry, now)
		},
	}, nil
}

func runBundlePass(
	ctx context.Context,
	store *incidentbundle.PostgresStore,
	creator *incidentbundle.Creator,
	expiry *incidentbundle.ExpiryWorker,
	now func() time.Time,
) error {
	pending, err := store.PendingClosedIncidents(ctx, bundleBatch)
	if err != nil {
		return err
	}
	var failure error
	for _, incidentID := range pending {
		if _, err := creator.Create(ctx, incidentID, now().UTC()); err != nil {
			// A single incident's failure must not hold back the rest of the
			// batch or the expiry half of the pass: storage refusing one
			// object leaves the other bundles assemblable, and an expiry that
			// is due stays due. The first failure is what the job reports.
			if failure == nil && !errors.Is(err, context.Canceled) {
				failure = err
			}
			continue
		}
	}
	if _, err := expiry.RunOnce(ctx); err != nil && failure == nil {
		failure = err
	}
	return failure
}
