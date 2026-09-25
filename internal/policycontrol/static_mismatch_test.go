package policycontrol

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
)

// supersededStore is a store whose active generation was compiled against a
// static authority that has since moved: recovering it fails, and its lineage
// still says which generation it is.
func supersededStore(t *testing.T, domain policy.Domain) (*recordingCandidateStore, RuntimeConfig) {
	t.Helper()
	publicKey := ed25519.NewKeyFromSeed(
		bytes.Repeat([]byte{31}, ed25519.SeedSize),
	).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(domain, publicKey).Runtime(domain)
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingCandidateStore{
		domain:     domain,
		recoverErr: policy.ErrRestartRequired,
		pendingErr: policystore.ErrRecordNotFound,
		lineage: policystore.Lineage{
			Domain:           domain,
			Generation:       policystore.Generation{Bundle: 4, Policy: 3},
			ManifestSHA256:   policy.SHA256Hex([]byte("synthetic-manifest-4")),
			PayloadSHA256:    policy.SHA256Hex([]byte("synthetic-payload-4")),
			PolicySchema:     1,
			StaticSHA256:     policy.SHA256Hex([]byte("the envelope that came before")),
			NotBefore:        "2030-01-01T00:00:00Z",
			ExpiresAt:        "2030-02-01T00:00:00Z",
			ConfirmedAt:      "2030-01-01T00:00:01Z",
			StaticSuperseded: true,
		},
	}
	return store, runtime
}

func supersededHandler(t *testing.T, store *recordingCandidateStore, runtime RuntimeConfig) *Handler {
	t.Helper()
	now := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC)
	handler, err := NewHandler(store, runtime, func() time.Time { return now })
	if err != nil {
		t.Fatalf("a runtime whose static authority moved refused to start: %v", err)
	}
	if handler == nil {
		t.Fatal("no handler and no error")
	}
	return handler
}

// A runtime whose static authority moved ahead of the generation in force starts
// and says so.
//
// Refusing took more than authority with it: measured 2026-09-25, both daemons
// stopped — observation, operator socket and remedy together — and the only act
// that resolves the mismatch runs through the socket that was gone.
func TestARuntimeWhoseStaticAuthorityMovedStartsAndSaysSo(t *testing.T) {
	store, runtime := supersededStore(t, policy.DomainRoot)
	handler := supersededHandler(t, store, runtime)

	status := handler.status
	if status.State != policy.PolicyRestartRequired {
		t.Fatalf("state = %q, want %q", status.State, policy.PolicyRestartRequired)
	}
	if status.Reason != policy.ReasonStaticMismatch {
		t.Fatalf("reason = %q, want %q", status.Reason, policy.ReasonStaticMismatch)
	}
	// It names the generation it cannot run, out of the lineage.
	if status.BundleGeneration != 4 || status.PolicyGeneration != 3 {
		t.Fatalf("status names generation %d/%d", status.BundleGeneration, status.PolicyGeneration)
	}
	if status.ManifestSHA256 != policy.SHA256Hex([]byte("synthetic-manifest-4")) {
		t.Fatalf("status names manifest %q", status.ManifestSHA256)
	}
	if status.ActivatedAt != "" {
		t.Fatalf("a generation that cannot run reported an activation at %q", status.ActivatedAt)
	}
	if store.lineageCalls == 0 {
		t.Fatal("the generation was named without reading the lineage")
	}
}

// It authorizes nothing while it is in that state.
func TestARuntimeWithASupersededStaticAuthorityAuthorizesNothing(t *testing.T) {
	store, runtime := supersededStore(t, policy.DomainRoot)
	handler := supersededHandler(t, store, runtime)
	if handler.MutationAllowed() {
		t.Fatal("a runtime that cannot run its own generation allowed a mutation")
	}
	digest := policy.SHA256Hex([]byte(tunnelTestPlan))
	for name, answer := range map[string]policy.ActionAuthorizationDecision{
		"tunnel":          handler.AuthorizeTunnelOwnership("tunnel", 7, digest),
		"pritunl":         handler.AuthorizePritunlRecovery(policy.DomainRoot, "pritunl", 7, digest),
		"operator resume": handler.EvaluateOperatorResume(policy.DomainRoot, "tunnel", 7, digest),
	} {
		if answer.Allowed {
			t.Fatalf("%s was authorized in a static mismatch: %+v", name, answer)
		}
	}
}

