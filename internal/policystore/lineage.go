package policystore

import (
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

// Lineage is what the store proves about the generation that came before.
//
// It deliberately carries no manifest, no payload and no approval. A caller
// holding one cannot evaluate policy from it, cannot authorize an action and
// cannot restore an active pointer, because none of the material those need is
// here. That is the whole point: the record answers "which generation preceded
// this one", and a generation that may no longer govern still has an answer.
//
// The predecessor's own validity window and static digest travel as facts about
// it rather than as conditions on it. Whether they match the present is a
// question for the candidate being installed, not for its parent.
type Lineage struct {
	Domain           policy.Domain
	Generation       Generation
	PayloadSHA256    string
	PolicySchema     uint16
	StaticSHA256     string
	NotBefore        string
	ExpiresAt        string
	ConfirmedAt      string
	Expired          bool
	StaticSuperseded bool
}

var ErrLineageEvidence = errors.New("policy lineage evidence is inconsistent")

// RecoverLineage derives the parent of the next generation from stored
// evidence.
//
// It verifies everything that proves the artifact: the records agree with the
// pointer, every artifact digest matches, the manifest, payload, review and
// approval cross-reference each other, the approval is signed by the pinned key,
// and the compiler that produced it is trusted. It does not check whether the
// approval is currently in force, and it does not compare the predecessor's
// static digest against the installed one.
//
// Those two are the only omissions, and they are omitted for the same reason:
// both ask about the present, and lineage is about the past. An expired
// generation was still the generation that came before, and so was one compiled
// against a safety envelope that has since gained a capability. Refusing on
// either would make the chain restart whenever time passes or the system grows,
// and a chain that restarts is not a chain.
//
// What may run is decided elsewhere, strictly, by RecoverActive and by the
// compatibility check applied to the candidate itself.
func (store *Store) RecoverLineage(
	installed policy.InstalledCompatibility,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
) (Lineage, error) {
	if installed.Validate() != nil || installed.Domain != storeDomain(store) ||
		len(pinnedPublicKey) != ed25519.PublicKeySize || now.IsZero() {
		return Lineage{}, policy.ErrInvalidCompatibility
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return Lineage{}, err
	}
	pointer, err := store.readStartupPointerLocked()
	if err != nil {
		return Lineage{}, err
	}
	if _, _, err := store.validateStartupRecordsLocked(pointer); err != nil {
		return Lineage{}, err
	}

	generation := Generation{Bundle: pointer.BundleGeneration, Policy: pointer.PolicyGeneration}
	manifestEncoded, err := store.readArtifactLocked(generation, ArtifactManifest)
	if err != nil {
		return Lineage{}, err
	}
	payloadEncoded, err := store.readArtifactLocked(generation, ArtifactPayload)
	if err != nil {
		return Lineage{}, err
	}
	reviewEncoded, err := store.readArtifactLocked(generation, ArtifactReview)
	if err != nil {
		return Lineage{}, err
	}
	approvalEncoded, err := store.readArtifactLocked(generation, ArtifactApproval)
	if err != nil {
		return Lineage{}, err
	}

	manifest, manifestDigest, err := policy.DecodeManifestArtifact(manifestEncoded)
	if err != nil {
		return Lineage{}, err
	}
	payload, payloadDigest, err := policy.DecodeDomainPayloadArtifact(payloadEncoded)
	if err != nil {
		return Lineage{}, err
	}
	review, err := policyapproval.DecodeReviewArtifact(reviewEncoded)
	if err != nil {
		return Lineage{}, err
	}
	approval, err := policyapproval.DecodeApprovalArtifact(approvalEncoded)
	if err != nil {
		return Lineage{}, err
	}
	approvalDigest, err := approvalSHA256(approval)
	if err != nil || manifestDigest != pointer.ManifestSHA256 ||
		payloadDigest != pointer.PayloadSHA256 || approvalDigest != pointer.ApprovalSHA256 ||
		approval != pointer.Approval {
		return Lineage{}, ErrActivePointerConsistency
	}
	if payload.Domain != store.domain || payload.PolicyGeneration != generation.Policy ||
		manifest.BundleGeneration != generation.Bundle {
		return Lineage{}, ErrLineageEvidence
	}
	if err := policyapproval.VerifyDomainBinding(
		manifest, manifestDigest, payload, review, approval, pinnedPublicKey,
	); err != nil {
		return Lineage{}, err
	}
	// The compiler that produced the predecessor is still checked. Unlike the
	// window and the static digest, trust in a compiler is a statement about
	// where the artifact came from rather than about what may run today, so
	// dropping it would weaken the proof rather than narrow the question.
	if !policy.CompilerIsTrusted(installed, manifest.CompilerSHA256) {
		return Lineage{}, policy.ErrUntrustedCompiler
	}

	return Lineage{
		Domain:           store.domain,
		Generation:       generation,
		PayloadSHA256:    payloadDigest,
		PolicySchema:     manifest.PolicySchema,
		StaticSHA256:     manifest.StaticSHA256,
		NotBefore:        manifest.NotBefore,
		ExpiresAt:        manifest.ExpiresAt,
		ConfirmedAt:      pointer.ConfirmedAt,
		Expired:          policyapproval.CheckApprovalWindow(approval, now) != nil,
		StaticSuperseded: manifest.StaticSHA256 != installed.StaticSHA256,
	}, nil
}
