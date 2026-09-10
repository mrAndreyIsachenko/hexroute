package userdaemon

import (
	"context"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/credentials"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlclient"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue"
)

// recoveryTarget is the one thing this domain may be authorized to recover.
const recoveryTarget = "pritunl"

// recoveryOutcome is what one authorized-or-not act came to. Each value is a
// different thing to do about it, which is why they are not collapsed into a
// success flag.
type recoveryOutcome string

const (
	// recoveryProposed is the pre-cutover behaviour: the plan says act and
	// nothing authorizes it, so it is reported and not done.
	recoveryProposed recoveryOutcome = "proposed"
	// recoveryUnequipped is authorized and unable. It is reported rather than
	// silently downgraded to a proposal, because an authority that cannot be
	// exercised is a fault in the deployment and not a decision.
	recoveryUnequipped recoveryOutcome = "unequipped"
	recoveryDone       recoveryOutcome = "done"
	// recoveryAnswererFailed is the other side not having looked at all: it
	// answered that it failed, or answered something this cannot read. That is
	// not a refusal, and reporting it as one sends the reader to the policy for
	// a decision nobody made. On 2026-09-10 it did exactly that.
	recoveryAnswererFailed recoveryOutcome = "answerer_failed"
	// The refusals root does name. They are separate values because they send
	// the reader somewhere different: to what signed for the act, to the
	// generation in force, to root's own view of the path and the service, and
	// to the request itself. Collapsing them is what left an operator reading
	// the source to find out why a rescue had been refused.
	recoveryRefusedAuthority    recoveryOutcome = "refused_authority"
	recoveryRefusedStale        recoveryOutcome = "refused_stale_generation"
	recoveryRefusedPrecondition recoveryOutcome = "refused_precondition"
	recoveryRefusedRequest      recoveryOutcome = "refused_request"
	recoveryFailed              recoveryOutcome = "failed"
	// Where an attempt stopped. Separate because they send the reader
	// somewhere different: to the Keychain, to the client, and to this code.
	// Until this existed all of them were the one word "failed", and on
	// 2026-09-09 that left two explanations fitting one observation.
	recoveryFailedCredentials recoveryOutcome = "failed_credentials"
	recoveryFailedCode        recoveryOutcome = "failed_code"
	recoveryFailedNotStarted  recoveryOutcome = "failed_not_started"
)

// recovery performs what the planner decided, if anything authorizes it.
//
// It holds the two halves of the act in one place so that the authorization
// question is asked once, in one way: the reconnect it performs itself, and the
// service restart it asks root for. It cannot restart anything itself, and root
// cannot read what it holds.
// reconnector is what an attempt needs of the Pritunl client, which is one
// method.
//
// It is an interface because the step an attempt stops at is decided partly by
// the wall clock: the real client refuses a one-time-code window too short to
// use before it reads any credential. A test driving the concrete client
// through perform therefore passed or failed by the second it ran on, which is
// how it passed on one runner and failed on another. Where Reconnect stops is
// settled in that package against a pinned clock; what this one does with the
// answer is settled here.
type reconnector interface {
	Reconnect(context.Context, credentials.Source) error
}

type recovery struct {
	authorize func(string, uint64, string) policy.ActionAuthorizationDecision
	client    reconnector
	source    credentials.Source
	roundTrip func(context.Context, ipc.Request) (ipc.Response, error)
	requestID func() (string, error)
	// bundleGeneration is the generation of the policy that permits the act.
	//
	// It is what the request carries. The control-state generation it used to
	// carry counts this runtime's own cycles, and root compared it with the
	// count of its own — two numbers that could match only by coincidence.
	bundleGeneration func() uint64
}

// perform decides and acts.
//
// A plan that asks for nothing is not authorized and not reported: asking
// permission to do nothing would put a decision in the record that nobody made.
func (executor *recovery) perform(
	ctx context.Context,
	plan pritunlplan.Plan,
	unreachableAddress string,
) recoveryOutcome {
	if executor == nil || ctx == nil || executor.authorize == nil {
		return recoveryProposed
	}
	switch plan.Action {
	case pritunlplan.ActionReconnect, pritunlplan.ActionRequestRescue:
	default:
		return recoveryProposed
	}
	digest, err := planDigest(plan)
	if err != nil {
		return recoveryFailed
	}
	if decision := executor.authorize(
		recoveryTarget, plan.Snapshot.Generation, digest,
	); !decision.Allowed {
		return recoveryProposed
	}

	switch plan.Action {
	case pritunlplan.ActionReconnect:
		if executor.client == nil || executor.source == nil {
			return recoveryUnequipped
		}
		if err := executor.client.Reconnect(ctx, executor.source); err != nil {
			return reconnectOutcome(err)
		}
		return recoveryDone
	case pritunlplan.ActionRequestRescue:
		return executor.requestRescue(ctx, plan, unreachableAddress)
	}
	return recoveryProposed
}

// requestRescue asks root to restart the service.
//
// The request is typed and carries no credential, and root reaches its own
// conclusion before acting. A refusal is not a failure: root looked and
// disagreed, which is what it is there for.
func (executor *recovery) requestRescue(
	ctx context.Context,
	plan pritunlplan.Plan,
	unreachableAddress string,
) recoveryOutcome {
	if executor.roundTrip == nil || executor.requestID == nil {
		return recoveryUnequipped
	}
	id, err := executor.requestID()
	if err != nil {
		return recoveryFailed
	}
	if executor.bundleGeneration == nil {
		return recoveryUnequipped
	}
	// The evidence, when this cycle saw any: the address the session claims and
	// that no tunnel interface carries. Root looks for it itself.
	request, err := pritunlrescue.NewRequest(
		id, executor.bundleGeneration(), unreachableAddress)
	if err != nil {
		return recoveryFailed
	}
	response, err := executor.roundTrip(ctx, request)
	if err != nil {
		return recoveryFailed
	}
	if !response.OK {
		return refusalOutcome(response.Error)
	}
	return recoveryDone
}

