package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const dashboardLimit = 100

type Database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// connectivityStaleAfter is how long a stored projection stays current on the
// dashboard. It is generous on purpose: the page marks an old row rather than
// hiding it, and marking one too early would read as a node in trouble.
const connectivityStaleAfter = 15 * time.Minute

type PostgresStore struct {
	database         Database
	workerStaleAfter time.Duration
}

func NewPostgresStore(
	database Database,
	workerStaleAfter time.Duration,
) (*PostgresStore, error) {
	if database == nil || workerStaleAfter <= 0 {
		return nil, ErrInvalidDashboard
	}
	return &PostgresStore{
		database:         database,
		workerStaleAfter: workerStaleAfter,
	}, nil
}

func (store *PostgresStore) Load(
	ctx context.Context,
	at time.Time,
) (Snapshot, error) {
	if ctx == nil || at.IsZero() {
		return Snapshot{}, ErrInvalidDashboard
	}
	at = at.UTC()
	snapshot := Snapshot{GeneratedAt: at}
	var err error
	if snapshot.Nodes, err = store.loadNodes(ctx, at); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	if err = store.attachConnectivity(ctx, snapshot.Nodes, at); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	if snapshot.Incidents, err = store.loadIncidents(ctx); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	if snapshot.Deployments, err = store.loadDeployments(ctx); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	if snapshot.SLOs, err = store.loadSLOs(ctx); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	if snapshot.Workers, err = store.loadWorkers(ctx, at); err != nil {
		return Snapshot{}, ErrDashboardUnavailable
	}
	return snapshot, nil
}

