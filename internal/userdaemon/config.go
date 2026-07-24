package userdaemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

const (
	ConfigSchema  = "hexroute.user-observe.v1"
	MaxConfigSize = 32 * 1024
)

type Config struct {
	Schema               string         `json:"schema"`
	Mode                 string         `json:"mode"`
	ObservationIntervalS uint32         `json:"observation_interval_seconds"`
	ExpectedUID          int            `json:"expected_uid"`
	ProfileID            string         `json:"profile_id"`
	PritunlCLI           string         `json:"pritunl_cli"`
	OuterEndpoint        EndpointConfig `json:"outer_endpoint"`
	Policy               PolicyConfig   `json:"policy"`
}

type EndpointConfig struct {
	Transport      observe.Transport         `json:"transport"`
	Certificate    observe.CertificatePolicy `json:"certificate_policy"`
	Address        string                    `json:"address"`
	ServerName     string                    `json:"server_name"`
	TimeoutSeconds uint32                    `json:"timeout_seconds"`
}

type PolicyConfig struct {
	FailureThreshold          uint32 `json:"failure_threshold"`
	ActionBudget              uint32 `json:"action_budget"`
	BaseBackoffSeconds        uint32 `json:"base_backoff_seconds"`
	MaxBackoffSeconds         uint32 `json:"max_backoff_seconds"`
	VerificationWindowSeconds uint32 `json:"verification_window_seconds"`
	CooldownSeconds           uint32 `json:"cooldown_seconds"`
	WakeSettleSeconds         uint32 `json:"wake_settle_seconds"`
	ConnectingGraceSeconds    uint32 `json:"connecting_grace_seconds"`
	OTPPeriodSeconds          uint32 `json:"otp_period_seconds"`
	OTPMinValidSeconds        uint32 `json:"otp_min_valid_seconds"`
}

type RuntimeConfig struct {
	Interval      time.Duration
	ExpectedUID   int
	ProfileID     string
	PritunlCLI    string
	OuterEndpoint observe.Endpoint
	Policy        pritunlplan.Policy
}

var (
	ErrInvalidConfig = errors.New("invalid user observe configuration")
	profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
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
		interval < 10*time.Second ||
		interval > 5*time.Minute ||
		config.ExpectedUID <= 0 ||
		!profileIDPattern.MatchString(config.ProfileID) ||
		!filepath.IsAbs(config.PritunlCLI) ||
		filepath.Base(config.PritunlCLI) != "pritunl-client" ||
		filepath.Clean(config.PritunlCLI) != config.PritunlCLI {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	if config.Policy.FailureThreshold == 0 ||
		config.Policy.FailureThreshold > 60 ||
		config.Policy.ActionBudget == 0 ||
		config.Policy.ActionBudget > 20 ||
		config.Policy.BaseBackoffSeconds == 0 ||
		config.Policy.BaseBackoffSeconds > 3600 ||
		config.Policy.MaxBackoffSeconds < config.Policy.BaseBackoffSeconds ||
		config.Policy.MaxBackoffSeconds > 86400 ||
		config.Policy.VerificationWindowSeconds > 3600 ||
		config.Policy.CooldownSeconds == 0 ||
		config.Policy.CooldownSeconds > 604800 ||
		config.Policy.WakeSettleSeconds > 600 ||
		config.Policy.ConnectingGraceSeconds == 0 ||
		config.Policy.ConnectingGraceSeconds > 3600 ||
		config.Policy.OTPPeriodSeconds < 15 ||
		config.Policy.OTPPeriodSeconds > 120 ||
		config.Policy.OTPMinValidSeconds == 0 ||
		config.Policy.OTPMinValidSeconds > config.Policy.OTPPeriodSeconds {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	address, err := netip.ParseAddrPort(config.OuterEndpoint.Address)
	if err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	endpoint := observe.Endpoint{
		Name:        "outer-ready",
		Transport:   config.OuterEndpoint.Transport,
		Certificate: config.OuterEndpoint.Certificate,
		Address:     address,
		ServerName:  config.OuterEndpoint.ServerName,
		Timeout:     time.Duration(config.OuterEndpoint.TimeoutSeconds) * time.Second,
	}
	if endpoint.Transport != observe.TransportDirectTLS ||
		endpoint.Certificate != observe.CertificateHandshakeOnly ||
		!endpoint.Address.Addr().Is4() ||
		endpoint.ProxyAddress.IsValid() ||
		endpoint.Validate() != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}

	policy := pritunlplan.Policy{
		Recovery: control.Policy{
			FailureThreshold:   config.Policy.FailureThreshold,
			ActionBudget:       config.Policy.ActionBudget,
			BaseBackoff:        control.Tick(config.Policy.BaseBackoffSeconds),
			MaxBackoff:         control.Tick(config.Policy.MaxBackoffSeconds),
			VerificationWindow: control.Tick(config.Policy.VerificationWindowSeconds),
			Cooldown:           control.Tick(config.Policy.CooldownSeconds),
		},
		WakeSettle:      control.Tick(config.Policy.WakeSettleSeconds),
		ConnectingGrace: control.Tick(config.Policy.ConnectingGraceSeconds),
		OTPPeriod:       config.Policy.OTPPeriodSeconds,
		OTPMinValid:     config.Policy.OTPMinValidSeconds,
	}
	if _, err := pritunlplan.NewPlanner(
		policy,
		control.NewSnapshot(control.StateHealthy),
	); err != nil {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	return RuntimeConfig{
		Interval:      interval,
		ExpectedUID:   config.ExpectedUID,
		ProfileID:     config.ProfileID,
		PritunlCLI:    config.PritunlCLI,
		OuterEndpoint: endpoint,
		Policy:        policy,
	}, nil
}
