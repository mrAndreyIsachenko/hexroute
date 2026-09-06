// Package ingressagent is the ingress side of configuration delivery.
//
// It obtains a version by its own action and decides for itself whether to
// apply it. Nothing in the cloud can reach this: there is no route by which a
// version could be pushed at a host, and a host that is off when one is
// published simply finds it the next time it looks.
package ingressagent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configpublish"
	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
)

// Fetcher reads objects from the configuration store. What it returns is not
// authentic by having been returned.
type Fetcher interface {
	GetVersion(ctx context.Context, key string) ([]byte, error)
}

// Applier puts a configuration into service. It is separate from this package's
// decision to apply, so that what is verified and what is written are two
// steps and the first cannot be skipped by doing the second.
type Applier interface {
	Apply(ctx context.Context, content []byte) error
}

// Outcome is what one synchronisation did.
type Outcome string

const (
	// OutcomeApplied is a version verified and put into service.
	OutcomeApplied Outcome = "applied"
	// OutcomeUnchanged is the version already running.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeRefused is a version that failed a check. The host keeps serving
	// what it has.
	OutcomeRefused Outcome = "refused"
	// OutcomeUnreachable is a store that could not be read. It is not a
	// refusal: nothing was offered to refuse.
	OutcomeUnreachable Outcome = "unreachable"
	// OutcomeReturned is a return to the retained version.
	OutcomeReturned Outcome = "returned"
)

