package ingressobserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressprobe"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

const (
	HeartbeatPath   = "/v1/heartbeat"
	dependencyLimit = 2 * time.Second
	shutdownLimit   = 5 * time.Second
)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

type Service struct {
	config Config
	key    signing.Key
	now    func() time.Time
	dial   dialContextFunc
	random io.Reader
}

func NewService(config Config) (*Service, error) {
	key, err := signing.LoadFile(config.KeyFile)
	if err != nil || key.NodeID != config.NodeID {
		return nil, ErrInvalidConfig
	}
	return &Service{
		config: config,
		key:    key,
		now:    time.Now,
		dial:   (&net.Dialer{}).DialContext,
		random: rand.Reader,
	}, nil
}

func (service *Service) Handler() http.Handler {
	return http.HandlerFunc(service.serveHeartbeat)
}

func (service *Service) Run(ctx context.Context) error {
	if service == nil || ctx == nil {
		return ErrInvalidConfig
	}
	listener, err := net.Listen("tcp", service.config.ListenAddr)
	if err != nil {
		return errors.New("observer listener unavailable")
	}
	server := &http.Server{
		Handler:           service.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("observer server stopped")
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownLimit)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return errors.New("observer shutdown failed")
		}
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("observer server stopped")
		}
		return nil
	}
}

func (service *Service) serveHeartbeat(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path != HeartbeatPath {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if request.ContentLength > 0 {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}

	observedAt := service.now().UTC()
	healthy := service.endpointHealthy(request.Context(), service.config.XRayEndpoint) &&
		service.endpointHealthy(request.Context(), service.config.OutboundEndpoint)
	body, err := json.Marshal(ingressprobe.Heartbeat{
		Schema:           ingressprobe.HeartbeatSchema,
		Version:          ingressprobe.HeartbeatVersion,
		NodeID:           service.key.NodeID,
		Generation:       service.config.Generation,
		ObservedAt:       observedAt.Format(time.RFC3339Nano),
		TransportHealthy: healthy,
	})
	if err != nil {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
		return
	}
	requestID, err := metadata.NewUUID(service.random)
	if err != nil {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
		return
	}
	signed, err := signing.Sign(service.key, requestID, observedAt, body)
	if err != nil {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(ingressprobe.SignedHeartbeat{
		Schema: ingressprobe.HeartbeatResponseSchema,
		Body:   body,
		Signed: signed,
	})
}

func (service *Service) endpointHealthy(parent context.Context, endpoint string) bool {
	if service == nil || service.dial == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, dependencyLimit)
	defer cancel()
	connection, err := service.dial(ctx, "tcp", endpoint)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
