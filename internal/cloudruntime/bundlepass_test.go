package cloudruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// TestPartialBundleStorageIsRefused separates the two states a deployment can
// honestly be in — given storage, or not given storage — from the one it can
// only be in by mistake. Starting on a partial configuration would present a
// misconfigured deployment as a merely unconfigured one, and the log it writes
// says the same thing in both cases.
func TestPartialBundleStorageIsRefused(t *testing.T) {
	whole := func() map[string]string {
		values := validWorkerEnvironment()
		values["HEXROUTE_BUNDLE_ENDPOINT"] = "https://fra1.example.invalid"
		values["HEXROUTE_BUNDLE_REGION"] = "fra1"
		values["HEXROUTE_BUNDLE_BUCKET"] = "hexroute-bundles"
		values["HEXROUTE_BUNDLE_ACCESS_KEY_ID"] = "EXAMPLEKEYIDENTIFIER"
		values["HEXROUTE_BUNDLE_SECRET_KEY"] = "not-a-secret-only-a-fixture"
		return values
	}

	configured, err := LoadWorkerConfig(mapEnvironment(whole()))
	if err != nil {
		t.Fatalf("LoadWorkerConfig() with whole storage error = %v", err)
	}
	if !configured.BundleStorageConfigured() {
		t.Fatal("whole bundle storage did not report as configured")
	}

	absent, err := LoadWorkerConfig(mapEnvironment(validWorkerEnvironment()))
	if err != nil {
		t.Fatalf("LoadWorkerConfig() without storage error = %v", err)
	}
	if absent.BundleStorageConfigured() {
		t.Fatal("absent bundle storage reported as configured")
	}

	for _, missing := range []string{
		"HEXROUTE_BUNDLE_ENDPOINT",
		"HEXROUTE_BUNDLE_REGION",
		"HEXROUTE_BUNDLE_BUCKET",
		"HEXROUTE_BUNDLE_ACCESS_KEY_ID",
		"HEXROUTE_BUNDLE_SECRET_KEY",
	} {
		values := whole()
		delete(values, missing)
		if _, err := LoadWorkerConfig(mapEnvironment(values)); err == nil {
			t.Fatalf("LoadWorkerConfig() without %s was accepted", missing)
		}
	}
}

// TestAnUnconfiguredPassSaysSoEveryInterval is the difference between a
// deployment that was never finished and a deployment with nothing to bundle.
// Both create no bundle; only one of them is a mistake, and without this
// record they are the same silence.
func TestAnUnconfiguredPassSaysSoEveryInterval(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentIngest)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	config, err := LoadWorkerConfig(mapEnvironment(validWorkerEnvironment()))
	if err != nil {
		t.Fatalf("LoadWorkerConfig() error = %v", err)
	}

	// A nil pool would be refused, so the pass is built the way the worker
	// builds it and only the storage is absent.
	job, err := newBundlePass(
		config,
		nonNilPool(t),
		logger,
		workerInstance(t),
		func() time.Time { return time.Unix(0, 0).UTC() },
	)
	if err != nil {
		t.Fatalf("newBundlePass() error = %v", err)
	}
	if job.event != logging.EventCloudIncidentBundleUnconfigured {
		t.Fatalf("unconfigured pass named %q", job.event)
	}
	if job.interval <= 0 || job.timeout <= 0 {
		t.Fatal("unconfigured pass would be refused by the worker")
	}
	for range 2 {
		if err := job.run(context.Background()); err != nil {
			t.Fatalf("run() error = %v", err)
		}
	}
	written := output.String()
	if strings.Count(
		written,
		string(logging.EventCloudIncidentBundleUnconfigured),
	) != 2 {
		t.Fatalf("unconfigured pass recorded: %q", written)
	}
}

// nonNilPool builds a pool that is never connected to. The pass under test
// creates nothing and asks the database nothing; what matters is that the
// argument is real, so the test exercises the same refusal path the worker
// would hit rather than a nil check.
func nonNilPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(
		context.Background(),
		"postgres://unused:unused@127.0.0.1:1/unused",
	)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func workerInstance(t *testing.T) metadata.UUID {
	t.Helper()
	instanceID, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		t.Fatalf("metadata.NewUUID() error = %v", err)
	}
	return instanceID
}
