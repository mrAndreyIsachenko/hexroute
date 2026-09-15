package rootdaemon

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func configWithSupervision(wakeThresholdSeconds string) string {
	return strings.Replace(validConfig,
		`"upstream_probe_address": "203.0.113.53",`,
		`"upstream_probe_address": "203.0.113.53",
  "tunnel_supervision": {
    "wake_threshold_seconds": `+wakeThresholdSeconds+`,
    "payload_failures": 2,
    "link_failures": 2,
    "payload": {"name": "payload", "url": "http://198.51.100.1/", "timeout_seconds": 4}
  },`, 1)
}

// The rule refuses a threshold within one interval, so it needs the interval the
// configuration runs at.
//
// Without it the rule has no interval to check against, and refuses every decision — a
// runtime that quietly decides nothing looks, in its record, like a machine on
// which nothing happened.
func TestTheRuleTakesTheConfiguredInterval(t *testing.T) {
	config, err := DecodeConfig(strings.NewReader(configWithSupervision("180")))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if config.TunnelSupervision == nil {
		t.Fatal("the supervision block was not decoded")
	}
	if config.TunnelSupervision.Policy.Interval != time.Minute {
		t.Fatalf("the rule's interval is %s, want the configured minute", config.TunnelSupervision.Policy.Interval)
	}
	if config.TunnelSupervision.Policy.WakeThreshold != 3*time.Minute {
		t.Fatalf("the wake threshold is %s", config.TunnelSupervision.Policy.WakeThreshold)
	}
}

// A threshold at or below the interval is refused, because every cycle is at
// least an interval after the last and would name a wake gap.
func TestAThresholdWithinTheIntervalIsRefused(t *testing.T) {
	for _, threshold := range []string{"60", "30"} {
		if _, err := DecodeConfig(strings.NewReader(configWithSupervision(threshold))); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("wake threshold %ss with a 60s interval: err = %v, want ErrInvalidConfig", threshold, err)
		}
	}
}
