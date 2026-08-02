package rootdaemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

const (
	ConfigSchema  = "hexroute.root-observe.v1"
	MaxConfigSize = 64 * 1024
)

type EndpointPurpose string

const (
	PurposeOuterReady      EndpointPurpose = "outer_ready"
	PurposeNormalCodex     EndpointPurpose = "normal_codex"
	PurposeTwilightCodex   EndpointPurpose = "twilight_codex"
	maxTargets                             = 128
	maxEndpoints                           = 32
	minObservationInterval                 = 10 * time.Second
	maxObservationInterval                 = 5 * time.Minute
)

type Config struct {
	Schema                string                      `json:"schema"`
	Mode                  string                      `json:"mode"`
	ObservationIntervalS  uint32                      `json:"observation_interval_seconds"`
	OperatorUID           int                         `json:"operator_uid,omitempty"`
	PhysicalInterface     string                      `json:"physical_interface"`
	ManagedTUNAddress     string                      `json:"managed_tun_address"`
	UpstreamProbeAddress  string                      `json:"upstream_probe_address,omitempty"`
	ExpectedSingBoxParent int                         `json:"expected_sing_box_parent_pid,omitempty"`
	Routes                []RouteConfig               `json:"routes"`
	Endpoints             []EndpointConfig            `json:"endpoints"`
	PolicyControl         *policycontrol.StaticConfig `json:"policy_control,omitempty"`
}

type RouteConfig struct {
	Name          string           `json:"name"`
	Address       string           `json:"address"`
	Role          routeplan.Role   `json:"role"`
	PreferredLink safety.LinkClass `json:"preferred_link,omitempty"`
}

type EndpointConfig struct {
	Name           string                    `json:"name"`
	Purpose        EndpointPurpose           `json:"purpose"`
	Transport      observe.Transport         `json:"transport"`
	Certificate    observe.CertificatePolicy `json:"certificate_policy"`
	Address        string                    `json:"address"`
	ProxyAddress   string                    `json:"proxy_address,omitempty"`
	ServerName     string                    `json:"server_name"`
	TimeoutSeconds uint32                    `json:"timeout_seconds"`
}

type RuntimeConfig struct {
	Interval              time.Duration
	OperatorUID           int
	PhysicalInterface     string
	ManagedTUNAddress     netip.Addr
	UpstreamProbeAddress  netip.Addr
	ExpectedSingBoxParent int
	Targets               []routeplan.Target
	Endpoints             []RuntimeEndpoint
	PolicyControl         *policycontrol.RuntimeConfig
}

type RuntimeEndpoint struct {
	Purpose  EndpointPurpose
	Endpoint observe.Endpoint
}

