package cloudincident

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/silentnode"
)

const (
	secondTriggerID    = metadata.UUID("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	integrationBatchID = metadata.UUID("44444444-4444-4444-8444-444444444444")
)

func TestPostgresIncidentLifecycleIsIdempotentAndSleepAware(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := incidentIntegrationPool(t, ctx, adminDSN)
	maintenance := incidentIntegrationPool(t, ctx, maintenanceDSN)
	resetIncidentIntegrationData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetIncidentIntegrationData(t, cleanupCtx, admin)
	})

	now := time.Date(2026, time.July, 25, 1, 0, 0, 0, time.UTC)
	seedIncidentEvents(t, ctx, admin, now)
	randomBytes := make([]byte, 16*16)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	store, err := NewPostgresStore(maintenance, bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}

	firstOutage := silentnode.Decision{
		NodeID:           testNodeID,
		State:            silentnode.StateSilent,
		ReferenceEventID: testTriggerID,
		EvaluatedAt:      now,
	}
	type reconcileResult struct {
		result Result
		err    error
	}
	concurrent := make(chan reconcileResult, 2)
	for range 2 {
		go func() {
			result, reconcileErr := store.ReconcileSilent(ctx, firstOutage)
			concurrent <- reconcileResult{result: result, err: reconcileErr}
		}()
	}
	first := <-concurrent
	second := <-concurrent
	for _, item := range []reconcileResult{first, second} {
		if item.err != nil {
			t.Fatalf("ReconcileSilent(concurrent open) error = %v", item.err)
		}
	}
	opened := first.result
	if !opened.Changed {
		opened = second.result
	}
	if !opened.Found || !opened.Changed ||
		opened.Status != StatusOpen || opened.Generation != 1 {
		t.Fatalf("opened = %+v", opened)
	}
	if first.result.IncidentID != second.result.IncidentID ||
		first.result.Changed == second.result.Changed {
		t.Fatalf("concurrent results = %+v / %+v", first.result, second.result)
	}
	duplicate, err := store.ReconcileSilent(ctx, firstOutage)
	if err != nil {
		t.Fatalf("ReconcileSilent(duplicate) error = %v", err)
	}
	if duplicate.Changed || duplicate.IncidentID != opened.IncidentID ||
		duplicate.Generation != 1 {
		t.Fatalf("duplicate = %+v", duplicate)
	}

	acknowledged, err := store.Acknowledge(
		ctx,
		"silent-node:"+string(testNodeID),
		now.Add(time.Minute),
		nil,
	)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	if !acknowledged.Changed ||
		acknowledged.Status != StatusAcknowledged ||
		acknowledged.Generation != 2 {
		t.Fatalf("acknowledged = %+v", acknowledged)
	}
	var lastObservedAfterAck time.Time
	if err := admin.QueryRow(ctx, `
		SELECT last_observed_at FROM incidents WHERE incident_id = $1
	`, string(opened.IncidentID)).Scan(&lastObservedAfterAck); err != nil {
		t.Fatalf("read last_observed_at after acknowledgement: %v", err)
	}
	if !lastObservedAfterAck.Equal(now) {
		t.Fatalf("acknowledgement changed condition time to %v", lastObservedAfterAck)
	}
	stillSilent := firstOutage
	stillSilent.EvaluatedAt = now.Add(2 * time.Minute)
	persistent, err := store.ReconcileSilent(ctx, stillSilent)
	if err != nil {
		t.Fatalf("ReconcileSilent(persistent) error = %v", err)
	}
	if persistent.Status != StatusAcknowledged || persistent.Generation != 2 {
		t.Fatalf("persistent = %+v", persistent)
	}

	conflicting, err := SignalFromSilentDecision(stillSilent)
	if err != nil {
		t.Fatalf("SignalFromSilentDecision() error = %v", err)
	}
	conflicting.Evidence[0].Role = EvidenceSupporting
	if _, err := store.Reconcile(ctx, conflicting); !errors.Is(err, ErrEvidenceConflict) {
		t.Fatalf("Reconcile(evidence conflict) error = %v, want %v", err, ErrEvidenceConflict)
	}

	recovered, err := store.ReconcileSilent(ctx, silentnode.Decision{
		NodeID:           testNodeID,
		State:            silentnode.StateHealthy,
		ReferenceEventID: testRecoveryID,
		EvaluatedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ReconcileSilent(recovery) error = %v", err)
	}
	if !recovered.Changed ||
		recovered.Status != StatusResolved ||
		recovered.Generation != 3 {
		t.Fatalf("recovered = %+v", recovered)
	}

	staleReplay := firstOutage
	staleReplay.EvaluatedAt = now.Add(2 * time.Minute)
	replayed, err := store.ReconcileSilent(ctx, staleReplay)
	if err != nil {
		t.Fatalf("ReconcileSilent(stale replay) error = %v", err)
	}
	if replayed.Changed || replayed.IncidentID != opened.IncidentID ||
		replayed.Status != StatusResolved {
		t.Fatalf("stale replay = %+v", replayed)
	}

	secondOutage := silentnode.Decision{
		NodeID:           testNodeID,
		State:            silentnode.StateSilent,
		ReferenceEventID: secondTriggerID,
		EvaluatedAt:      now.Add(4 * time.Minute),
	}
	reopened, err := store.ReconcileSilent(ctx, secondOutage)
	if err != nil {
		t.Fatalf("ReconcileSilent(reopen) error = %v", err)
	}
	if !reopened.Changed || reopened.Status != StatusOpen ||
		reopened.Generation != 1 || reopened.IncidentID == opened.IncidentID {
		t.Fatalf("reopened = %+v", reopened)
	}

	conflictingIdentity, err := SignalFromSilentDecision(secondOutage)
	if err != nil {
		t.Fatalf("SignalFromSilentDecision(second) error = %v", err)
	}
	conflictingIdentity.Category = event.IncidentDeployment
	if _, err := store.Reconcile(ctx, conflictingIdentity); !errors.Is(err, ErrIncidentConflict) {
		t.Fatalf("Reconcile(identity conflict) error = %v, want %v", err, ErrIncidentConflict)
	}

	sleepResolved, err := store.ReconcileSilent(ctx, silentnode.Decision{
		NodeID:       testNodeID,
		State:        silentnode.StateSleeping,
		SleepEventID: testSleepID,
		EvaluatedAt:  now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ReconcileSilent(sleep) error = %v", err)
	}
	if !sleepResolved.Changed ||
		sleepResolved.Status != StatusResolved ||
		sleepResolved.Generation != 2 {
		t.Fatalf("sleep resolved = %+v", sleepResolved)
	}

	assertIncidentLifecycle(t, ctx, admin, opened.IncidentID, reopened.IncidentID)
}

func incidentIntegrationPool(
	t *testing.T,
	ctx context.Context,
	dsn string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("PostgreSQL ping error = %v", err)
	}
	return pool
}

