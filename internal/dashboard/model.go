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