var (
	ErrInvalidConfig      = errors.New("invalid root observe configuration")
	configInterfaceFormat = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
)

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
	if config.Schema != ConfigSchema ||
		config.Mode != "observe-only" ||
		interval < minObservationInterval ||
		interval > maxObservationInterval ||
		!configInterfaceFormat.MatchString(config.PhysicalInterface) ||
		strings.HasPrefix(config.PhysicalInterface, "utun") ||
		len(config.Routes) == 0 ||
		len(config.Routes) > maxTargets ||
		len(config.Endpoints) == 0 ||
		len(config.Endpoints) > maxEndpoints ||
		config.OperatorUID < 0 ||
		config.ExpectedSingBoxParent < 0 {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	tunAddress, err := netip.ParseAddr(config.ManagedTUNAddress)
	if err != nil || !tunAddress.Is4() {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	var upstreamAddress netip.Addr
	if config.UpstreamProbeAddress != "" {
		upstreamAddress, err = netip.ParseAddr(config.UpstreamProbeAddress)
		if err != nil || !upstreamAddress.Is4() {
			return RuntimeConfig{}, ErrInvalidConfig
		}
	}

	runtime := RuntimeConfig{
		Interval:              interval,
		OperatorUID:           config.OperatorUID,
		PhysicalInterface:     config.PhysicalInterface,
		ManagedTUNAddress:     tunAddress,
		UpstreamProbeAddress:  upstreamAddress,
		ExpectedSingBoxParent: config.ExpectedSingBoxParent,
	}
	if config.PolicyControl != nil {
		policyRuntime, err := config.PolicyControl.Runtime(policy.DomainRoot)
		if err != nil {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		runtime.PolicyControl = &policyRuntime
	}
	names := make(map[string]struct{})
	addresses := make(map[netip.Addr]struct{})
	needsUpstream := false
	needsCodex := false
	for _, route := range config.Routes {
		address, err := netip.ParseAddr(route.Address)
		if err != nil || !address.Is4() {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		if _, exists := names[route.Name]; exists {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		if _, exists := addresses[address]; exists {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		names[route.Name] = struct{}{}
		addresses[address] = struct{}{}
		target := routeplan.Target{
			Name:        route.Name,
			Destination: address,
			Role:        route.Role,
			Preferred:   route.PreferredLink,
		}
		if target.Role == routeplan.RoleIngress && target.Preferred == safety.LinkUpstreamVPN {
			needsUpstream = true
		}
		if target.Role == routeplan.RoleCodexFallback {
			needsCodex = true
		}
		runtime.Targets = append(runtime.Targets, target)
	}
	if needsUpstream && !runtime.UpstreamProbeAddress.IsValid() {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	purposes := make(map[EndpointPurpose]uint32)
	endpointNames := make(map[string]struct{})
	for _, endpointConfig := range config.Endpoints {
		purpose := endpointConfig.Purpose
		if !validPurpose(purpose) ||
			endpointConfig.Transport == "" ||
			endpointConfig.Certificate == "" {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		address, err := netip.ParseAddrPort(endpointConfig.Address)
		if err != nil {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		var proxyAddress netip.AddrPort
		if endpointConfig.ProxyAddress != "" {
			proxyAddress, err = netip.ParseAddrPort(endpointConfig.ProxyAddress)
			if err != nil {
				return RuntimeConfig{}, ErrInvalidConfig
			}
		}
		endpoint := observe.Endpoint{
			Name:         endpointConfig.Name,
			Transport:    endpointConfig.Transport,
			Certificate:  endpointConfig.Certificate,
			Address:      address,
			ProxyAddress: proxyAddress,
			ServerName:   endpointConfig.ServerName,
			Timeout:      time.Duration(endpointConfig.TimeoutSeconds) * time.Second,
		}
		if err := endpoint.Validate(); err != nil {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		if _, exists := endpointNames[endpoint.Name]; exists {
			return RuntimeConfig{}, ErrInvalidConfig
		}
		endpointNames[endpoint.Name] = struct{}{}
		purposes[purpose]++
		runtime.Endpoints = append(runtime.Endpoints, RuntimeEndpoint{
			Purpose:  purpose,
			Endpoint: endpoint,
		})
	}
	if purposes[PurposeOuterReady] == 0 ||
		(needsCodex && (purposes[PurposeNormalCodex] != 1 || purposes[PurposeTwilightCodex] != 1)) {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	for _, endpoint := range runtime.Endpoints {
		switch endpoint.Purpose {
		case PurposeNormalCodex:
			if endpoint.Endpoint.Transport != observe.TransportDirectTLS ||
				endpoint.Endpoint.Certificate != observe.CertificateVerify {
				return RuntimeConfig{}, ErrInvalidConfig
			}
		case PurposeTwilightCodex:
			if endpoint.Endpoint.Transport != observe.TransportSOCKS5TLS ||
				endpoint.Endpoint.Certificate != observe.CertificateVerify {
				return RuntimeConfig{}, ErrInvalidConfig
			}
		}
	}
	validationInput := routeplan.Input{
		Targets: runtime.Targets,
		Physical: routeplan.Path{
			Link:      safety.LinkPhysical,
			Interface: config.PhysicalInterface,
			Gateway:   netip.MustParseAddr("192.0.2.1"),
		},
		TUN: routeplan.Path{
			Link:      safety.LinkTwilightTUN,
			Interface: "utun99",
		},
		Codex: routeplan.CodexState{NormalReady: true},
	}
	if needsUpstream {
		validationInput.Upstream = &routeplan.Path{
			Link:      safety.LinkUpstreamVPN,
			Interface: "utun98",
		}
	}
	if _, err := routeplan.Build(validationInput); err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	return runtime, nil
}

func validPurpose(purpose EndpointPurpose) bool {
	switch purpose {
	case PurposeOuterReady, PurposeNormalCodex, PurposeTwilightCodex:
		return true
	default:
		return false
	}
}
