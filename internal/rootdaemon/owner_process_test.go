package rootdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
)

// The cycle asks for the process running the owner's configuration.
//
// It used to ask for the first process named sing-box, and an ingress probe
// runs sing-box. Which configuration is the owner's follows the claim.
func TestTheCycleAsksForTheOwnersTunnel(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	var asked string
	processes.asked = &asked
	cycle, err := NewCycle(config, network, processes, endpoints,
		WithOwnerConfig(func() (string, error) { return tunnelclaim.HexrouteContent, nil }))
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	cycle.Observe(context.Background())
	if asked != tunnelclaim.HexrouteContent {
		t.Fatalf("the cycle asked for %q, want the claimed owner's %q", asked, tunnelclaim.HexrouteContent)
	}
}

// Without a claim reader the previous owner is taken to hold the tunnel.
func TestWithoutAClaimReaderThePreviousOwnerHoldsIt(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	var asked string
	processes.asked = &asked
	cycle, _ := NewCycle(config, network, processes, endpoints)
	cycle.Observe(context.Background())
	if asked != tunnelclaim.PreviousOwnerConfig {
		t.Fatalf("the cycle asked for %q, want the previous owner's", asked)
	}
}

// A claim that cannot be read is a failed observation, not a lost tunnel.
//
// Reported as absent, it would decide process_gone from a file nobody could
// read — and the claim itself refuses to treat unreadable as absent, for the
// same reason.
func TestAnUnreadableClaimIsAFailedObservation(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	var asked string
	processes.asked = &asked
	cycle, _ := NewCycle(config, network, processes, endpoints,
		WithOwnerConfig(func() (string, error) { return "", errors.New("claim unreadable") }))
	summary := cycle.Observe(context.Background())
	if summary.Observed.ProcessError == nil {
		t.Fatal("an unreadable claim left no error on the process observation")
	}
	if asked != "" {
		t.Fatalf("the process was looked for under %q without knowing the owner", asked)
	}
	if summary.Failures == 0 {
		t.Fatal("an unreadable claim was not counted as a failure")
	}
}
