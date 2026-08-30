package connectivityhost

import (
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityview"
)

// Two planners look at this host every cycle and neither has ever been asked
// whether they agree.
//
// The component planners — the route planner in root, the Pritunl planner in
// user — decide what they would change. The read model, from its own facts and
// the active policy, decides what it would propose. They are built from
// different evidence and different rules, so agreement is worth something and
// disagreement is worth more: a materially divergent proposed action is what
// the roadmap requires resolving before any mutation authority is granted.
//
// Nothing here executes either one. The recorder cannot: a proposal and the
// code that changes a host may not sit in one package, so the planners' output
// arrives as bounded data rather than as a plan.

// PlannerIntent is what an existing component planner would do, reduced to the
// only two things a comparison needs: which component, and in which direction.
//
// It deliberately carries no destination, interface, path or reason text. The
// planners have those; a comparison does not need them, and a recorder that
// held them would be describing the host's topology in a file whose purpose is
// to describe a disagreement.
type PlannerIntent struct {
	Component connectivity.Component `json:"component"`
	// Establish is a planner asking for something to exist that does not, and
	// Withdraw one asking for something to stop. A planner proposing nothing
	// for a component contributes no intent at all.
	Establish bool `json:"establish"`
	Withdraw  bool `json:"withdraw"`
}

// Agreement is how one component's two opinions relate.
type Agreement string

const (
	// AgreementBoth means both planners want a change of the same direction.
	AgreementBoth Agreement = "both"
	// AgreementModelOnly means the read model proposes a change the component
	// planner does not. This is the interesting direction: the read model sees
	// freshness, ownership and policy authority that the planner does not.
	AgreementModelOnly Agreement = "model_only"
	// AgreementPlannerOnly means the component planner would act where the
	// read model proposes only observation. Before mutation authority exists
	// this is the direction that matters most, because that planner's output
	// is what a future executor would carry out.
	AgreementPlannerOnly Agreement = "planner_only"
	// AgreementDivergent means both would act, in opposite directions.
	AgreementDivergent Agreement = "divergent"
	// AgreementNeither means neither proposes a change.
	AgreementNeither Agreement = "neither"
)

// ComponentComparison is one component's pair of opinions.
type ComponentComparison struct {
	Component connectivity.Component           `json:"component"`
	Model     connectivityreduce.ProposalClass `json:"model_class,omitempty"`
	Reason    connectivityreduce.DiffReason    `json:"model_reason,omitempty"`
	Planner   PlannerIntent                    `json:"planner"`
	Agreement Agreement                        `json:"agreement"`
	// Authorized records whether the read model was allowed to want anything
	// at all. An unauthorized snapshot proposes only observation, so every
	// disagreement under it is explained by the absence of policy rather than
	// by the two planners seeing different things.
	Authorized bool `json:"authorized"`
}

// Comparison is one cycle's correlation.
type Comparison struct {
	Schema             string `json:"schema"`
	Version            uint16 `json:"version"`
	SnapshotGeneration uint64 `json:"snapshot_generation"`
	BootID             string `json:"boot_id"`

	Components []ComponentComparison `json:"components"`

	// Divergent counts the components where the two would act differently.
	// It is the number an operator watches during a shadow soak.
	Divergent uint16 `json:"divergent"`
}

const (
	// ComparisonSchema names the wire contract for one correlation.
	ComparisonSchema = "hexroute.connectivity-shadow-comparison.v1"
	// ComparisonSchemaVersion is bumped only for an incompatible change.
	ComparisonSchemaVersion uint16 = 1
)

// Compare correlates the read model's proposals with what the component
// planners would do.
//
// Every configured component appears, including those neither planner speaks
// about. A component missing from a comparison would be indistinguishable from
// one both agreed to leave alone.
func Compare(
	status connectivityview.LocalStatus,
	intents []PlannerIntent,
	bootID string,
	generation uint64,
	authorized bool,
) Comparison {
	byComponent := make(map[connectivity.Component]PlannerIntent, len(intents))
	for _, intent := range intents {
		// A planner proposing both directions for one component is proposing
		// neither coherently; the disagreement is with itself and is recorded
		// as an intent to act, without a direction.
		existing := byComponent[intent.Component]
		existing.Component = intent.Component
		existing.Establish = existing.Establish || intent.Establish
		existing.Withdraw = existing.Withdraw || intent.Withdraw
		byComponent[intent.Component] = existing
	}

	comparison := Comparison{
		Schema:             ComparisonSchema,
		Version:            ComparisonSchemaVersion,
		SnapshotGeneration: generation,
		BootID:             bootID,
	}
	for _, component := range connectivity.Components() {
		model, reason := proposalFor(status, component)
		intent := byComponent[component]
		entry := ComponentComparison{
			Component:  component,
			Model:      model,
			Reason:     reason,
			Planner:    intent,
			Authorized: authorized,
		}
		entry.Agreement = agree(model, intent)
		if entry.Agreement == AgreementDivergent {
			comparison.Divergent++
		}
		comparison.Components = append(comparison.Components, entry)
	}
	sort.Slice(comparison.Components, func(i, j int) bool {
		return comparison.Components[i].Component < comparison.Components[j].Component
	})
	return comparison
}

// proposalFor reads one component's proposal class out of the operator view.
func proposalFor(
	status connectivityview.LocalStatus,
	component connectivity.Component,
) (connectivityreduce.ProposalClass, connectivityreduce.DiffReason) {
	for _, entry := range status.Components {
		if entry.Component == component {
			return entry.ProposalClass, entry.DiffReason
		}
	}
	return "", ""
}

// agree decides how one component's two opinions relate.
//
// The read model's observe class is not a proposal to act: it is the class it
// uses when it cannot say what should be. Treating it as agreement with a
// planner that also does nothing would hide the case where the model is
// uncertain and the planner is confident.
func agree(model connectivityreduce.ProposalClass, intent PlannerIntent) Agreement {
	modelActs := model == connectivityreduce.ProposalEstablish ||
		model == connectivityreduce.ProposalReconcile ||
		model == connectivityreduce.ProposalWithdraw
	plannerActs := intent.Establish || intent.Withdraw
	switch {
	case !modelActs && !plannerActs:
		return AgreementNeither
	case modelActs && !plannerActs:
		return AgreementModelOnly
	case !modelActs && plannerActs:
		return AgreementPlannerOnly
	}
	// Both would act. Opposite directions are a divergence; the same direction
	// is agreement even when the classes are not identical, because reconcile
	// and establish both mean "make this exist".
	modelWithdraws := model == connectivityreduce.ProposalWithdraw
	switch {
	case modelWithdraws && intent.Withdraw:
		return AgreementBoth
	case !modelWithdraws && intent.Establish:
		return AgreementBoth
	default:
		return AgreementDivergent
	}
}
