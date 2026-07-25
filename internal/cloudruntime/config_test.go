package cloudruntime

import (
	"errors"
	"testing"
	"time"
)

func TestLoadAPIConfigRequiresDistinctDatabaseIdentitiesAndExactOrigin(t *testing.T) {
	values := validAPIEnvironment()
	config, err := LoadAPIConfig(mapEnvironment(values))
	if err != nil {
		t.Fatalf("LoadAPIConfig() error = %v", err)
	}
	if config.ListenAddress != ":8080" ||
		config.ExpectedHost != "status.example" ||
		config.ProviderHost != "hexroute-example.ondigitalocean.app" ||
		config.WorkerName != "primary" {
		t.Fatalf("LoadAPIConfig() = %+v", config)
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{
			name: "http origin",
			mutate: func(values map[string]string) {
				values["HEXROUTE_PUBLIC_ORIGIN"] = "http://status.example"
			},
		},
		{
			name: "rp mismatch",
			mutate: func(values map[string]string) {
				values["HEXROUTE_WEBAUTHN_RP_ID"] = "other.example"
			},
		},
		{
			name: "non-DigitalOcean provider origin",
			mutate: func(values map[string]string) {
				values["HEXROUTE_PROVIDER_ORIGIN"] = "https://provider.example"
			},
		},
		{
			name: "provider origin path",
			mutate: func(values map[string]string) {
				values["HEXROUTE_PROVIDER_ORIGIN"] =
					"https://hexroute-example.ondigitalocean.app/path"
			},
		},
		{
			name: "shared login",
			mutate: func(values map[string]string) {
				values["HEXROUTE_AUTH_DATABASE_URL"] =
					"postgres://dashboard@db.example/hexroute"
			},
		},
		{
			name: "short bootstrap",
			mutate: func(values map[string]string) {
				values["HEXROUTE_BOOTSTRAP_SECRET"] = "short"
			},
		},
		{
			name: "invalid port",
			mutate: func(values map[string]string) {
				values["PORT"] = "70000"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := validAPIEnvironment()
			test.mutate(invalid)
			if _, err := LoadAPIConfig(mapEnvironment(invalid)); !errors.Is(
				err,
				ErrInvalidCloudConfig,
			) {
				t.Fatalf("LoadAPIConfig() error = %v", err)
			}
		})
	}
}

func TestLoadAPIConfigDoesNotReturnSecretValuesInErrors(t *testing.T) {
	values := validAPIEnvironment()
	secret := "0123456789abcdef0123456789abcdef"
	values["HEXROUTE_BOOTSTRAP_SECRET"] = secret + "\n"
	_, err := LoadAPIConfig(mapEnvironment(values))
	if !errors.Is(err, ErrInvalidCloudConfig) || err.Error() != ErrInvalidCloudConfig.Error() {
		t.Fatalf("LoadAPIConfig() error = %v", err)
	}
}

func TestLoadWorkerConfigUsesBoundedDefaultsAndRejectsSecrets(t *testing.T) {
	values := validWorkerEnvironment()
	config, err := LoadWorkerConfig(mapEnvironment(values))
	if err != nil {
		t.Fatalf("LoadWorkerConfig() error = %v", err)
	}
	if config.WorkerName != "primary" ||
		config.Location.String() != "Europe/Moscow" ||
		config.HeartbeatInterval != 30*time.Second ||
		config.RetentionInterval != time.Hour {
		t.Fatalf("LoadWorkerConfig() = %+v", config)
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{
			name: "missing database",
			mutate: func(values map[string]string) {
				delete(values, "HEXROUTE_MAINTENANCE_DATABASE_URL")
			},
		},
		{
			name: "token newline",
			mutate: func(values map[string]string) {
				values["HEXROUTE_TELEGRAM_BOT_TOKEN"] += "\n"
			},
		},
		{
			name: "short heartbeat",
			mutate: func(values map[string]string) {
				values["HEXROUTE_HEARTBEAT_INTERVAL"] = "1s"
			},
		},
		{
			name: "long retention",
			mutate: func(values map[string]string) {
				values["HEXROUTE_RETENTION_INTERVAL"] = "25h"
			},
		},
		{
			name: "unknown timezone",
			mutate: func(values map[string]string) {
				values["HEXROUTE_TIMEZONE"] = "Unknown/Nowhere"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := validWorkerEnvironment()
			test.mutate(invalid)
			if _, err := LoadWorkerConfig(mapEnvironment(invalid)); !errors.Is(
				err,
				ErrInvalidCloudConfig,
			) {
				t.Fatalf("LoadWorkerConfig() error = %v", err)
			}
		})
	}
}

func validAPIEnvironment() map[string]string {
	return map[string]string{
		"HEXROUTE_PUBLIC_ORIGIN":          "https://status.example",
		"HEXROUTE_PROVIDER_ORIGIN":        "https://hexroute-example.ondigitalocean.app",
		"HEXROUTE_WEBAUTHN_RP_ID":         "status.example",
		"HEXROUTE_BOOTSTRAP_SECRET":       "0123456789abcdef0123456789abcdef",
		"HEXROUTE_INGEST_DATABASE_URL":    "postgres://ingest@db.example/hexroute",
		"HEXROUTE_DASHBOARD_DATABASE_URL": "postgres://dashboard@db.example/hexroute",
		"HEXROUTE_AUTH_DATABASE_URL":      "postgres://auth@db.example/hexroute",
	}
}

func validWorkerEnvironment() map[string]string {
	return map[string]string{
		"HEXROUTE_MAINTENANCE_DATABASE_URL": "postgres://maintenance@db.example/hexroute",
		"HEXROUTE_TELEGRAM_BOT_TOKEN":       "12345678:abcdefghijklmnop",
		"HEXROUTE_TELEGRAM_CHAT_ID":         "-123456789",
	}
}

func mapEnvironment(values map[string]string) Environment {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