// planDigest binds the authorization to what was decided, so that a lease
// issued for one plan cannot be spent on another.
// refusalOutcome carries the ground root gave for its refusal.
//
// The code is all that crosses the boundary, and it is enough to send the
// reader to the right place. What it cannot separate — which of root's own
// preconditions failed — stays in root's log beside the same refusal.
// reconnectOutcome carries the step an attempt stopped at.
//
// A fault this code has not named stays the general failure. Naming it here
// without naming it there would put a word in the log that nothing produces.
func reconnectOutcome(err error) recoveryOutcome {
	switch {
	case errors.Is(err, pritunlclient.ErrCredentialsUnavailable):
		return recoveryFailedCredentials
	case errors.Is(err, pritunlclient.ErrOneTimeCodeUnavailable):
		return recoveryFailedCode
	case errors.Is(err, pritunlclient.ErrSessionNotStarted):
		return recoveryFailedNotStarted
	default:
		return recoveryFailed
	}
}

func refusalOutcome(code ipc.ErrorCode) recoveryOutcome {
	switch code {
	case ipc.ErrorUnauthorized:
		return recoveryRefusedAuthority
	case ipc.ErrorStaleGeneration:
		return recoveryRefusedStale
	case ipc.ErrorPrecondition:
		return recoveryRefusedPrecondition
	case ipc.ErrorInvalidRequest:
		return recoveryRefusedRequest
	default:
		// Every code left is the other side failing rather than refusing:
		// it said so, or it said something with no ground in it at all.
		return recoveryAnswererFailed
	}
}

func planDigest(plan pritunlplan.Plan) (string, error) {
	digest, _, err := policy.CanonicalSHA256(struct {
		Action     string `json:"action"`
		Reason     string `json:"reason"`
		State      string `json:"state"`
		Generation uint64 `json:"generation"`
		Target     string `json:"target"`
	}{
		Action:     string(plan.Action),
		Reason:     string(plan.Reason),
		State:      string(plan.State),
		Generation: plan.Snapshot.Generation,
		Target:     recoveryTarget,
	})
	if err != nil {
		return "", errors.New("plan digest unavailable")
	}
	return digest, nil
}

// RecoveryConfig is how this daemon would act, if it were authorized to.
//
// It is optional and separate from the authority: without it the daemon cannot
// perform a reconnect at all, which is a fact about the deployment. The
// authority to act is the active generation, and nothing here grants any.
type RecoveryConfig struct {
	ClientPath  string `json:"client_path"`
	ProfileID   string `json:"profile_id"`
	Mode        string `json:"mode"`
	Account     string `json:"keychain_account"`
	PINService  string `json:"keychain_pin_service"`
	TOTPService string `json:"keychain_totp_service"`
}

// valid refuses a half-written section. A recovery configuration missing a
// field is not a smaller capability, it is a deployment nobody finished, and
// discovering that at the moment a session needs recovering is the wrong time.
func (config *RecoveryConfig) valid() error {
	if config == nil {
		return nil
	}
	if config.ClientPath == "" || config.ProfileID == "" || config.Mode == "" ||
		config.Account == "" || config.PINService == "" || config.TOTPService == "" {
		return ErrInvalidConfig
	}
	return nil
}

// newRecovery builds the executor.
//
// Every failure to build one leaves an executor that can still ask the
// authorization question and still report an authorized act it cannot perform.
// A daemon that silently stopped asking would look identical to one nothing
// authorizes, which is the state this is meant to be distinguishable from.
func newRecovery(
	handler *policycontrol.Handler,
	config *RecoveryConfig,
	rootSocket string,
) *recovery {
	executor := &recovery{requestID: newRecoveryRequestID}
	if handler != nil {
		executor.bundleGeneration = handler.ActiveBundleGeneration
		executor.authorize = func(
			target string,
			generation uint64,
			digest string,
		) policy.ActionAuthorizationDecision {
			return handler.AuthorizePritunlRecovery(
				policy.DomainUser, target, generation, digest,
			)
		}
	}
	if rootSocket != "" {
		executor.roundTrip = func(
			ctx context.Context,
			request ipc.Request,
		) (ipc.Response, error) {
			return (ipc.Client{Path: rootSocket}).Do(ctx, request)
		}
	}
	if config == nil {
		return executor
	}
	source, err := credentials.NewKeychainSource(observe.ExecRunner{}, credentials.KeychainConfig{
		Account: config.Account, PINService: config.PINService,
		TOTPService: config.TOTPService,
	})
	if err != nil {
		return executor
	}
	client, err := pritunlclient.New(pritunlclient.Config{
		Path: config.ClientPath, ProfileID: config.ProfileID, Mode: config.Mode,
	}, pritunlclient.ExecRunner{})
	if err != nil {
		return executor
	}
	executor.source = source
	executor.client = client
	return executor
}

func newRecoveryRequestID() (string, error) {
	id, err := metadata.NewUUID(nil)
	if err != nil {
		return "", err
	}
	return string(id), nil
}