// Result is one synchronisation, in the form it is recorded.
type Result struct {
	Outcome       Outcome `json:"outcome"`
	VersionLabel  string  `json:"version_label,omitempty"`
	ContentSHA256 string  `json:"content_sha256,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	At            string  `json:"at"`
}

var (
	// ErrAgent is a misconfigured agent.
	ErrAgent = errors.New("invalid ingress configuration agent")
	// ErrNoRetainedVersion is a return with nothing to return to.
	ErrNoRetainedVersion = errors.New("no retained configuration version")
	// ErrApply is a configuration that could not be put into service.
	ErrApply = errors.New("configuration version could not be applied")
)

// Agent synchronises one target's configuration.
type Agent struct {
	fetcher Fetcher
	applier Applier
	store   *Store
	// pinned is the operator public key placed on this host when it was
	// built. It is the whole of what the host trusts; the store, the
	// transport and the key's own location are not evidence of anything.
	pinned ed25519.PublicKey
	target configversion.Target
	now    func() time.Time
}

// New builds an agent. A missing pinned key is refused rather than defaulted,
// because an agent that would accept any signature is worse than one that
// applies nothing.
func New(
	fetcher Fetcher,
	applier Applier,
	store *Store,
	pinned ed25519.PublicKey,
	target configversion.Target,
) (*Agent, error) {
	if fetcher == nil || applier == nil || store == nil ||
		len(pinned) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: incomplete agent", ErrAgent)
	}
	if target.Kind == "" || target.Key == "" {
		return nil, fmt.Errorf("%w: no target", ErrAgent)
	}
	return &Agent{
		fetcher: fetcher,
		applier: applier,
		store:   store,
		pinned:  append(ed25519.PublicKey(nil), pinned...),
		target:  target,
		now:     time.Now,
	}, nil
}

// Sync obtains the current version and applies it if it verifies.
//
// Every path that does not end in an application leaves the host serving what
// it already serves. That is the same decision whether the store was
// unreachable, the bytes were not a version, or the signature was somebody
// else's.
func (agent *Agent) Sync(ctx context.Context) (Result, error) {
	if agent == nil || ctx == nil {
		return Result{}, fmt.Errorf("%w: no agent", ErrAgent)
	}
	key := configpublish.CurrentKey(string(agent.target.Kind), agent.target.Key)
	encoded, err := agent.fetcher.GetVersion(ctx, key)
	if err != nil {
		return agent.record(Result{Outcome: OutcomeUnreachable, Reason: "store_unreachable"})
	}

	artifact, decodeErr := configversion.Decode(encoded)
	if decodeErr != nil {
		return agent.record(Result{
			Outcome: OutcomeRefused, Reason: configversion.Reason(decodeErr),
		})
	}
	content, verifyErr := configversion.Verify(artifact, agent.pinned, agent.target)
	if verifyErr != nil {
		return agent.record(Result{
			Outcome:      OutcomeRefused,
			VersionLabel: artifact.Statement.VersionLabel,
			Reason:       configversion.Reason(verifyErr),
		})
	}

	applied, appliedEncoded, hasApplied, err := agent.store.Applied()
	if err != nil {
		return Result{}, err
	}
	if hasApplied {
		if applied.Statement.ContentSHA256 == artifact.Statement.ContentSHA256 {
			return agent.record(Result{
				Outcome:       OutcomeUnchanged,
				VersionLabel:  artifact.Statement.VersionLabel,
				ContentSHA256: artifact.Statement.ContentSHA256,
			})
		}
		// A signed version that predates what is running is a valid signature
		// on the wrong thing: replacing what runs with something older is how
		// a fixed fault comes back.
		newer, err := isNewer(artifact.Statement, applied.Statement)
		if err != nil {
			return agent.record(Result{
				Outcome: OutcomeRefused, Reason: "malformed",
				VersionLabel: artifact.Statement.VersionLabel,
			})
		}
		if !newer {
			return agent.record(Result{
				Outcome:      OutcomeRefused,
				VersionLabel: artifact.Statement.VersionLabel,
				Reason:       "not_newer_than_running",
			})
		}
	}

	// The version being replaced is retained before the new one is applied,
	// so that returning to it is a local operation from here on.
	if hasApplied {
		if err := agent.store.Retain(appliedEncoded); err != nil {
			return Result{}, err
		}
	}
	if err := agent.applier.Apply(ctx, content); err != nil {
		// The host is now serving something nobody chose. Putting the
		// retained configuration back is the only outcome that leaves it
		// where it was.
		if hasApplied {
			_ = agent.applyRetained(ctx)
		}
		return agent.record(Result{
			Outcome:      OutcomeRefused,
			VersionLabel: artifact.Statement.VersionLabel,
			Reason:       "apply_failed",
		})
	}
	if err := agent.store.SetApplied(encoded); err != nil {
		return Result{}, err
	}
	return agent.record(Result{
		Outcome:       OutcomeApplied,
		VersionLabel:  artifact.Statement.VersionLabel,
		ContentSHA256: artifact.Statement.ContentSHA256,
	})
}

// Return puts the retained version back.
//
// It reads nothing from the network. That is the point of retaining: a host
// that cannot reach the store is exactly the host most likely to need this.
func (agent *Agent) Return(ctx context.Context) (Result, error) {
	if agent == nil || ctx == nil {
		return Result{}, fmt.Errorf("%w: no agent", ErrAgent)
	}
	retained, _, ok, err := agent.store.Retained()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNoRetainedVersion
	}
	// The retained version is verified again rather than trusted for having
	// been verified once. Local storage is not a signature either.
	content, err := configversion.Verify(retained, agent.pinned, agent.target)
	if err != nil {
		return agent.record(Result{
			Outcome: OutcomeRefused, Reason: configversion.Reason(err),
		})
	}
	if err := agent.applier.Apply(ctx, content); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrApply, err)
	}
	if err := agent.store.PromoteRetained(); err != nil {
		return Result{}, err
	}
	return agent.record(Result{
		Outcome:       OutcomeReturned,
		VersionLabel:  retained.Statement.VersionLabel,
		ContentSHA256: retained.Statement.ContentSHA256,
	})
}

func (agent *Agent) applyRetained(ctx context.Context) error {
	retained, _, ok, err := agent.store.Retained()
	if err != nil || !ok {
		return ErrNoRetainedVersion
	}
	content, err := configversion.Verify(retained, agent.pinned, agent.target)
	if err != nil {
		return err
	}
	return agent.applier.Apply(ctx, content)
}

func (agent *Agent) record(result Result) (Result, error) {
	result.At = agent.now().UTC().Format(time.RFC3339Nano)
	if err := agent.store.RecordResult(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func isNewer(candidate configversion.Statement, running configversion.Statement) (bool, error) {
	candidateAt, err := time.Parse(time.RFC3339Nano, candidate.CreatedAt)
	if err != nil {
		return false, err
	}
	runningAt, err := time.Parse(time.RFC3339Nano, running.CreatedAt)
	if err != nil {
		return false, err
	}
	return candidateAt.After(runningAt), nil
}