func (store *PostgresStore) loadNodes(
	ctx context.Context,
	at time.Time,
) ([]Node, error) {
	rows, err := store.database.Query(ctx, `
		WITH selected_nodes AS (
			SELECT
				node_id,
				node_name,
				node_kind,
				last_seen_at,
				expected_heartbeat_seconds
			FROM nodes
			WHERE lifecycle_status = 'active'
			ORDER BY node_name, node_id
			LIMIT $1
		)
		SELECT
			n.node_id::text,
			n.node_name,
			n.node_kind,
			n.last_seen_at,
			n.expected_heartbeat_seconds,
			s.component,
			s.health,
			s.observed_at
		FROM selected_nodes n
		LEFT JOIN latest_component_states s ON s.node_id = n.node_id
		ORDER BY n.node_name, n.node_id, s.component
	`, dashboardLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var (
		nodes   []Node
		current *Node
	)
	for rows.Next() {
		var (
			nodeID     string
			name       string
			kind       string
			lastSeenAt *time.Time
			expected   int
			component  *string
			health     *string
			observedAt *time.Time
		)
		if err := rows.Scan(
			&nodeID,
			&name,
			&kind,
			&lastSeenAt,
			&expected,
			&component,
			&health,
			&observedAt,
		); err != nil {
			return nil, err
		}
		if current == nil || string(current.NodeID) != nodeID {
			node := Node{
				NodeID:     metadata.UUID(nodeID),
				Name:       name,
				Kind:       kind,
				LastSeenAt: utcPointer(lastSeenAt),
				Stale: lastSeenAt == nil ||
					lastSeenAt.Before(at.Add(-3*time.Duration(expected)*time.Second)),
			}
			nodes = append(nodes, node)
			current = &nodes[len(nodes)-1]
		}
		if component != nil && health != nil && observedAt != nil {
			current.Components = append(current.Components, Component{
				Name:       *component,
				Health:     *health,
				ObservedAt: observedAt.UTC(),
			})
		}
	}
	return nodes, rows.Err()
}

func (store *PostgresStore) loadIncidents(ctx context.Context) ([]Incident, error) {
	rows, err := store.database.Query(ctx, `
		SELECT
			category,
			component,
			severity,
			incident_status,
			requires_action,
			generation,
			last_observed_at
		FROM incidents
		ORDER BY
			(incident_status <> 'resolved') DESC,
			last_observed_at DESC,
			incident_id
		LIMIT $1
	`, dashboardLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var incidents []Incident
	for rows.Next() {
		var incident Incident
		if err := rows.Scan(
			&incident.Category,
			&incident.Component,
			&incident.Severity,
			&incident.Status,
			&incident.RequiresAction,
			&incident.Generation,
			&incident.LastObservedAt,
		); err != nil {
			return nil, err
		}
		incident.LastObservedAt = incident.LastObservedAt.UTC()
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

func (store *PostgresStore) loadDeployments(ctx context.Context) ([]Deployment, error) {
	rows, err := store.database.Query(ctx, `
		SELECT
			d.target_key,
			d.application_version,
			d.deployment_status,
			d.started_at,
			d.completed_at,
			COALESCE(c.version_label, '')
		FROM deployments d
		LEFT JOIN config_versions c
			ON c.config_version_id = d.config_version_id
		ORDER BY d.started_at DESC, d.deployment_id
		LIMIT $1
	`, dashboardLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deployments []Deployment
	for rows.Next() {
		var deployment Deployment
		if err := rows.Scan(
			&deployment.TargetKey,
			&deployment.ApplicationVersion,
			&deployment.Status,
			&deployment.StartedAt,
			&deployment.FinishedAt,
			&deployment.ConfigVersion,
		); err != nil {
			return nil, err
		}
		deployment.StartedAt = deployment.StartedAt.UTC()
		deployment.FinishedAt = utcPointer(deployment.FinishedAt)
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

func (store *PostgresStore) loadSLOs(ctx context.Context) ([]SLO, error) {
	rows, err := store.database.Query(ctx, `
		SELECT
			target_key,
			service,
			objective,
			window_start,
			window_end,
			eligible_milliseconds,
			good_milliseconds,
			bad_milliseconds,
			qualifying_count,
			total_count
		FROM slo_aggregates
		ORDER BY window_start DESC, service, objective, target_key
		LIMIT $1
	`, dashboardLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []SLO
	for rows.Next() {
		var value SLO
		if err := rows.Scan(
			&value.TargetKey,
			&value.Service,
			&value.Objective,
			&value.WindowStart,
			&value.WindowEnd,
			&value.EligibleMilliseconds,
			&value.GoodMilliseconds,
			&value.BadMilliseconds,
			&value.QualifyingCount,
			&value.TotalCount,
		); err != nil {
			return nil, err
		}
		value.WindowStart = value.WindowStart.UTC()
		value.WindowEnd = value.WindowEnd.UTC()
		values = append(values, value)
	}
	return values, rows.Err()
}

func (store *PostgresStore) loadWorkers(
	ctx context.Context,
	at time.Time,
) ([]Worker, error) {
	rows, err := store.database.Query(ctx, `
		SELECT worker_name, application_version, heartbeat_at
		FROM worker_heartbeats
		ORDER BY worker_name
		LIMIT $1
	`, dashboardLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workers []Worker
	for rows.Next() {
		var worker Worker
		if err := rows.Scan(
			&worker.Name,
			&worker.ApplicationVersion,
			&worker.HeartbeatAt,
		); err != nil {
			return nil, err
		}
		worker.HeartbeatAt = worker.HeartbeatAt.UTC()
		worker.Stale = worker.HeartbeatAt.Before(at.Add(-store.workerStaleAfter))
		workers = append(workers, worker)
	}
	return workers, rows.Err()
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// attachConnectivity puts each node's stored projection beside it.
//
// The dashboard reads this model and never derives it. Nothing here writes,
// and nothing here is sent to a host: what a page renders cannot become an
// input to the reduction that produced it.
func (store *PostgresStore) attachConnectivity(
	ctx context.Context,
	nodes []Node,
	at time.Time,
) error {
	if len(nodes) == 0 {
		return nil
	}
	index := make(map[metadata.UUID]int, len(nodes))
	for position := range nodes {
		index[nodes[position].NodeID] = position
	}
	if err := store.loadConnectivityRows(ctx, nodes, index, at); err != nil {
		return err
	}
	if err := store.loadConnectivityComponents(ctx, nodes, index); err != nil {
		return err
	}
	return store.loadConnectivityProposals(ctx, nodes, index)
}

func (store *PostgresStore) loadConnectivityRows(
	ctx context.Context,
	nodes []Node,
	index map[metadata.UUID]int,
	at time.Time,
) error {
	rows, err := store.database.Query(ctx, `
		SELECT
			node_id::text,
			observed_at,
			snapshot_generation,
			bundle_generation,
			root_generation,
			user_generation,
			aggregate_state,
			authorization_state,
			authorization_reason,
			open_gaps,
			gap_overflow,
			source_conflicts,
			awaiting_baseline,
			conflict_overflow,
			lineage_reset
		FROM connectivity_snapshots
		ORDER BY node_id
		LIMIT $1
	`, dashboardLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			nodeID     string
			entry      Connectivity
			generation int64
			bundle     int64
			root       int64
			user       int64
			openGaps   int
			conflicts  int
			awaiting   int
		)
		if err := rows.Scan(
			&nodeID,
			&entry.ObservedAt,
			&generation,
			&bundle,
			&root,
			&user,
			&entry.Aggregate,
			&entry.Authorization,
			&entry.AuthorizationReason,
			&openGaps,
			&entry.GapOverflow,
			&conflicts,
			&awaiting,
			&entry.ConflictOverflow,
			&entry.LineageReset,
		); err != nil {
			return err
		}
		position, known := index[metadata.UUID(nodeID)]
		if !known {
			continue
		}
		entry.ObservedAt = entry.ObservedAt.UTC()
		entry.Stale = entry.ObservedAt.Add(connectivityStaleAfter).Before(at)
		entry.SnapshotGeneration = uint64(generation)
		entry.BundleGeneration = uint64(bundle)
		entry.RootGeneration = uint64(root)
		entry.UserGeneration = uint64(user)
		entry.OpenGaps = uint16(openGaps)
		entry.SourceConflicts = uint16(conflicts)
		entry.AwaitingBaseline = uint16(awaiting)
		nodes[position].Connectivity = &entry
	}
	return rows.Err()
}

func (store *PostgresStore) loadConnectivityComponents(
	ctx context.Context,
	nodes []Node,
	index map[metadata.UUID]int,
) error {
	rows, err := store.database.Query(ctx, `
		SELECT node_id::text, component, component_state, freshness, diff_reason
		FROM connectivity_snapshot_components
		ORDER BY node_id, component
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var component ConnectivityComponent
		if err := rows.Scan(
			&nodeID,
			&component.Name,
			&component.State,
			&component.Freshness,
			&component.DiffReason,
		); err != nil {
			return err
		}
		position, known := index[metadata.UUID(nodeID)]
		if !known || nodes[position].Connectivity == nil {
			continue
		}
		nodes[position].Connectivity.Components = append(
			nodes[position].Connectivity.Components, component)
	}
	return rows.Err()
}

func (store *PostgresStore) loadConnectivityProposals(
	ctx context.Context,
	nodes []Node,
	index map[metadata.UUID]int,
) error {
	rows, err := store.database.Query(ctx, `
		SELECT node_id::text, proposal_class, proposal_count
		FROM connectivity_snapshot_proposal_classes
		ORDER BY node_id, proposal_class
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var class ConnectivityProposalClass
		var count int
		if err := rows.Scan(&nodeID, &class.Class, &count); err != nil {
			return err
		}
		position, known := index[metadata.UUID(nodeID)]
		if !known || nodes[position].Connectivity == nil || count < 1 {
			continue
		}
		class.Count = uint16(count)
		nodes[position].Connectivity.ProposalClasses = append(
			nodes[position].Connectivity.ProposalClasses, class)
	}
	return rows.Err()
}
