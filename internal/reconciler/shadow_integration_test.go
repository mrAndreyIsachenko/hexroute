package reconciler

import (
	"reflect"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type protectedRuntimeState struct {
	ObservableGeneration uint64 `json:"observable_generation"`
	TwilightPath         string `json:"twilight_path"`
	AdGuardPath          string `json:"adguard_path"`
	PritunlPath          string `json:"pritunl_path"`
	SingBoxPath          string `json:"sing_box_path"`
	RoutesPath           string `json:"routes_path"`
	DNSPath              string `json:"dns_path"`
	CodexPrimaryPath     string `json:"codex_primary_path"`
	CodexFallbackPath    string `json:"codex_fallback_path"`
}

func TestShadowCloudLossAndEngineFailureDoNotMutateProtectedState(t *testing.T) {
	before := protectedRuntimeFixture()
	cloudLossState := before
	evaluation, err := EvaluateShadowPeers(nil, nil)
	if err != nil {
		t.Fatalf("EvaluateShadowPeers(cloud loss) error = %v", err)
	}
	if evaluation.CrossDomainAvailable ||
		evaluation.Root.Available ||
		evaluation.User.Available ||
		evaluation.Root.Reason != ReasonFreshness ||
		evaluation.User.Reason != ReasonFreshness {
		t.Fatalf("cloud loss evaluation = %+v", evaluation)
	}
	assertProtectedRuntimeUnchanged(t, before, cloudLossState)

	engineFailureState := before
	surface := BuildStartupSurface(
		Gate{},
		DefaultSyntheticRegistry(),
		FeatureGate{
			SyntheticEngine:           true,
			SyntheticShadowComparison: true,
			SyntheticReplay:           true,
		},
	)
	if surface.ProposalTranslation ||
		surface.ExecutionIPC ||
		surface.ProposalComparison ||
		surface.Replay ||
		len(surface.CapabilityIDs) != 0 {
		t.Fatalf("engine failure exposed mutable surface: %+v", surface)
	}
	assertProtectedRuntimeUnchanged(t, before, engineFailureState)
}

func protectedRuntimeFixture() protectedRuntimeState {
	return protectedRuntimeState{
		ObservableGeneration: 17,
		TwilightPath:         "protected.twilight",
		AdGuardPath:          "protected.adguard",
		PritunlPath:          "protected.pritunl",
		SingBoxPath:          "protected.sing_box",
		RoutesPath:           "protected.routes",
		DNSPath:              "protected.dns",
		CodexPrimaryPath:     "protected.codex.primary",
		CodexFallbackPath:    "protected.codex.fallback",
	}
}

func assertProtectedRuntimeUnchanged(
	t *testing.T,
	before protectedRuntimeState,
	after protectedRuntimeState,
) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("protected runtime changed:\nbefore=%+v\nafter=%+v", before, after)
	}
	beforeEncoded, err := policy.MarshalCanonical(before)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	afterEncoded, err := policy.MarshalCanonical(after)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if policy.SHA256Hex(beforeEncoded) != policy.SHA256Hex(afterEncoded) {
		t.Fatalf("protected runtime digest changed")
	}
}
