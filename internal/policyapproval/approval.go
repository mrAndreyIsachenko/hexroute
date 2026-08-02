package policyapproval

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"sort"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/replay"
)

const (
	ReviewSchema    = "hexroute.policy-review.v1"
	ApprovalSchema  = "hexroute.policy-approval.v1"
	MaxReviewChecks = 8
)

type GateCode string

const (
	GateConflictFree GateCode = "conflict_free"
	GateCompatible   GateCode = "compatible"
	GateSemanticDiff GateCode = "semantic_change"
	GateReplayPassed GateCode = "replay_passed"
)

type InstalledDomains struct {
	Root policy.InstalledCompatibility
	User policy.InstalledCompatibility
}

type ReviewReport struct {
	Schema                  string     `json:"schema"`
	ManifestSHA256          string     `json:"manifest_sha256"`
	CandidateSemanticSHA256 string     `json:"candidate_semantic_sha256"`
	DiffSHA256              string     `json:"diff_sha256"`
	ReplaySHA256            string     `json:"replay_sha256"`
	Checks                  []GateCode `json:"checks"`
}

type ApprovalStatement struct {
	Schema            string `json:"schema"`
	ManifestSHA256    string `json:"manifest_sha256"`
	RootSHA256        string `json:"root_sha256"`
	UserSHA256        string `json:"user_sha256"`
	ReviewSHA256      string `json:"review_sha256"`
	SignerFingerprint string `json:"signer_fingerprint"`
	NotBefore         string `json:"not_before"`
	ExpiresAt         string `json:"expires_at"`
}

type SignedApproval struct {
	Statement ApprovalStatement `json:"statement"`
	Signature string            `json:"signature"`
}

type Signer interface {
	PublicKey() (ed25519.PublicKey, error)
	Sign(message []byte) ([]byte, error)
}

var (
	ErrInvalidReview     = errors.New("invalid policy review report")
	ErrGateFailed        = errors.New("policy signing gate failed")
	ErrInvalidApproval   = errors.New("invalid policy approval")
	ErrSignerMismatch    = errors.New("policy signer fingerprint mismatch")
	ErrApprovalExpired   = errors.New("policy approval is outside its validity window")
	ErrApprovalSignature = errors.New("invalid policy approval signature")
)

func BuildReviewReport(
	candidate policy.CandidateBundle,
	current *policy.EffectiveSnapshot,
	diff policy.SemanticDiff,
	replayReport replay.PolicyReport,
	installed InstalledDomains,
) (ReviewReport, error) {
	if candidate.Validate() != nil || diff.Validate() != nil || replayReport.Validate() != nil {
		return ReviewReport{}, ErrGateFailed
	}
	if conflicts := policy.FindConflicts(candidate.Snapshot); !conflicts.Empty() {
		return ReviewReport{}, ErrGateFailed
	}
	candidateSemantic, err := policy.EffectiveSemanticSHA256(candidate.Snapshot)
	if err != nil || candidateSemantic != diff.CandidateSemanticSHA256 ||
		candidateSemantic != replayReport.CandidateSemanticSHA256 {
		return ReviewReport{}, ErrGateFailed
	}
	if current != nil {
		currentSemantic, err := policy.EffectiveSemanticSHA256(*current)
		if err != nil || currentSemantic != diff.CurrentSemanticSHA256 {
			return ReviewReport{}, ErrGateFailed
		}
		if noOp, err := policy.IsSemanticNoOp(*current, candidate.Snapshot); err != nil || noOp {
			return ReviewReport{}, ErrGateFailed
		}
	} else if diff.CurrentSemanticSHA256 != policy.SHA256Hex(nil) {
		return ReviewReport{}, ErrGateFailed
	}
	if replay.RequirePolicyReplay(replayReport) != nil ||
		policy.CheckCandidateCompatibility(candidate.Manifest, candidate.Root, installed.Root) != nil ||
		policy.CheckCandidateCompatibility(candidate.Manifest, candidate.User, installed.User) != nil {
		return ReviewReport{}, ErrGateFailed
	}
	diffDigest, err := policy.SemanticDiffSHA256(diff)
	if err != nil {
		return ReviewReport{}, ErrGateFailed
	}
	replayDigest, err := replay.PolicyReportSHA256(replayReport)
	if err != nil {
		return ReviewReport{}, ErrGateFailed
	}
	report := ReviewReport{
		Schema: ReviewSchema, ManifestSHA256: candidate.ManifestSHA256,
		CandidateSemanticSHA256: candidateSemantic, DiffSHA256: diffDigest, ReplaySHA256: replayDigest,
		Checks: []GateCode{GateCompatible, GateConflictFree, GateReplayPassed, GateSemanticDiff},
	}
	if report.Validate() != nil {
		return ReviewReport{}, ErrInvalidReview
	}
	return report, nil
}

