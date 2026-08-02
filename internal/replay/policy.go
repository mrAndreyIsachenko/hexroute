package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	PolicyCaseSchema    = "hexroute.policy-replay-case.v1"
	PolicyReportSchema  = "hexroute.policy-replay-report.v1"
	MaxPolicyCases      = 512
	MaxPolicyViolations = 64
	MaxPolicyCaseInput  = 256 * 1024
)

type PolicyCaseKind string

const (
	CaseSyntheticInvariant  PolicyCaseKind = "synthetic_invariant"
	CaseRedactedObservation PolicyCaseKind = "redacted_observation"
)

type PolicyDecision string

const (
	DecisionAllow PolicyDecision = "allow"
	DecisionDeny  PolicyDecision = "deny"
)

type PolicyCase struct {
	Schema     string            `json:"schema"`
	Kind       PolicyCaseKind    `json:"kind"`
	ID         string            `json:"id"`
	Domain     policy.Domain     `json:"domain"`
	Capability policy.Capability `json:"capability"`
	Target     string            `json:"target"`
	Expected   PolicyDecision    `json:"expected"`
}

type PolicyViolationCode string

const (
	ViolationUnexpectedAllow PolicyViolationCode = "unexpected_allow"
	ViolationUnexpectedDeny  PolicyViolationCode = "unexpected_deny"
	ViolationRootDivergence  PolicyViolationCode = "root_trace_diverged"
)

type PolicyViolation struct {
	Code       PolicyViolationCode `json:"code"`
	CaseSHA256 string              `json:"case_sha256"`
}

type PolicyReport struct {
	Schema                  string            `json:"schema"`
	CandidateSemanticSHA256 string            `json:"candidate_semantic_sha256"`
	InputSHA256             string            `json:"input_sha256"`
	SyntheticCases          uint16            `json:"synthetic_cases"`
	ObservationCases        uint16            `json:"observation_cases"`
	RootTraces              uint16            `json:"root_traces"`
	Passed                  bool              `json:"passed"`
	Violations              []PolicyViolation `json:"violations"`
	Truncated               bool              `json:"truncated"`
}

var (
	ErrInvalidPolicyCase   = errors.New("invalid policy replay case")
	ErrInvalidPolicyReport = errors.New("invalid policy replay report")
	ErrPolicyReplayFailed  = errors.New("policy replay safety gate failed")
)

func DecodePolicyCases(reader io.Reader) ([]PolicyCase, error) {
	content, err := io.ReadAll(io.LimitReader(reader, MaxPolicyCaseInput+1))
	if err != nil || len(content) == 0 || len(content) > MaxPolicyCaseInput {
		return nil, ErrInvalidPolicyCase
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), 16*1024)
	cases := make([]PolicyCase, 0)
	ids := make(map[string]struct{})
	for scanner.Scan() {
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		var candidate PolicyCase
		if err := decoder.Decode(&candidate); err != nil {
			return nil, ErrInvalidPolicyCase
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || !candidate.valid() {
			return nil, ErrInvalidPolicyCase
		}
		if _, duplicate := ids[candidate.ID]; duplicate {
			return nil, ErrInvalidPolicyCase
		}
		ids[candidate.ID] = struct{}{}
		cases = append(cases, candidate)
		if len(cases) > MaxPolicyCases {
			return nil, ErrInvalidPolicyCase
		}
	}
	if scanner.Err() != nil || len(cases) == 0 {
		return nil, ErrInvalidPolicyCase
	}
	return cases, nil
}

