package rootdaemon

import (
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelexec"
)

// The identity has to be one the schema accepts, which is the whole reason the
// reason is spelled with hyphens. Encoding it is the only proof of that; the
// spelling test next door proves the mapping and not that the result is
// writable.
func TestEveryHandbackReasonMakesAWritableIncident(t *testing.T) {
	for _, reason := range tunnelexec.Handbacks() {
		incident, err := handbackIncident(
			event.TunnelHandback{Reason: string(reason), Given: true}, 41)
		if err != nil {
			t.Fatalf("%q: %v", reason, err)
		}
		if _, err := event.Encode(event.SchemaIncident, incident); err != nil {
			t.Fatalf("%q produced an incident the schema refuses: %v", reason, err)
		}
		if incident.Status != event.IncidentOpened ||
			incident.Severity != event.SeverityCritical ||
			incident.Category != event.IncidentAvailability ||
			incident.Component != control.ComponentRuntime {
			t.Fatalf("%q produced %+v", reason, incident)
		}
		if incident.Generation != 41 {
			t.Fatalf("%q carried generation %d, want 41", reason, incident.Generation)
		}
		// The reason is in the identity because it is the first thing whoever
		// reads the alert needs, and the three reasons are three different
		// things to do about it.
		spelled, _ := reason.Reference()
		if !strings.HasSuffix(incident.IncidentID, ":"+spelled) {
			t.Fatalf("%q is identified as %q and does not carry its reason",
				reason, incident.IncidentID)
		}
	}
}

// The end is writable too, and says the condition resolved. A condition that
// can only open is one nobody keeps reading about.
func TestTheEndingIsWritableAndResolves(t *testing.T) {
	ended := handbackEnded(7)
	if _, err := event.Encode(event.SchemaIncident, ended); err != nil {
		t.Fatalf("the ending is an incident the schema refuses: %v", err)
	}
	if ended.Status != event.IncidentResolved {
		t.Fatalf("the ending has status %q, want %q",
			ended.Status, event.IncidentResolved)
	}
	// Both records are about one condition, and what correlates them where
	// they are read is the category and the component rather than this.
	opened, err := handbackIncident(
		event.TunnelHandback{Reason: string(tunnelexec.HandbackRate)}, 7)
	if err != nil {
		t.Fatalf("opened: %v", err)
	}
	if ended.Category != opened.Category || ended.Component != opened.Component {
		t.Fatalf("the ending is correlated differently from the opening: %+v vs %+v",
			ended, opened)
	}
}

// A reason this runtime cannot name is refused here, so that nothing writes an
// incident with an identity the schema would reject. What the caller does about
// it is the cycle's business, and it does not end over it.
func TestAnUnnameableReasonIsRefusedRatherThanWrittenAround(t *testing.T) {
	for _, reason := range []string{"", "carrier_changed", "rate_bound extra"} {
		if _, err := handbackIncident(
			event.TunnelHandback{Reason: reason}, 1); err == nil {
			t.Fatalf("reason %q produced an incident", reason)
		}
	}
}

// The cause exists so a cycle can say it could not name a condition without
// ending. Ending there is the failure of 2026-10-05 in a new place.
func TestTheUnnameableCauseIsOneTheCycleMayKeep(t *testing.T) {
	if !control.ReasonIncidentUnnameable.Valid() {
		t.Fatal("the cause a cycle keeps for an unnameable incident is not valid")
	}
	summary := &Summary{}
	summary.fail(control.ReasonIncidentUnnameable)
	if summary.Failures != 1 || summary.Cause != control.ReasonIncidentUnnameable {
		t.Fatalf("the summary kept %d failures and cause %q",
			summary.Failures, summary.Cause)
	}
}
