package ingressobserver

import "testing"

func TestLoadConfigRequiresLoopbackAndPublicLiteralDependencies(t *testing.T) {
	valid := map[string]string{
		envListenAddr:       "127.0.0.1:9080",
		envXRayEndpoint:     "127.0.0.1:443",
		envOutboundEndpoint: "1.1.1.1:443",
		envNodeID:           "11111111-1111-4111-8111-111111111111",
		envGeneration:       "provider-b-generation-1",
		envKeyFile:          "/etc/hexroute/runtime/observer-key.json",
	}
	lookup := func(values map[string]string) LookupEnv {
		return func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		}
	}
	if _, err := LoadConfig(lookup(valid)); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "public listener", field: envListenAddr, value: "0.0.0.0:9080"},
		{name: "hostname listener", field: envListenAddr, value: "localhost:9080"},
		{name: "IPv6 listener", field: envListenAddr, value: "[::1]:9080"},
		{name: "public xray", field: envXRayEndpoint, value: "198.51.100.2:443"},
		{name: "private dependency", field: envOutboundEndpoint, value: "10.0.0.1:443"},
		{name: "hostname dependency", field: envOutboundEndpoint, value: "example.com:443"},
		{name: "floating generation", field: envGeneration, value: "generation latest"},
		{name: "relative key", field: envKeyFile, value: "observer-key.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			values[test.field] = test.value
			if _, err := LoadConfig(lookup(values)); err == nil {
				t.Fatal("LoadConfig() accepted unsafe configuration")
			}
		})
	}
}
