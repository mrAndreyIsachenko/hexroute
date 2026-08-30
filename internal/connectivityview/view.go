// Package connectivityview renders the read model for the two audiences that
// are allowed to see it.
//
// The operator, on this host, gets everything: the aggregate beside every
// component record, freshness, gaps, conflicts, generations and what class of
// reconciliation each divergence would need. The cloud gets a projection that
// is an allowlist by construction — states, buckets, counts and generations,
// with no topology, no identity and no proposal digest.
//
// Keeping both in one package is deliberate. They are built from the same
// snapshot, and putting the wide view next to the narrow one makes it obvious
// which fields were dropped on the way out.
package connectivityview

import (
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// LocalComponent is one component as an operator sees it.
type LocalComponent struct {
	Component connectivity.Component            `json:"component"`
	Domain    policy.Domain                     `json:"domain"`
	Source    connectivity.SourceID             `json:"source"`
	State     connectivityreduce.ComponentState `json:"state"`
	Observed  connectivity.Lifecycle            `json:"observed"`
	Reason    connectivity.Reason               `json:"reason"`

	Freshness event.Freshness `json:"freshness"`
	// DeadlineIn is how many ticks of headroom the component had at
	// evaluation, negative once it has passed. It is shown locally because an
	// operator needs to know whether something is about to go quiet.
	DeadlineIn control.Tick `json:"deadline_in"`

	HasBaseline        bool   `json:"has_baseline"`
	RebaselineRequired bool   `json:"rebaseline_required"`
	Conflicts          uint32 `json:"conflicts"`

	Classification connectivityreduce.Classification `json:"classification"`
	DiffReason     connectivityreduce.DiffReason     `json:"diff_reason"`
	ProposalClass  connectivityreduce.ProposalClass  `json:"proposal_class,omitempty"`

	Corroborations []connectivityreduce.Corroboration `json:"corroborations,omitempty"`
}

// LocalSource is one source's integrity as an operator sees it.
type LocalSource struct {
	Source           connectivity.SourceID         `json:"source"`
	Domain           policy.Domain                 `json:"domain"`
	BootID           string                        `json:"boot_id"`
	LastSequence     uint64                        `json:"last_sequence"`
	Gaps             []connectivityaccept.GapRange `json:"gaps,omitempty"`
	GapOverflow      bool                          `json:"gap_overflow"`
	AwaitingBaseline bool                          `json:"awaiting_baseline"`
	Conflicts        uint32                        `json:"conflicts"`
}

// LocalStatus is the whole operator view.
type LocalStatus struct {
	SnapshotGeneration uint64                              `json:"snapshot_generation"`
	ReducerVersion     uint16                              `json:"reducer_version"`
	BootID             string                              `json:"boot_id"`
	EvaluationTick     control.Tick                        `json:"evaluation_tick"`
	Policy             connectivityreduce.PolicyDescriptor `json:"policy"`

	Aggregate     connectivityreduce.AggregateState      `json:"aggregate"`
	Authorization connectivityreduce.Authorization       `json:"authorization"`
	Reason        connectivityreduce.AuthorizationReason `json:"authorization_reason"`

	Components []LocalComponent `json:"components"`
	Sources    []LocalSource    `json:"sources"`

	OpenGaps        uint16 `json:"open_gaps"`
	GapOverflow     bool   `json:"gap_overflow"`
	SourceConflicts uint16 `json:"source_conflicts"`

	ProposalClasses []event.ProjectedProposalClass `json:"proposal_classes,omitempty"`
}

// freshness buckets a component without exposing a deadline.
func freshness(record connectivityreduce.ComponentRecord) event.Freshness {
	switch {
	case record.HostSequence == 0:
		return event.FreshnessNeverObserved
	case record.State == connectivityreduce.StateStale:
		return event.FreshnessStale
	default:
		return event.FreshnessFresh
	}
}

// proposalsByComponent indexes proposals so each component can show what its
// divergence would need, without the operator having to correlate by hand.
func proposalsByComponent(
	proposals []connectivityreduce.Proposal,
) map[connectivity.Component]connectivityreduce.ProposalClass {
	index := make(map[connectivity.Component]connectivityreduce.ProposalClass, len(proposals))
	for _, proposal := range proposals {
		index[proposal.Target] = proposal.Class
	}
	return index
}

func classCounts(proposals []connectivityreduce.Proposal) []event.ProjectedProposalClass {
	if len(proposals) == 0 {
		return nil
	}
	counts := make(map[connectivityreduce.ProposalClass]uint16, len(proposals))
	for _, proposal := range proposals {
		counts[proposal.Class]++
	}
	out := make([]event.ProjectedProposalClass, 0, len(counts))
	for class, count := range counts {
		out = append(out, event.ProjectedProposalClass{Class: string(class), Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out
}

// Local renders the operator view.
func Local(
	snapshot connectivityreduce.Snapshot,
	diff connectivityreduce.Diff,
	proposals []connectivityreduce.Proposal,
) LocalStatus {
	classifications := make(map[connectivity.Component]connectivityreduce.ComponentDiff, len(diff.Components))
	for _, entry := range diff.Components {
		classifications[entry.Component] = entry
	}
	classes := proposalsByComponent(proposals)

	status := LocalStatus{
		SnapshotGeneration: snapshot.Generation,
		ReducerVersion:     snapshot.ReducerVersion,
		BootID:             snapshot.BootID,
		EvaluationTick:     snapshot.EvaluationTick,
		Policy:             snapshot.Policy,
		Aggregate:          snapshot.Summary.State,
		Authorization:      snapshot.Authorization,
		Reason:             snapshot.Reason,
		OpenGaps:           snapshot.Summary.OpenGaps,
		GapOverflow:        snapshot.Summary.GapOverflow,
		SourceConflicts:    snapshot.Summary.SourceConflicts,
		ProposalClasses:    classCounts(proposals),
	}
	for _, record := range snapshot.Components {
		entry := classifications[record.Component]
		component := LocalComponent{
			Component:          record.Component,
			Domain:             record.Domain,
			Source:             record.Source,
			State:              record.State,
			Observed:           record.Observed,
			Reason:             record.Reason,
			Freshness:          freshness(record),
			HasBaseline:        record.HasBaseline,
			RebaselineRequired: record.RebaselineRequired,
			Conflicts:          record.Conflicts,
			Classification:     entry.Classification,
			DiffReason:         entry.Reason,
			ProposalClass:      classes[record.Component],
			Corroborations:     record.Corroborations,
		}
		if record.FreshnessDeadline > 0 && record.BootID == snapshot.BootID {
			component.DeadlineIn = record.FreshnessDeadline - snapshot.EvaluationTick
		}
		status.Components = append(status.Components, component)
	}
	for _, watermark := range snapshot.Sources {
		status.Sources = append(status.Sources, LocalSource{
			Source:           watermark.Source,
			Domain:           watermark.Domain,
			BootID:           watermark.BootID,
			LastSequence:     watermark.LastSequence,
			Gaps:             watermark.Gaps,
			GapOverflow:      watermark.GapOverflow,
			AwaitingBaseline: watermark.AwaitingBaseline,
			Conflicts:        watermark.Conflicts,
		})
	}
	return status
}

// Project renders what may leave the host.
//
// It is built from the snapshot rather than from the local view, so a field
// added to the operator view cannot reach the cloud by being carried along.
func Project(
	snapshot connectivityreduce.Snapshot,
	diff connectivityreduce.Diff,
	proposals []connectivityreduce.Proposal,
) event.ConnectivityProjection {
	classifications := make(map[connectivity.Component]connectivityreduce.ComponentDiff, len(diff.Components))
	for _, entry := range diff.Components {
		classifications[entry.Component] = entry
	}
	projection := event.ConnectivityProjection{
		SnapshotGeneration:  snapshot.Generation,
		ReducerVersion:      snapshot.ReducerVersion,
		BundleGeneration:    snapshot.Policy.BundleGeneration,
		RootGeneration:      snapshot.Policy.RootGeneration,
		UserGeneration:      snapshot.Policy.UserGeneration,
		Aggregate:           string(snapshot.Summary.State),
		Authorization:       string(snapshot.Authorization),
		AuthorizationReason: string(snapshot.Reason),
		OpenGaps:            snapshot.Summary.OpenGaps,
		GapOverflow:         snapshot.Summary.GapOverflow,
		SourceConflicts:     snapshot.Summary.SourceConflicts,
		ProposalClasses:     classCounts(proposals),
	}
	for _, record := range snapshot.Components {
		entry := classifications[record.Component]
		reason := string(entry.Reason)
		if reason == "" {
			reason = string(connectivityreduce.DiffReasonNone)
		}
		projection.Components = append(projection.Components, event.ProjectedComponent{
			Component: string(record.Component),
			State:     string(record.State),
			Freshness: freshness(record),
			Reason:    reason,
		})
	}
	return projection
}
