package connectivityreduce

import (
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

var (
	ErrInvalidProposal = errors.New("reconciliation proposal is invalid")
	ErrStaleProposal   = errors.New("reconciliation proposal is stale")
)

// ProposalClass is the kind of reconciliation a divergence would need.
//
// It names a shape, not a step. There is deliberately no way to say how the
// work would be done: this change has no executor, and a proposal that
// described commands would be one refactor away from being run.
type ProposalClass string

const (
	// ProposalEstablish covers something policy wants that is not there.
	ProposalEstablish ProposalClass = "establish"
	// ProposalReconcile covers something that exists but differs.
	ProposalReconcile ProposalClass = "reconcile"
	// ProposalWithdraw covers something present that policy does not ask for
	// and that was never established under an earlier policy.
	ProposalWithdraw ProposalClass = "withdraw"
	// ProposalObserve covers uncertainty: the answer is a fresh observation,
	// never a network change.
	ProposalObserve ProposalClass = "observe"
)

func (class ProposalClass) valid() bool {
	switch class {
	case ProposalEstablish, ProposalReconcile, ProposalWithdraw, ProposalObserve:
		return true
	default:
		return false
	}
}

// Proposal is an immutable, digest-addressed statement that a divergence
// exists and which domain owns it.
//
// Every field is an identifier, an enumeration or a generation. There is no
// command, argument, path, endpoint, selector, process detail or credential
// reference, and no IPC operation accepts one.
type Proposal struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	SnapshotGeneration     uint64        `json:"snapshot_generation"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	DomainPolicyGeneration uint64        `json:"domain_policy_generation"`
	Domain                 policy.Domain `json:"domain"`

	// Target is a configured component, which is the only target identity
	// this model has. It is not a route, an endpoint or a process.
	Target connectivity.Component `json:"target"`
	Class  ProposalClass          `json:"class"`
	Reason DiffReason             `json:"reason"`

	DiffDigest string `json:"diff_digest"`
	// Digest addresses the proposal. It is computed over every other field,
	// so any edit produces a different proposal rather than a changed one.
	Digest string `json:"digest"`
}

// proposalBody is the proposal without its own address.
func proposalBody(proposal Proposal) Proposal {
	proposal.Digest = ""
	return proposal
}

// Verify recomputes the address and rejects a mutated proposal.
func (proposal Proposal) Verify() error {
	if proposal.Schema != ProposalSchema || proposal.Version != ProposalSchemaVersion {
		return fmt.Errorf("%w: schema", ErrInvalidProposal)
	}
	if !proposal.Target.Valid() || !proposal.Class.valid() {
		return fmt.Errorf("%w: target or class", ErrInvalidProposal)
	}
	if proposal.Domain != policy.DomainRoot && proposal.Domain != policy.DomainUser {
		return fmt.Errorf("%w: domain", ErrInvalidProposal)
	}
	if proposal.SnapshotGeneration == 0 || proposal.BundleGeneration == 0 ||
		len(proposal.DiffDigest) != 64 {
		return fmt.Errorf("%w: bindings", ErrInvalidProposal)
	}
	expected, _, err := policy.CanonicalSHA256(proposalBody(proposal))
	if err != nil {
		return fmt.Errorf("%w: encoding", ErrInvalidProposal)
	}
	if expected != proposal.Digest {
		return fmt.Errorf("%w: digest", ErrInvalidProposal)
	}
	return nil
}

// VerifyCurrent rejects a proposal that no longer describes the current
// snapshot and policy. A proposal minted before a state or policy change
// cannot be resumed: it must be produced again by a fresh reduction.
func (proposal Proposal) VerifyCurrent(snapshot Snapshot, diffDigest string) error {
	if err := proposal.Verify(); err != nil {
		return err
	}
	if proposal.SnapshotGeneration != snapshot.Generation {
		return fmt.Errorf("%w: snapshot generation %d, current %d",
			ErrStaleProposal, proposal.SnapshotGeneration, snapshot.Generation)
	}
	if proposal.BundleGeneration != snapshot.Policy.BundleGeneration {
		return fmt.Errorf("%w: bundle generation %d, current %d",
			ErrStaleProposal, proposal.BundleGeneration, snapshot.Policy.BundleGeneration)
	}
	if proposal.DomainPolicyGeneration != domainGeneration(snapshot.Policy, proposal.Domain) {
		return fmt.Errorf("%w: domain generation", ErrStaleProposal)
	}
	if proposal.DiffDigest != diffDigest {
		return fmt.Errorf("%w: diff digest", ErrStaleProposal)
	}
	return nil
}

func domainGeneration(descriptor PolicyDescriptor, domain policy.Domain) uint64 {
	if domain == policy.DomainUser {
		return descriptor.UserGeneration
	}
	return descriptor.RootGeneration
}

// Propose derives the proposals a diff implies.
//
// Converged components produce nothing, and neither does a grandfathered one:
// an established state that a new policy no longer permits is reported, never
// withdrawn. Withdrawing it would be a disconnect, and this change has no
// authority to disconnect anything.
func Propose(snapshot Snapshot, diff Diff) ([]Proposal, error) {
	if !diff.Authorized {
		return nil, nil
	}
	diffDigest, err := diff.Digest()
	if err != nil {
		return nil, err
	}
	var proposals []Proposal
	for _, entry := range diff.Components {
		class, wanted := proposalClass(entry.Classification)
		if !wanted {
			continue
		}
		proposal := Proposal{
			Schema:                 ProposalSchema,
			Version:                ProposalSchemaVersion,
			SnapshotGeneration:     snapshot.Generation,
			BundleGeneration:       snapshot.Policy.BundleGeneration,
			DomainPolicyGeneration: domainGeneration(snapshot.Policy, entry.Domain),
			Domain:                 entry.Domain,
			Target:                 entry.Component,
			Class:                  class,
			Reason:                 entry.Reason,
			DiffDigest:             diffDigest,
		}
		digest, _, err := policy.CanonicalSHA256(proposalBody(proposal))
		if err != nil {
			return nil, fmt.Errorf("%w: encoding", ErrInvalidProposal)
		}
		proposal.Digest = digest
		if err := proposal.Verify(); err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, nil
}

func proposalClass(classification Classification) (ProposalClass, bool) {
	switch classification {
	case ClassMissing:
		return ProposalEstablish, true
	case ClassDivergent:
		return ProposalReconcile, true
	case ClassUnexpected:
		return ProposalWithdraw, true
	case ClassStale, ClassUnknown, ClassConflict:
		return ProposalObserve, true
	default:
		// Converged needs nothing, and grandfathered is reported only.
		return "", false
	}
}