func EvaluatePolicy(snapshot policy.EffectiveSnapshot, cases []PolicyCase, rootTraces []Trace) (PolicyReport, error) {
	if snapshot.Validate() != nil || len(cases) == 0 || len(cases) > MaxPolicyCases {
		return PolicyReport{}, ErrInvalidPolicyCase
	}
	candidateDigest, err := policy.EffectiveSemanticSHA256(snapshot)
	if err != nil {
		return PolicyReport{}, err
	}
	inputDigests := make([]string, 0, len(cases)+len(rootTraces))
	violations := make([]PolicyViolation, 0)
	truncated := false
	syntheticCount := 0
	observationCount := 0
	appendViolation := func(violation PolicyViolation) {
		if len(violations) >= MaxPolicyViolations {
			truncated = true
			return
		}
		violations = append(violations, violation)
	}
	for _, candidate := range cases {
		if !candidate.valid() {
			return PolicyReport{}, ErrInvalidPolicyCase
		}
		caseDigest, _, err := policy.CanonicalSHA256(candidate)
		if err != nil {
			return PolicyReport{}, err
		}
		inputDigests = append(inputDigests, caseDigest)
		if candidate.Kind == CaseSyntheticInvariant {
			syntheticCount++
		} else {
			observationCount++
		}
		actual := evaluateAction(snapshot, candidate.Domain, candidate.Capability, candidate.Target)
		if actual == candidate.Expected {
			continue
		}
		code := ViolationUnexpectedDeny
		if actual == DecisionAllow {
			code = ViolationUnexpectedAllow
		}
		appendViolation(PolicyViolation{Code: code, CaseSHA256: caseDigest})
	}
	for _, trace := range rootTraces {
		traceDigest, _, err := policy.CanonicalSHA256(trace)
		if err != nil {
			return PolicyReport{}, err
		}
		inputDigests = append(inputDigests, traceDigest)
		if err := CompareRoot(trace); err != nil {
			appendViolation(PolicyViolation{Code: ViolationRootDivergence, CaseSHA256: traceDigest})
		}
	}
	sort.Strings(inputDigests)
	inputDigest, _, err := policy.CanonicalSHA256(inputDigests)
	if err != nil {
		return PolicyReport{}, err
	}
	sort.Slice(violations, func(left, right int) bool {
		if violations[left].Code != violations[right].Code {
			return violations[left].Code < violations[right].Code
		}
		return violations[left].CaseSHA256 < violations[right].CaseSHA256
	})
	report := PolicyReport{
		Schema: PolicyReportSchema, CandidateSemanticSHA256: candidateDigest,
		InputSHA256: inputDigest, SyntheticCases: uint16(syntheticCount),
		ObservationCases: uint16(observationCount), RootTraces: uint16(len(rootTraces)),
		Passed: len(violations) == 0 && !truncated, Violations: violations, Truncated: truncated,
	}
	if report.Validate() != nil {
		return PolicyReport{}, ErrInvalidPolicyReport
	}
	return report, nil
}

func (report PolicyReport) Validate() error {
	if report.Schema != PolicyReportSchema || !validDigest(report.CandidateSemanticSHA256) ||
		!validDigest(report.InputSHA256) || len(report.Violations) > MaxPolicyViolations ||
		report.Passed != (len(report.Violations) == 0 && !report.Truncated) ||
		int(report.SyntheticCases)+int(report.ObservationCases) == 0 {
		return ErrInvalidPolicyReport
	}
	for _, violation := range report.Violations {
		if !violation.Code.valid() || !validDigest(violation.CaseSHA256) {
			return ErrInvalidPolicyReport
		}
	}
	return nil
}

func PolicyReportSHA256(report PolicyReport) (string, error) {
	if report.Validate() != nil {
		return "", ErrInvalidPolicyReport
	}
	digest, _, err := policy.CanonicalSHA256(report)
	return digest, err
}

func RequirePolicyReplay(report PolicyReport) error {
	if report.Validate() != nil {
		return ErrInvalidPolicyReport
	}
	if !report.Passed {
		return ErrPolicyReplayFailed
	}
	return nil
}

func evaluateAction(snapshot policy.EffectiveSnapshot, domain policy.Domain, capability policy.Capability, target string) PolicyDecision {
	payload := snapshot.Root
	if domain == policy.DomainUser {
		payload = snapshot.User
	}
	allowed := false
	for _, rule := range payload.Rules {
		if rule.Selector.Kind != policy.SelectorAction ||
			rule.Selector.Action.Capability != capability || rule.Selector.Action.Target != target {
			continue
		}
		if rule.Effect == policy.EffectDeny {
			return DecisionDeny
		}
		allowed = true
	}
	if allowed {
		return DecisionAllow
	}
	return DecisionDeny
}

func (candidate PolicyCase) valid() bool {
	return candidate.Schema == PolicyCaseSchema && candidate.Kind.valid() &&
		validReplayID(candidate.ID) && candidate.Domain.Valid() && candidate.Capability.Valid() &&
		validReplayID(candidate.Target) && candidate.Expected.valid()
}

func (kind PolicyCaseKind) valid() bool {
	return kind == CaseSyntheticInvariant || kind == CaseRedactedObservation
}

func (decision PolicyDecision) valid() bool {
	return decision == DecisionAllow || decision == DecisionDeny
}

func (code PolicyViolationCode) valid() bool {
	return code == ViolationUnexpectedAllow || code == ViolationUnexpectedDeny ||
		code == ViolationRootDivergence
}

func validReplayID(value string) bool {
	if value == "" || len(value) > 80 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
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
