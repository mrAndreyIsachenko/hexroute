package reconciler

type FeatureGate struct {
	SyntheticEngine bool
}

type StartupSurface struct {
	ProposalTranslation bool           `json:"proposal_translation"`
	ExecutionIPC        bool           `json:"execution_ipc"`
	CapabilityIDs       []CapabilityID `json:"capability_ids"`
}

func BuildStartupSurface(gate Gate, registry Registry, feature FeatureGate) StartupSurface {
	if !gate.Ready() || !feature.SyntheticEngine || !registry.SyntheticOnly() {
		return StartupSurface{}
	}
	return StartupSurface{
		ProposalTranslation: true,
		ExecutionIPC:        true,
		CapabilityIDs:       registry.IDs(),
	}
}
