package reconciler

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type ShadowDomainStatus struct {
	Domain      policy.Domain `json:"domain"`
	Available   bool          `json:"available"`
	ObserveOnly bool          `json:"observe_only"`
	Reason      Reason        `json:"reason"`
}

type ShadowPeerEvaluation struct {
	CrossDomainAvailable bool               `json:"cross_domain_available"`
	Root                 ShadowDomainStatus `json:"root"`
	User                 ShadowDomainStatus `json:"user"`
}

func EvaluateShadowPeers(
	root *ipc.ReconcilerShadowStatusResult,
	user *ipc.ReconcilerShadowStatusResult,
) (ShadowPeerEvaluation, error) {
	evaluation := ShadowPeerEvaluation{
		Root: ShadowDomainStatus{
			Domain: policy.DomainRoot, Reason: ReasonFreshness,
		},
		User: ShadowDomainStatus{
			Domain: policy.DomainUser, Reason: ReasonFreshness,
		},
	}
	if root != nil {
		status, err := shadowDomainStatus(*root, policy.DomainRoot)
		if err != nil {
			return ShadowPeerEvaluation{}, err
		}
		evaluation.Root = status
	}
	if user != nil {
		status, err := shadowDomainStatus(*user, policy.DomainUser)
		if err != nil {
			return ShadowPeerEvaluation{}, err
		}
		evaluation.User = status
	}
	evaluation.CrossDomainAvailable = evaluation.Root.Available && evaluation.User.Available
	return evaluation, nil
}

func shadowDomainStatus(
	status ipc.ReconcilerShadowStatusResult,
	expected policy.Domain,
) (ShadowDomainStatus, error) {
	if status.Validate() != nil || status.Domain != expected {
		return ShadowDomainStatus{}, ErrShadowStore
	}
	return ShadowDomainStatus{
		Domain:      expected,
		Available:   true,
		ObserveOnly: !status.ProposalTranslation && !status.ExecutionIPC,
		Reason:      ReasonAccepted,
	}, nil
}
