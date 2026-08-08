package policyadvisor

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestUnsignedDraftIsRedactedAndCannotCompileAsOperatorPolicy(t *testing.T) {
	draft := validDraft()
	encoded, err := EncodeYAML(draft)
	if err != nil {
		t.Fatalf("EncodeYAML() error = %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"endpoint", "source_path", "credential", "signature", "approval", "pritunl-profile",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("draft contains forbidden field %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "status: unsigned_draft") ||
		!strings.Contains(text, "target_placeholder: "+TargetPlaceholder) {
		t.Fatalf("draft output = %s", text)
	}
	if _, err := policy.DecodeOperatorSource(bytes.NewReader(encoded)); !errors.Is(err, policy.ErrInvalidOperatorSource) {
		t.Fatalf("advisor draft compiled as operator policy: %v", err)
	}
}

func TestDraftRejectsLiveTargetAndInvalidEvidence(t *testing.T) {
	for _, mutate := range []func(*Draft){
		func(draft *Draft) { draft.SuggestedRule.TargetPlaceholder = "pritunl" },
		func(draft *Draft) { draft.EvidenceCount = 0 },
		func(draft *Draft) { draft.Status = "approved" },
		func(draft *Draft) { draft.LastObservedAt = "2030-01-01T00:00:02Z" },
	} {
		draft := validDraft()
		mutate(&draft)
		if _, err := EncodeYAML(draft); !errors.Is(err, ErrInvalidDraft) {
			t.Fatalf("EncodeYAML(%+v) error = %v, want %v", draft, err, ErrInvalidDraft)
		}
	}
}

func validDraft() Draft {
	return Draft{
		Schema: DraftSchema, Status: StatusUnsigned,
		DraftID:         "123e4567-e89b-42d3-a456-426614174000",
		CreatedAt:       "2030-01-01T00:00:01Z",
		Reason:          ReasonRepeatedDenial,
		EvidenceCount:   3,
		FirstObservedAt: "2030-01-01T00:00:00Z",
		LastObservedAt:  "2030-01-01T00:00:01Z",
		SuggestedRule: SuggestedRule{
			Effect: policy.EffectAllow, Domain: policy.DomainUser,
			Capability:        policy.CapabilityOperatorResume,
			TargetPlaceholder: TargetPlaceholder,
		},
	}
}
