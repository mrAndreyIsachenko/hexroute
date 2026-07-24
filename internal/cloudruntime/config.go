package cloudruntime

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress = ":8080"
	defaultWorkerName    = "primary"

	maxDatabaseURLBytes = 4096
	maxBootstrapBytes   = 4096
)

type Environment func(string) (string, bool)

type APIConfig struct {
	ListenAddress        string
	PublicOrigin         string
	ExpectedHost         string
	RelyingPartyID       string
	BootstrapSecret      string
	IngestDatabaseURL    string
	DashboardDatabaseURL string
	AuthDatabaseURL      string
	WorkerName           string
	IngestTolerance      time.Duration
	WorkerStaleAfter     time.Duration
	FutureTolerance      time.Duration
	ShutdownTimeout      time.Duration
}

var ErrInvalidCloudConfig = errors.New("invalid cloud runtime configuration")

func LoadAPIConfig(environment Environment) (APIConfig, error) {
	if environment == nil {
		return APIConfig{}, ErrInvalidCloudConfig
	}
	listenAddress := environmentValue(environment, "HEXROUTE_HTTP_ADDR")
	if listenAddress == "" {
		port := environmentValue(environment, "PORT")
		if port != "" {
			listenAddress = ":" + port
		} else {
			listenAddress = defaultListenAddress
		}
	}
	origin := environmentValue(environment, "HEXROUTE_PUBLIC_ORIGIN")
	relyingPartyID := environmentValue(environment, "HEXROUTE_WEBAUTHN_RP_ID")
	parsedOrigin, err := validateOrigin(origin, relyingPartyID)
	if err != nil {
		return APIConfig{}, ErrInvalidCloudConfig
	}
	config := APIConfig{
		ListenAddress:     listenAddress,
		PublicOrigin:      origin,
		ExpectedHost:      parsedOrigin.Host,
		RelyingPartyID:    relyingPartyID,
		BootstrapSecret:   environmentValue(environment, "HEXROUTE_BOOTSTRAP_SECRET"),
		IngestDatabaseURL: environmentValue(environment, "HEXROUTE_INGEST_DATABASE_URL"),
		DashboardDatabaseURL: environmentValue(
			environment,
			"HEXROUTE_DASHBOARD_DATABASE_URL",
		),
		AuthDatabaseURL:  environmentValue(environment, "HEXROUTE_AUTH_DATABASE_URL"),
		WorkerName:       environmentValue(environment, "HEXROUTE_WORKER_NAME"),
		IngestTolerance:  5 * time.Minute,
		WorkerStaleAfter: 2 * time.Minute,
		FutureTolerance:  15 * time.Second,
		ShutdownTimeout:  10 * time.Second,
	}
	if config.WorkerName == "" {
		config.WorkerName = defaultWorkerName
	}
	if err := config.Validate(); err != nil {
		return APIConfig{}, err
	}
	return config, nil
}

func (config APIConfig) Validate() error {
	if !validListenAddress(config.ListenAddress) ||
		len(config.BootstrapSecret) < 32 ||
		len(config.BootstrapSecret) > maxBootstrapBytes ||
		strings.TrimSpace(config.BootstrapSecret) != config.BootstrapSecret ||
		strings.ContainsAny(config.BootstrapSecret, "\r\n\x00") ||
		!validWorkerName(config.WorkerName) ||
		config.IngestTolerance <= 0 ||
		config.IngestTolerance > time.Hour ||
		config.WorkerStaleAfter <= 0 ||
		config.WorkerStaleAfter > time.Hour ||
		config.FutureTolerance < 0 ||
		config.FutureTolerance > time.Minute ||
		config.ShutdownTimeout < time.Second ||
		config.ShutdownTimeout > time.Minute {
		return ErrInvalidCloudConfig
	}
	origin, err := validateOrigin(config.PublicOrigin, config.RelyingPartyID)
	if err != nil || !strings.EqualFold(config.ExpectedHost, origin.Host) {
		return ErrInvalidCloudConfig
	}
	identities := make(map[string]struct{}, 3)
	for _, value := range []string{
		config.IngestDatabaseURL,
		config.DashboardDatabaseURL,
		config.AuthDatabaseURL,
	} {
		identity, err := databaseIdentity(value)
		if err != nil {
			return ErrInvalidCloudConfig
		}
		if _, duplicate := identities[identity]; duplicate {
			return ErrInvalidCloudConfig
		}
		identities[identity] = struct{}{}
	}
	return nil
}

func environmentValue(environment Environment, name string) string {
	value, ok := environment(name)
	if !ok {
		return ""
	}
	return value
}

func validateOrigin(value, relyingPartyID string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Hostname() != relyingPartyID ||
		net.ParseIP(relyingPartyID) != nil ||
		!validDNSName(relyingPartyID) {
		return nil, ErrInvalidCloudConfig
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return nil, ErrInvalidCloudConfig
	}
	return parsed, nil
}

func databaseIdentity(value string) (string, error) {
	if value == "" || len(value) > maxDatabaseURLBytes ||
		strings.ContainsAny(value, "\r\n\x00") {
		return "", ErrInvalidCloudConfig
	}
	parsed, err := url.Parse(value)
	if err != nil ||
		(parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.Host == "" ||
		parsed.User == nil ||
		parsed.User.Username() == "" ||
		parsed.Fragment != "" {
		return "", ErrInvalidCloudConfig
	}
	return parsed.User.Username(), nil
}

func validListenAddress(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return false
	}
	return host == "" ||
		host == "localhost" ||
		net.ParseIP(host) != nil
}

func validDNSName(value string) bool {
	if len(value) == 0 || len(value) > 253 || strings.HasSuffix(value, ".") {
		return false
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') ||
				character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func validWorkerName(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}