// The user domain answers the same way: both daemons were stopped by this, and
// both have to come up.
func TestTheUserDomainStartsTheSameWay(t *testing.T) {
	store, runtime := supersededStore(t, policy.DomainUser)
	handler := supersededHandler(t, store, runtime)
	if status := handler.status; status.State != policy.PolicyRestartRequired ||
		status.Domain != policy.DomainUser {
		t.Fatalf("status = %+v", status)
	}
}

// A lineage that cannot be read names nothing, and the runtime keeps the answer
// it had: refusing rather than reporting a generation nobody verified.
func TestALineageThatCannotBeReadIsNotAStaticMismatch(t *testing.T) {
	store, runtime := supersededStore(t, policy.DomainRoot)
	store.lineageErr = policystore.ErrInvalidRecord
	now := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC)
	if _, err := NewHandler(store, runtime, func() time.Time { return now }); err == nil {
		t.Fatal("a store whose lineage could not be read reported a static mismatch")
	}
}

// A lineage that says the static authority did not move is not this state
// either. The two readings have to agree before a runtime reports one of them.
func TestALineageThatDoesNotSayItMovedIsNotAStaticMismatch(t *testing.T) {
	store, runtime := supersededStore(t, policy.DomainRoot)
	store.lineage.StaticSuperseded = false
	now := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC)
	if _, err := NewHandler(store, runtime, func() time.Time { return now }); err == nil {
		t.Fatal("a lineage that did not say the authority moved was read as a static mismatch")
	}
}

// A lineage that cannot make a well-formed status names nothing. Reporting a
// malformed status would put a number nobody can read where a generation
// belongs.
func TestALineageThatCannotMakeAStatusNamesNothing(t *testing.T) {
	now := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC)
	for name, damage := range map[string]func(*policystore.Lineage){
		"no generation": func(lineage *policystore.Lineage) {
			lineage.Generation = policystore.Generation{}
		},
		"no manifest": func(lineage *policystore.Lineage) {
			lineage.ManifestSHA256 = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, runtime := supersededStore(t, policy.DomainRoot)
			damage(&store.lineage)
			if _, err := NewHandler(store, runtime, func() time.Time { return now }); err == nil {
				t.Fatalf("a lineage with %s reported a static mismatch", name)
			}
		})
	}
}

// Every other startup failure keeps the answer it had. Making one of them
// legible must not make the rest so.
func TestTheOtherStartupFailuresKeepTheirAnswers(t *testing.T) {
	now := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC)
	for name, cause := range map[string]error{
		"corruption":        policystore.ErrInvalidRecord,
		"invalid signature": policyapproval.ErrApprovalSignature,
		"clock anomaly":     policystore.ErrActiveClockAnomaly,
		"unreadable store":  policystore.ErrStoreUnavailable,
	} {
		t.Run(name, func(t *testing.T) {
			store, runtime := supersededStore(t, policy.DomainRoot)
			store.recoverErr = cause
			handler, err := NewHandler(store, runtime, func() time.Time { return now })
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}
			status := handler.status
			if status.Reason == policy.ReasonStaticMismatch ||
				status.State == policy.PolicyRestartRequired {
				t.Fatalf("%s was reported as a static mismatch: %+v", name, status)
			}
			if handler.MutationAllowed() {
				t.Fatalf("%s allowed a mutation", name)
			}
		})
	}
	// And an error nobody classified is still a refusal to start.
	store, runtime := supersededStore(t, policy.DomainRoot)
	store.recoverErr = errors.New("something nobody wrote a reason for")
	if _, err := NewHandler(store, runtime, func() time.Time { return now }); err == nil {
		t.Fatal("an unclassified failure started without saying anything")
	}
}
