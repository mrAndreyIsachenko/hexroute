package cloudruntime

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
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
	ProviderHost         string
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

type WorkerConfig struct {
	MaintenanceDatabaseURL string
	TelegramBotToken       string
	TelegramChatID         string
	WorkerName             string
	Location               *time.Location
	HeartbeatInterval      time.Duration
	ReconcileInterval      time.Duration
	AlertQueueInterval     time.Duration
	DeliveryInterval       time.Duration
	RetentionInterval      time.Duration
	JobTimeout             time.Duration
	ShutdownTimeout        time.Duration
}

type MigrationConfig struct {
	MigratorDatabaseURL  string
	BootstrapUsername    string
	BootstrapDisplayName string
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
	providerHost, err := validateProviderOrigin(
		environmentValue(environment, "HEXROUTE_PROVIDER_ORIGIN"),
	)
	if err != nil {
		return APIConfig{}, ErrInvalidCloudConfig
	}
	config := APIConfig{
		ListenAddress:     listenAddress,
		PublicOrigin:      origin,
		ExpectedHost:      parsedOrigin.Host,
		ProviderHost:      providerHost,
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

func LoadWorkerConfig(environment Environment) (WorkerConfig, error) {
	if environment == nil {
		return WorkerConfig{}, ErrInvalidCloudConfig
	}
	locationName := environmentValue(environment, "HEXROUTE_TIMEZONE")
	if locationName == "" {
		locationName = "Europe/Moscow"
	}
	location, err := time.LoadLocation(locationName)
	if err != nil {
		return WorkerConfig{}, ErrInvalidCloudConfig
	}
	config := WorkerConfig{
		MaintenanceDatabaseURL: environmentValue(
			environment,
			"HEXROUTE_MAINTENANCE_DATABASE_URL",
		),
		TelegramBotToken:   environmentValue(environment, "HEXROUTE_TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     environmentValue(environment, "HEXROUTE_TELEGRAM_CHAT_ID"),
		WorkerName:         environmentValue(environment, "HEXROUTE_WORKER_NAME"),
		Location:           location,
		HeartbeatInterval:  30 * time.Second,
		ReconcileInterval:  30 * time.Second,
		AlertQueueInterval: 10 * time.Second,
		DeliveryInterval:   10 * time.Second,
		RetentionInterval:  time.Hour,
		JobTimeout:         20 * time.Second,
		ShutdownTimeout:    10 * time.Second,
	}
	if config.WorkerName == "" {
		config.WorkerName = defaultWorkerName
	}
	durationFields := []struct {
		name   string
		target *time.Duration
	}{
		{"HEXROUTE_HEARTBEAT_INTERVAL", &config.HeartbeatInterval},
		{"HEXROUTE_RECONCILE_INTERVAL", &config.ReconcileInterval},
		{"HEXROUTE_ALERT_QUEUE_INTERVAL", &config.AlertQueueInterval},
		{"HEXROUTE_DELIVERY_INTERVAL", &config.DeliveryInterval},
		{"HEXROUTE_RETENTION_INTERVAL", &config.RetentionInterval},
		{"HEXROUTE_JOB_TIMEOUT", &config.JobTimeout},
		{"HEXROUTE_SHUTDOWN_TIMEOUT", &config.ShutdownTimeout},
	}
	for _, field := range durationFields {
		value := environmentValue(environment, field.name)
		if value == "" {
			continue
		}
		duration, parseErr := time.ParseDuration(value)
		if parseErr != nil || strings.TrimSpace(value) != value {
			return WorkerConfig{}, ErrInvalidCloudConfig
		}
		*field.target = duration
	}
	if err := config.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return config, nil
}

func LoadMigrationConfig(environment Environment) (MigrationConfig, error) {
	if environment == nil {
		return MigrationConfig{}, ErrInvalidCloudConfig
	}
	config := MigrationConfig{
		MigratorDatabaseURL: environmentValue(
			environment,
			"HEXROUTE_MIGRATOR_DATABASE_URL",
		),
		BootstrapUsername: environmentValue(
			environment,
			"HEXROUTE_BOOTSTRAP_USERNAME",
		),
		BootstrapDisplayName: environmentValue(
			environment,
			"HEXROUTE_BOOTSTRAP_DISPLAY_NAME",
		),
	}
	if config.BootstrapUsername == "" {
		config.BootstrapUsername = "operator"
	}
	if config.BootstrapDisplayName == "" {
		config.BootstrapDisplayName = "Operator"
	}
	if err := config.Validate(); err != nil {
		return MigrationConfig{}, err
	}
	return config, nil
}

func (config MigrationConfig) Validate() error {
	if !validWorkerName(config.BootstrapUsername) ||
		len(config.BootstrapDisplayName) == 0 ||
		len(config.BootstrapDisplayName) > 128 ||
		strings.TrimSpace(config.BootstrapDisplayName) != config.BootstrapDisplayName ||
		strings.ContainsAny(config.BootstrapDisplayName, "\r\n\x00") {
		return ErrInvalidCloudConfig
	}
	if _, err := databaseIdentity(config.MigratorDatabaseURL); err != nil {
		return ErrInvalidCloudConfig
	}
	return nil
}

func (config WorkerConfig) Validate() error {
	if !validWorkerName(config.WorkerName) ||
		config.Location == nil ||
		!validSecretValue(config.TelegramBotToken, 24, 256) ||
		!validSecretValue(config.TelegramChatID, 1, 128) ||
		!durationBetween(config.HeartbeatInterval, 5*time.Second, 5*time.Minute) ||
		!durationBetween(config.ReconcileInterval, 5*time.Second, 5*time.Minute) ||
		!durationBetween(config.AlertQueueInterval, 5*time.Second, 5*time.Minute) ||
		!durationBetween(config.DeliveryInterval, 5*time.Second, 5*time.Minute) ||
		!durationBetween(config.RetentionInterval, time.Minute, 24*time.Hour) ||
		!durationBetween(config.JobTimeout, time.Second, 2*time.Minute) ||
		!durationBetween(config.ShutdownTimeout, time.Second, time.Minute) {
		return ErrInvalidCloudConfig
	}
	if _, err := databaseIdentity(config.MaintenanceDatabaseURL); err != nil {
		return ErrInvalidCloudConfig
	}
	return nil
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
	if err != nil || !strings.EqualFold(config.ExpectedHost, origin.Host) ||
		!validProviderHost(config.ProviderHost) {
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

func validSecretValue(value string, minimum, maximum int) bool {
	return len(value) >= minimum &&
		len(value) <= maximum &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\r\n\x00")
}

func durationBetween(value, minimum, maximum time.Duration) bool {
	return value >= minimum && value <= maximum
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

func validateProviderOrigin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Port() != "" && parsed.Port() != "443") ||
		!strings.HasSuffix(parsed.Hostname(), ".ondigitalocean.app") ||
		!validDNSName(parsed.Hostname()) {
		return "", ErrInvalidCloudConfig
	}
	return parsed.Host, nil
}

func validProviderHost(value string) bool {
	if value == "" {
		return true
	}
	validated, err := validateProviderOrigin("https://" + value)
	return err == nil && strings.EqualFold(validated, value)
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
