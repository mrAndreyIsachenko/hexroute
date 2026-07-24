package sentinel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const (
	ConfigSchema  = "hexroute.sentinel-observe.v1"
	MaxConfigSize = 32 * 1024
)

type Config struct {
	Schema               string         `json:"schema"`
	Mode                 string         `json:"mode"`
	ObservationIntervalS uint32         `json:"observation_interval_seconds"`
	StaleThresholdS      uint32         `json:"stale_threshold_seconds"`
	HeartbeatPath        string         `json:"heartbeat_path"`
	DataPathEndpoint     EndpointConfig `json:"data_path_endpoint"`
}

type EndpointConfig struct {
	Transport      observe.Transport         `json:"transport"`
	Certificate    observe.CertificatePolicy `json:"certificate_policy"`
	Address        string                    `json:"address"`
	ProxyAddress   string                    `json:"proxy_address"`
	ServerName     string                    `json:"server_name"`
	TimeoutSeconds uint32                    `json:"timeout_seconds"`
}

type RuntimeConfig struct {
	Interval         time.Duration
	StaleThreshold   control.Tick
	HeartbeatPath    string
	DataPathEndpoint observe.Endpoint
}

var ErrInvalidConfig = errors.New("invalid sentinel observe configuration")

func LoadConfig(path string) (RuntimeConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	defer file.Close()
	return DecodeConfig(file)
}

func DecodeConfig(reader io.Reader) (RuntimeConfig, error) {
	content, err := io.ReadAll(io.LimitReader(reader, MaxConfigSize+1))
	if err != nil || len(content) == 0 || len(content) > MaxConfigSize {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	return config.runtime()
}

func (config Config) runtime() (RuntimeConfig, error) {
	interval := time.Duration(config.ObservationIntervalS) * time.Second
	stale := control.Tick(config.StaleThresholdS)
	if config.Schema != ConfigSchema ||
		config.Mode != "observe-only" ||
		interval < 10*time.Second ||
		interval > time.Minute ||
		stale < control.Tick(2*config.ObservationIntervalS) ||
		stale > 600 ||
		!filepath.IsAbs(config.HeartbeatPath) ||
		filepath.Clean(config.HeartbeatPath) != config.HeartbeatPath ||
		filepath.Base(config.HeartbeatPath) != heartbeat.FileName {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	address, err := netip.ParseAddrPort(config.DataPathEndpoint.Address)
	if err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	proxyAddress, err := netip.ParseAddrPort(config.DataPathEndpoint.ProxyAddress)
	if err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	endpoint := observe.Endpoint{
		Name:         "legacy-twilight",
		Transport:    config.DataPathEndpoint.Transport,
		Certificate:  config.DataPathEndpoint.Certificate,
		Address:      address,
		ProxyAddress: proxyAddress,
		ServerName:   config.DataPathEndpoint.ServerName,
		Timeout:      time.Duration(config.DataPathEndpoint.TimeoutSeconds) * time.Second,
	}
	if endpoint.Transport != observe.TransportSOCKS5TLS ||
		endpoint.Certificate != observe.CertificateVerify ||
		!endpoint.Address.Addr().Is4() ||
		!endpoint.ProxyAddress.Addr().IsLoopback() ||
		endpoint.Validate() != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	return RuntimeConfig{
		Interval:         interval,
		StaleThreshold:   stale,
		HeartbeatPath:    config.HeartbeatPath,
		DataPathEndpoint: endpoint,
	}, nil
}
