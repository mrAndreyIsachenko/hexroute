package cloudruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAPIRuntimeRequiresExclusiveRolesAndBuildsReadOnlySurface(
	t *testing.T,
) {
	adminURL := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	ingestURL := os.Getenv("HEXROUTE_TEST_POSTGRES_INGEST_DSN")
	dashboardURL := os.Getenv("HEXROUTE_TEST_POSTGRES_DASHBOARD_DSN")
	authURL := os.Getenv("HEXROUTE_TEST_POSTGRES_AUTH_DSN")
	if adminURL == "" || ingestURL == "" || dashboardURL == "" || authURL == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("admin pgxpool.New() error = %v", err)
	}
	t.Cleanup(admin.Close)
	now := time.Now().UTC()
	if _, err := admin.Exec(
		ctx,
		"DELETE FROM worker_heartbeats WHERE worker_name = 'primary'",
	); err != nil {
		t.Fatalf("clear worker heartbeat: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO worker_heartbeats (
			worker_name,
			instance_id,
			application_version,
			started_at,
			heartbeat_at
		) VALUES (
			'primary',
			'77777777-7777-4777-8777-777777777777',
			'integration',
			$1,
			$1
		)
	`, now); err != nil {
		t.Fatalf("seed worker heartbeat: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, cleanupErr := admin.Exec(
			cleanupContext,
			"DELETE FROM worker_heartbeats WHERE worker_name = 'primary'",
		); cleanupErr != nil {
			t.Errorf("cleanup worker heartbeat: %v", cleanupErr)
		}
	})
	config := APIConfig{
		ListenAddress:        ":8080",
		PublicOrigin:         "https://status.example",
		ExpectedHost:         "status.example",
		RelyingPartyID:       "status.example",
		BootstrapSecret:      "0123456789abcdef0123456789abcdef",
		IngestDatabaseURL:    ingestURL,
		DashboardDatabaseURL: dashboardURL,
		AuthDatabaseURL:      authURL,
		WorkerName:           "primary",
		IngestTolerance:      5 * time.Minute,
		WorkerStaleAfter:     2 * time.Minute,
		FutureTolerance:      15 * time.Second,
		ShutdownTimeout:      10 * time.Second,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("APIConfig.Validate() error = %v", err)
	}
	pools, err := openAPIPools(ctx, config)
	if err != nil {
		t.Fatalf("openAPIPools() error = %v", err)
	}
	t.Cleanup(pools.close)
	handler, err := buildAPIHandler(config, pools)
	if err != nil {
		t.Fatalf("buildAPIHandler() error = %v", err)
	}
	tests := []struct {
		name   string
		method string
		path   string
		host   string
		status int
	}{
		{
			name:   "liveness",
			method: http.MethodGet,
			path:   "/livez",
			host:   "status.example",
			status: http.StatusOK,
		},
		{
			name:   "readiness",
			method: http.MethodGet,
			path:   "/readyz",
			host:   "status.example",
			status: http.StatusOK,
		},
		{
			name:   "dashboard requires auth",
			method: http.MethodGet,
			path:   "/",
			host:   "status.example",
			status: http.StatusSeeOther,
		},
		{
			name:   "no control surface",
			method: http.MethodPost,
			path:   "/restart",
			host:   "status.example",
			status: http.StatusNotFound,
		},
		{
			name:   "platform readiness probe",
			method: http.MethodGet,
			path:   "/readyz",
			host:   "provider.example",
			status: http.StatusOK,
		},
		{
			name:   "alternate application host",
			method: http.MethodGet,
			path:   "/",
			host:   "provider.example",
			status: http.StatusMisdirectedRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				test.method,
				"https://"+test.host+test.path,
				nil,
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf(
					"%s %s status = %d body=%q",
					test.method,
					test.path,
					response.Code,
					response.Body.String(),
				)
			}
		})
	}

	mismatched := config
	mismatched.DashboardDatabaseURL = ingestURL
	if pools, err := openAPIPools(ctx, mismatched); err == nil {
		pools.close()
		t.Fatal("openAPIPools(mismatched role) succeeded")
	}
}
