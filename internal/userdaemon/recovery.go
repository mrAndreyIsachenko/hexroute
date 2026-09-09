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
	recoveryRefused    recoveryOutcome = "refused"
	recoveryFailed     recoveryOutcome = "failed"
)

// recovery performs what the planner decided, if anything authorizes it.
//
// It holds the two halves of the act in one place so that the authorization
// question is asked once, in one way: the reconnect it performs itself, and the
// service restart it asks root for. It cannot restart anything itself, and root
// cannot read what it holds.
type recovery struct {
	authorize func(string, uint64, string) policy.ActionAuthorizationDecision
	client    *pritunlclient.Client
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
			return recoveryFailed
		}
		return recoveryDone
	case pritunlplan.ActionRequestRescue:
		return executor.requestRescue(ctx, plan)
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
	request, err := pritunlrescue.NewRequest(id, executor.bundleGeneration())
	if err != nil {
		return recoveryFailed
	}
	response, err := executor.roundTrip(ctx, request)
	if err != nil {
		return recoveryFailed
	}
	if !response.OK {
		return recoveryRefused
	}
	return recoveryDone
}

// planDigest binds the authorization to what was decided, so that a lease
// issued for one plan cannot be spent on another.
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
