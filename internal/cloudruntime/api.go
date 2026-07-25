package cloudruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudhealth"
	"github.com/mrAndreyIsachenko/hexroute/internal/cloudingest"
	"github.com/mrAndreyIsachenko/hexroute/internal/dashboard"
	"github.com/mrAndreyIsachenko/hexroute/internal/dashboardauth"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

const (
	apiStartupTimeout = 15 * time.Second
	maxHeaderBytes    = 16 * 1024
)

type apiPools struct {
	ingest    *pgxpool.Pool
	dashboard *pgxpool.Pool
	auth      *pgxpool.Pool
}

var ErrAPIRuntime = errors.New("cloud API runtime unavailable")

func RunAPI(
	ctx context.Context,
	config APIConfig,
	logger *logging.Logger,
) error {
	if ctx == nil || logger == nil || config.Validate() != nil {
		return ErrAPIRuntime
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, apiStartupTimeout)
	defer cancelStartup()
	pools, err := openAPIPools(startupContext, config)
	if err != nil {
		return ErrAPIRuntime
	}
	defer pools.close()
	handler, err := buildAPIHandler(config, pools)
	if err != nil {
		return ErrAPIRuntime
	}
	listener, err := (&net.ListenConfig{}).Listen(
		startupContext,
		"tcp",
		config.ListenAddress,
	)
	if err != nil {
		return ErrAPIRuntime
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventCloudAPIStarted,
		logging.ResultOK,
		"",
	); err != nil {
		return ErrAPIRuntime
	}
	if err := serveUntilCanceled(
		ctx,
		server,
		listener,
		config.ShutdownTimeout,
	); err != nil {
		return ErrAPIRuntime
	}
	if err := logger.Emit(
		logging.LevelInfo,
		logging.EventCloudAPIStopped,
		logging.ResultOK,
		"",
	); err != nil {
		return ErrAPIRuntime
	}
	return nil
}

func openAPIPools(
	ctx context.Context,
	config APIConfig,
) (apiPools, error) {
	var pools apiPools
	var err error
	pools.ingest, err = openPool(ctx, config.IngestDatabaseURL, 8)
	if err != nil {
		return apiPools{}, err
	}
	pools.dashboard, err = openPool(ctx, config.DashboardDatabaseURL, 4)
	if err != nil {
		pools.close()
		return apiPools{}, err
	}
	pools.auth, err = openPool(ctx, config.AuthDatabaseURL, 4)
	if err != nil {
		pools.close()
		return apiPools{}, err
	}
	checks := []struct {
		pool *pgxpool.Pool
		role databaseRole
	}{
		{pool: pools.ingest, role: roleIngest},
		{pool: pools.dashboard, role: roleDashboard},
		{pool: pools.auth, role: roleDashboardAuth},
	}
	for _, check := range checks {
		if err := check.pool.Ping(ctx); err != nil {
			pools.close()
			return apiPools{}, ErrAPIRuntime
		}
		if err := requireExclusiveRole(ctx, check.pool, check.role); err != nil {
			pools.close()
			return apiPools{}, err
		}
	}
	return pools, nil
}

func openPool(
	ctx context.Context,
	databaseURL string,
	maxConnections int32,
) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	config.MaxConns = maxConnections
	config.MinConns = 0
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = time.Minute
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	return pool, nil
}

func (pools apiPools) close() {
	if pools.auth != nil {
		pools.auth.Close()
	}
	if pools.dashboard != nil {
		pools.dashboard.Close()
	}
	if pools.ingest != nil {
		pools.ingest.Close()
	}
}

func buildAPIHandler(
	config APIConfig,
	pools apiPools,
) (http.Handler, error) {
	ingestStore, err := cloudingest.NewPostgresStore(pools.ingest, rand.Reader)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	ingestService, err := cloudingest.NewService(
		ingestStore,
		config.IngestTolerance,
		rand.Reader,
		time.Now,
	)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	ingestHandler, err := cloudingest.NewHTTPHandler(ingestService)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	healthStore, err := cloudhealth.NewPostgresStore(pools.ingest)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	checker, err := cloudhealth.NewChecker(
		healthStore,
		config.WorkerName,
		config.WorkerStaleAfter,
		config.FutureTolerance,
		time.Now,
	)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	readiness, err := cloudhealth.NewHandler(checker)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	authStore, err := dashboardauth.NewPostgresStore(pools.auth, rand.Reader)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	ceremony, err := dashboardauth.NewWebAuthnCeremony(
		config.RelyingPartyID,
		config.PublicOrigin,
	)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	auth, err := dashboardauth.NewHandler(dashboardauth.Config{
		Store:           authStore,
		Ceremony:        ceremony,
		Origin:          config.PublicOrigin,
		BootstrapSecret: config.BootstrapSecret,
		Random:          rand.Reader,
		Now:             time.Now,
	})
	if err != nil {
		return nil, ErrAPIRuntime
	}
	dashboardStore, err := dashboard.NewPostgresStore(
		pools.dashboard,
		config.WorkerStaleAfter,
	)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	pages, err := dashboard.NewHandler(dashboardStore, auth, time.Now)
	if err != nil {
		return nil, ErrAPIRuntime
	}
	dashboardRouter, err := dashboard.NewRouter(pages, auth)
	if err != nil {
		return nil, ErrAPIRuntime
	}

	mux := http.NewServeMux()
	mux.Handle("/livez", http.HandlerFunc(liveness))
	mux.Handle("/readyz", readiness)
	mux.Handle(cloudingest.IngestPath, ingestHandler)
	mux.Handle("/", dashboardRouter)
	return bindPublicHost(mux, config.ExpectedHost, config.ProviderHost), nil
}

func bindPublicHost(
	next http.Handler,
	expectedHost string,
	providerHost string,
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Strict-Transport-Security", "max-age=31536000")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		platformHealthProbe := request.URL.Path == "/livez" ||
			request.URL.Path == "/readyz"
		if !platformHealthProbe &&
			!strings.EqualFold(request.Host, expectedHost) &&
			(providerHost == "" || !strings.EqualFold(request.Host, providerHost)) {
			response.Header().Set("Cache-Control", "no-store")
			http.Error(response, "not found", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func liveness(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		response.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(response, `{"status":"not_live"}`+"\n")
		return
	}
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, `{"status":"live"}`+"\n")
}

func serveUntilCanceled(
	ctx context.Context,
	server *http.Server,
	listener net.Listener,
	shutdownTimeout time.Duration,
) error {
	if ctx == nil ||
		server == nil ||
		listener == nil ||
		shutdownTimeout <= 0 {
		return ErrAPIRuntime
	}
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(listener)
	}()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return err
		}
		err := <-result
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
