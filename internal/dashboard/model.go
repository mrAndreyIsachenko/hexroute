package dashboard

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type Authorizer interface {
	Authorize(*http.Request) (metadata.UUID, string, bool)
}

type Store interface {
	Load(context.Context, time.Time) (Snapshot, error)
}

type Snapshot struct {
	GeneratedAt time.Time
	Nodes       []Node
	Incidents   []Incident
	Deployments []Deployment
	SLOs        []SLO
	Workers     []Worker
}

type Node struct {
	NodeID     metadata.UUID
	Name       string
	Kind       string
	LastSeenAt *time.Time
	Stale      bool
	Components []Component
	// Connectivity is the redacted read-model projection this node last
	// uploaded, or nil when it has never sent one. It is shown beside the
	// node rather than merged into it: what the host concluded and what the
	// cloud last heard are different facts, and a node that has gone quiet
	// keeps the second one long after the first stopped being true.
	Connectivity *Connectivity
}

// Connectivity is one node's stored connectivity projection.
//
// Every field is a bounded token, a count or a generation. There is no
// address, path, selector, session or proposal digest here, because the
// projection that produced it cannot express one.
type Connectivity struct {
	ObservedAt time.Time
	// Stale says the projection is older than the dashboard's freshness
	// window. A stale row is still shown, marked: hiding it would look like
	// a node with no read model rather than one that stopped reporting.
	Stale bool

	SnapshotGeneration uint64
	BundleGeneration   uint64
	RootGeneration     uint64
	UserGeneration     uint64

	Aggregate           string
	Authorization       string
	AuthorizationReason string

	OpenGaps         uint16
	GapOverflow      bool
	SourceConflicts  uint16
	AwaitingBaseline uint16
	ConflictOverflow bool
	LineageReset     bool

	Components      []ConnectivityComponent
	ProposalClasses []ConnectivityProposalClass
}

// ConnectivityComponent is one component of a stored projection.
type ConnectivityComponent struct {
	Name       string
	State      string
	Freshness  string
	DiffReason string
}

// ConnectivityProposalClass counts proposals of one class without naming any.
type ConnectivityProposalClass struct {
	Class string
	Count uint16
}

// NodeConnectivity pairs a node's name with its stored projection.
type NodeConnectivity struct {
	Name string
	Connectivity
}

// Connectivities returns one entry per node that has reported a projection.
//
// A node with none is left out rather than rendered as an empty row: it has
// not told the cloud anything about its read model, which is not the same as
// telling it there is nothing wrong.
func (snapshot Snapshot) Connectivities() []NodeConnectivity {
	entries := make([]NodeConnectivity, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if node.Connectivity == nil {
			continue
		}
		entries = append(entries, NodeConnectivity{
			Name:         node.Name,
			Connectivity: *node.Connectivity,
		})
	}
	return entries
}

type Component struct {
	Name       string
	Health     string
	ObservedAt time.Time
}

type Incident struct {
	Category       string
	Component      string
	Severity       string
	Status         string
	RequiresAction bool
	Generation     uint64
	LastObservedAt time.Time
}

type Deployment struct {
	TargetKey          string
	ApplicationVersion string
	Status             string
	StartedAt          time.Time
	FinishedAt         *time.Time
	ConfigVersion      string
}

type SLO struct {
	TargetKey            string
	Service              string
	Objective            string
	WindowStart          time.Time
	WindowEnd            time.Time
	EligibleMilliseconds int64
	GoodMilliseconds     int64
	BadMilliseconds      int64
	QualifyingCount      uint64
	TotalCount           uint64
}

type Worker struct {
	Name               string
	ApplicationVersion string
	HeartbeatAt        time.Time
	Stale              bool
}

var (
	ErrInvalidDashboard     = errors.New("invalid dashboard configuration")
	ErrDashboardUnavailable = errors.New("dashboard unavailable")
)
