package reconciler

type FeatureGate struct {
	SyntheticEngine           bool
	SyntheticShadowComparison bool
	SyntheticReplay           bool
}

type StartupSurface struct {
	ProposalTranslation bool           `json:"proposal_translation"`
	ExecutionIPC        bool           `json:"execution_ipc"`
	ProposalComparison  bool           `json:"proposal_comparison"`
	Replay              bool           `json:"replay"`
	CapabilityIDs       []CapabilityID `json:"capability_ids"`
}

func BuildStartupSurface(gate Gate, registry Registry, feature FeatureGate) StartupSurface {
	if !gate.Ready() || !feature.SyntheticEngine || !registry.SyntheticOnly() {
		return StartupSurface{}
	}
	return StartupSurface{
		ProposalTranslation: true,
		ExecutionIPC:        true,
		ProposalComparison:  feature.SyntheticShadowComparison,
		Replay:              feature.SyntheticShadowComparison && feature.SyntheticReplay,
		CapabilityIDs:       registry.IDs(),
	}
}