func (report ReviewReport) Validate() error {
	if report.Schema != ReviewSchema || !validDigest(report.ManifestSHA256) ||
		!validDigest(report.CandidateSemanticSHA256) || !validDigest(report.DiffSHA256) ||
		!validDigest(report.ReplaySHA256) || len(report.Checks) != 4 || len(report.Checks) > MaxReviewChecks {
		return ErrInvalidReview
	}
	checks := append([]GateCode(nil), report.Checks...)
	sort.Slice(checks, func(left, right int) bool { return checks[left] < checks[right] })
	expected := []GateCode{GateCompatible, GateConflictFree, GateReplayPassed, GateSemanticDiff}
	for index := range expected {
		if checks[index] != expected[index] {
			return ErrInvalidReview
		}
	}
	return nil
}

func ReviewSHA256(report ReviewReport) (string, error) {
	if report.Validate() != nil {
		return "", ErrInvalidReview
	}
	digest, _, err := policy.CanonicalSHA256(report)
	return digest, err
}

func ApproveCandidate(
	candidate policy.CandidateBundle,
	current *policy.EffectiveSnapshot,
	diff policy.SemanticDiff,
	replayReport replay.PolicyReport,
	installed InstalledDomains,
	signer Signer,
) (ReviewReport, SignedApproval, error) {
	if signer == nil {
		return ReviewReport{}, SignedApproval{}, ErrInvalidApproval
	}
	review, err := BuildReviewReport(candidate, current, diff, replayReport, installed)
	if err != nil {
		return ReviewReport{}, SignedApproval{}, err
	}
	publicKey, err := signer.PublicKey()
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ReviewReport{}, SignedApproval{}, ErrInvalidApproval
	}
	fingerprint := policy.SHA256Hex(publicKey)
	if fingerprint != candidate.Manifest.SignerFingerprint {
		return ReviewReport{}, SignedApproval{}, ErrSignerMismatch
	}
	reviewDigest, err := ReviewSHA256(review)
	if err != nil {
		return ReviewReport{}, SignedApproval{}, err
	}
	statement := ApprovalStatement{
		Schema: ApprovalSchema, ManifestSHA256: candidate.ManifestSHA256,
		RootSHA256: candidate.Manifest.Root.PayloadSHA256, UserSHA256: candidate.Manifest.User.PayloadSHA256,
		ReviewSHA256: reviewDigest, SignerFingerprint: fingerprint,
		NotBefore: candidate.Manifest.NotBefore, ExpiresAt: candidate.Manifest.ExpiresAt,
	}
	canonical, err := canonicalStatement(statement)
	if err != nil {
		return ReviewReport{}, SignedApproval{}, err
	}
	signature, err := signer.Sign(canonical)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, canonical, signature) {
		return ReviewReport{}, SignedApproval{}, ErrApprovalSignature
	}
	approval := SignedApproval{Statement: statement, Signature: base64.RawURLEncoding.EncodeToString(signature)}
	return review, approval, nil
}

func VerifyCandidate(
	candidate policy.CandidateBundle,
	review ReviewReport,
	approval SignedApproval,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
) error {
	if candidate.Validate() != nil || review.Validate() != nil || len(pinnedPublicKey) != ed25519.PublicKeySize {
		return ErrInvalidApproval
	}
	reviewDigest, err := ReviewSHA256(review)
	if err != nil {
		return ErrInvalidApproval
	}
	fingerprint := policy.SHA256Hex(pinnedPublicKey)
	statement := approval.Statement
	if statement.Schema != ApprovalSchema || statement.ManifestSHA256 != candidate.ManifestSHA256 ||
		statement.RootSHA256 != candidate.Manifest.Root.PayloadSHA256 ||
		statement.UserSHA256 != candidate.Manifest.User.PayloadSHA256 ||
		statement.ReviewSHA256 != reviewDigest || statement.SignerFingerprint != fingerprint ||
		candidate.Manifest.SignerFingerprint != fingerprint ||
		statement.NotBefore != candidate.Manifest.NotBefore || statement.ExpiresAt != candidate.Manifest.ExpiresAt ||
		review.ManifestSHA256 != candidate.ManifestSHA256 {
		return ErrSignerMismatch
	}
	notBefore, err := time.Parse(time.RFC3339Nano, statement.NotBefore)
	if err != nil {
		return ErrInvalidApproval
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, statement.ExpiresAt)
	if err != nil || now.Before(notBefore) || !now.Before(expiresAt) {
		return ErrApprovalExpired
	}
	canonical, err := canonicalStatement(statement)
	if err != nil {
		return ErrInvalidApproval
	}
	signature, err := base64.RawURLEncoding.DecodeString(approval.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(pinnedPublicKey, canonical, signature) {
		return ErrApprovalSignature
	}
	return nil
}

func canonicalStatement(statement ApprovalStatement) ([]byte, error) {
	canonical, err := policy.MarshalCanonical(statement)
	if err != nil {
		return nil, ErrInvalidApproval
	}
	return canonical, nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