func resetIncidentIntegrationData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			incident_events,
			incident_transitions,
			incidents,
			events,
			batches,
			node_public_keys,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset incident data: %v", err)
	}
}

func seedIncidentEvents(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		INSERT INTO nodes (
			node_id,
			node_name,
			node_kind,
			expected_heartbeat_seconds,
			last_seen_at,
			created_at,
			updated_at
		) VALUES ($1, 'incident-test', 'mac', 60, $2, $2, $2)
	`, string(testNodeID), now)
	if err != nil {
		t.Fatalf("insert incident node: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO node_public_keys (
			public_key_id,
			node_id,
			key_id,
			public_key,
			key_status,
			valid_from
		) VALUES (
			'22222222-2222-4222-8222-222222222222',
			$1,
			'33333333-3333-4333-8333-333333333333',
			$2,
			'active',
			$3
		)
	`, string(testNodeID), make([]byte, 32), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("insert incident signing key: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO batches (
			batch_id,
			request_id,
			node_id,
			signing_key_id,
			protocol_version,
			first_sequence,
			last_sequence,
			event_count,
			compressed_bytes,
			content_sha256,
			signed_at,
			received_at
		) VALUES (
			$1,
			'55555555-5555-4555-8555-555555555555',
			$2,
			'33333333-3333-4333-8333-333333333333',
			1,
			1,
			4,
			4,
			256,
			$3,
			$4,
			$4
		)
	`, string(integrationBatchID), string(testNodeID), make([]byte, 32), now)
	if err != nil {
		t.Fatalf("insert incident batch: %v", err)
	}
	eventIDs := []metadata.UUID{
		testTriggerID,
		testRecoveryID,
		secondTriggerID,
		testSleepID,
	}
	for index, eventID := range eventIDs {
		_, err = admin.Exec(ctx, `
			INSERT INTO events (
				event_id,
				batch_id,
				node_id,
				boot_session_id,
				sequence,
				occurred_at,
				monotonic_offset_ns,
				schema_name,
				schema_version,
				priority,
				payload,
				received_at
			) VALUES (
				$1,
				$2,
				$3,
				'66666666-6666-4666-8666-666666666666',
				$4,
				$5,
				$4,
				'component.observation',
				1,
				'operational',
				'{}'::jsonb,
				$5
			)
		`,
			string(eventID),
			string(integrationBatchID),
			string(testNodeID),
			index+1,
			now.Add(time.Duration(index)*time.Minute),
		)
		if err != nil {
			t.Fatalf("insert incident event %d: %v", index, err)
		}
	}
}

func assertIncidentLifecycle(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	firstIncidentID metadata.UUID,
	secondIncidentID metadata.UUID,
) {
	t.Helper()
	var incidents int
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM incidents
		WHERE correlation_key = $1
	`, "silent-node:"+string(testNodeID)).Scan(&incidents); err != nil {
		t.Fatalf("count incidents: %v", err)
	}
	if incidents != 2 {
		t.Fatalf("incident count = %d, want 2", incidents)
	}

	var transitions int
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM incident_transitions
		WHERE incident_id IN ($1, $2)
	`, string(firstIncidentID), string(secondIncidentID)).Scan(&transitions); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if transitions != 5 {
		t.Fatalf("transition count = %d, want 5", transitions)
	}

	var (
		firstStatus      Status
		firstGeneration  int
		firstEvidence    int
		secondStatus     Status
		secondGeneration int
		secondEvidence   int
		exclusionLinks   int
	)
	if err := admin.QueryRow(ctx, `
		SELECT incident_status, generation
		FROM incidents WHERE incident_id = $1
	`, string(firstIncidentID)).Scan(&firstStatus, &firstGeneration); err != nil {
		t.Fatalf("read first incident: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT incident_status, generation
		FROM incidents WHERE incident_id = $1
	`, string(secondIncidentID)).Scan(&secondStatus, &secondGeneration); err != nil {
		t.Fatalf("read second incident: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM incident_events WHERE incident_id = $1
	`, string(firstIncidentID)).Scan(&firstEvidence); err != nil {
		t.Fatalf("count first evidence: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM incident_events WHERE incident_id = $1
	`, string(secondIncidentID)).Scan(&secondEvidence); err != nil {
		t.Fatalf("count second evidence: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM incident_events
		WHERE incident_id = $1 AND evidence_role = 'exclusion'
	`, string(secondIncidentID)).Scan(&exclusionLinks); err != nil {
		t.Fatalf("count exclusion evidence: %v", err)
	}
	if firstStatus != StatusResolved || firstGeneration != 3 || firstEvidence != 2 ||
		secondStatus != StatusResolved || secondGeneration != 2 ||
		secondEvidence != 2 || exclusionLinks != 1 {
		t.Fatalf(
			"first=(%s,%d,%d) second=(%s,%d,%d) exclusions=%d",
			firstStatus,
			firstGeneration,
			firstEvidence,
			secondStatus,
			secondGeneration,
			secondEvidence,
			exclusionLinks,
		)
	}
}
