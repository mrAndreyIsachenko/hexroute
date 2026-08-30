package alertdelivery

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	criticalIncidentID = metadata.UUID("71717171-7171-4171-8171-717171717171")
	recoveryIncidentID = metadata.UUID("72727272-7272-4272-8272-727272727272")
	dayIncidentID      = metadata.UUID("73737373-7373-4373-8373-737373737373")
	alertWorkerOneID   = metadata.UUID("81818181-8181-4181-8181-818181818181")
	alertWorkerTwoID   = metadata.UUID("82828282-8282-4282-8282-828282828282")
)

func TestPostgresAlertQueueLeasesRetriesAndKeepsLocalAckIsolated(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := alertIntegrationPool(t, ctx, adminDSN)
	maintenance := alertIntegrationPool(t, ctx, maintenanceDSN)
	resetAlertIntegrationData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		resetAlertIntegrationData(t, cleanupCtx, admin)
	})

	location := time.FixedZone("MSK", 3*60*60)
	night := time.Date(2026, time.July, 25, 2, 0, 0, 0, location)
	morning := time.Date(2026, time.July, 25, 8, 0, 0, 0, location)
	day := time.Date(2026, time.July, 25, 12, 0, 0, 0, location)
	seedAlertIncidents(t, ctx, admin, night.UTC(), day.UTC())

	randomBytes := make([]byte, 16*32)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	policy := Policy{
		NightStartHour: 23,
		NightEndHour:   8,
		Location:       location,
		LeaseDuration:  time.Minute,
		RetryMinimum:   time.Minute,
		RetryMaximum:   5 * time.Minute,
	}
	store, err := NewPostgresStore(
		maintenance,
		policy,
		bytes.NewReader(randomBytes),
	)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}

	if queued, err := store.QueueGeneration(ctx, criticalIncidentID, 1); err != nil || queued != 2 {
		t.Fatalf("QueueGeneration(critical) = %d, %v", queued, err)
	}
	if queued, err := store.QueueGeneration(ctx, criticalIncidentID, 1); err != nil || queued != 0 {
		t.Fatalf("QueueGeneration(duplicate) = %d, %v", queued, err)
	}
	if queued, err := store.QueueGeneration(ctx, recoveryIncidentID, 2); err != nil || queued != 2 {
		t.Fatalf("QueueGeneration(recovery) = %d, %v", queued, err)
	}
	if queued, err := store.QueueGeneration(ctx, dayIncidentID, 1); err != nil || queued != 1 {
		t.Fatalf("QueueGeneration(day) = %d, %v", queued, err)
	}
	_, err = admin.Exec(ctx, `
		UPDATE incidents
		SET severity = 'critical',
		    generation = 2,
		    last_observed_at = $2,
		    updated_at = $2
		WHERE incident_id = $1
	`, string(dayIncidentID), day.Add(time.Minute).UTC())
	if err != nil {
		t.Fatalf("advance day incident after queueing: %v", err)
	}
	_, err = admin.Exec(ctx, `
		INSERT INTO incident_transitions (
			incident_transition_id,
			incident_id,
			generation,
			from_status,
			to_status,
			reason_code,
			transitioned_at
		) VALUES (
			'95959595-9595-4595-8595-959595959595',
			$1,
			2,
			'open',
			'open',
			'condition_updated',
			$2
		)
	`, string(dayIncidentID), day.Add(time.Minute).UTC())
	if err != nil {
		t.Fatalf("insert advanced day transition: %v", err)
	}

	if err := store.AcknowledgeLocal(
		ctx,
		criticalIncidentID,
		1,
		night.Add(time.Minute),
	); err != nil {
		t.Fatalf("AcknowledgeLocal() error = %v", err)
	}
	assertLocalAckDidNotClearTelegram(t, ctx, admin)

	claimAt := day.Add(time.Hour).UTC()
	claimed, err := store.ClaimDue(
		ctx,
		alertWorkerOneID,
		ChannelTelegram,
		claimAt,
		10,
	)
	if err != nil {
		t.Fatalf("ClaimDue(telegram) error = %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed Telegram = %+v, want 2", claimed)
	}
	if competing, err := store.ClaimDue(
		ctx,
		alertWorkerTwoID,
		ChannelTelegram,
		claimAt.Add(30*time.Second),
		10,
	); err != nil || len(competing) != 0 {
		t.Fatalf("competing claim = %+v, %v", competing, err)
	}

	byIncident := make(map[metadata.UUID]Delivery, len(claimed))
	for _, delivery := range claimed {
		byIncident[delivery.Snapshot.IncidentID] = delivery
	}
	criticalDelivery := byIncident[criticalIncidentID]
	dayDelivery := byIncident[dayIncidentID]
	if criticalDelivery.DeliveryID == "" || dayDelivery.DeliveryID == "" {
		t.Fatalf("claimed by incident = %+v", byIncident)
	}
	if dayDelivery.Snapshot.Generation != 1 ||
		dayDelivery.Snapshot.Severity != "warning" {
		t.Fatalf("queued snapshot changed with incident: %+v", dayDelivery.Snapshot)
	}
	if err := store.Complete(
		ctx,
		alertWorkerOneID,
		[]metadata.UUID{criticalDelivery.DeliveryID},
		CompletionDelivered,
		claimAt,
	); err != nil {
		t.Fatalf("Complete(critical) error = %v", err)
	}
	if err := store.Complete(
		ctx,
		alertWorkerOneID,
		[]metadata.UUID{dayDelivery.DeliveryID},
		CompletionUnavailable,
		claimAt,
	); err != nil {
		t.Fatalf("Complete(day unavailable) error = %v", err)
	}
	if premature, err := store.ClaimDue(
		ctx,
		alertWorkerTwoID,
		ChannelTelegram,
		claimAt.Add(59*time.Second),
		10,
	); err != nil || len(premature) != 0 {
		t.Fatalf("premature retry = %+v, %v", premature, err)
	}
	retry, err := store.ClaimDue(
		ctx,
		alertWorkerTwoID,
		ChannelTelegram,
		claimAt.Add(time.Minute),
		10,
	)
	if err != nil || len(retry) != 1 || retry[0].DeliveryID != dayDelivery.DeliveryID {
		t.Fatalf("retry claim = %+v, %v", retry, err)
	}
	if err := store.Complete(
		ctx,
		alertWorkerTwoID,
		[]metadata.UUID{retry[0].DeliveryID},
		CompletionDelivered,
		claimAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("Complete(retry) error = %v", err)
	}

	digestAt := morning.Add(time.Minute).UTC()
	digest, err := store.ClaimDue(
		ctx,
		alertWorkerOneID,
		ChannelMorningDigest,
		digestAt,
		10,
	)
	if err != nil || len(digest) != 1 {
		t.Fatalf("ClaimDue(digest) = %+v, %v", digest, err)
	}
	if blocked, err := store.ClaimDue(
		ctx,
		alertWorkerTwoID,
		ChannelMorningDigest,
		digestAt.Add(59*time.Second),
		10,
	); err != nil || len(blocked) != 0 {
		t.Fatalf("digest lease block = %+v, %v", blocked, err)
	}
	reclaimed, err := store.ClaimDue(
		ctx,
		alertWorkerTwoID,
		ChannelMorningDigest,
		digestAt.Add(time.Minute),
		10,
	)
	if err != nil || len(reclaimed) != 1 ||
		reclaimed[0].DeliveryID != digest[0].DeliveryID ||
		reclaimed[0].AttemptCount != 2 {
		t.Fatalf("digest reclaim = %+v, %v", reclaimed, err)
	}
	if err := store.Complete(
		ctx,
		alertWorkerTwoID,
		[]metadata.UUID{reclaimed[0].DeliveryID},
		CompletionDelivered,
		digestAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("Complete(digest) error = %v", err)
	}

	assertAlertFinalState(t, ctx, admin)
}

func TestPostgresIncidentOutboxQueuesSnapshotExactlyOnce(t *testing.T) {
	adminDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	maintenanceDSN := os.Getenv("HEXROUTE_TEST_POSTGRES_MAINTENANCE_DSN")
	if adminDSN == "" || maintenanceDSN == "" {
		t.Skip("PostgreSQL integration DSNs are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := alertIntegrationPool(t, ctx, adminDSN)
	maintenance := alertIntegrationPool(t, ctx, maintenanceDSN)
	resetAlertIntegrationData(t, ctx, admin)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		resetAlertIntegrationData(t, cleanupCtx, admin)
	})

	// The plan a transition produces depends on the hour it was observed at,
	// so the hour is chosen rather than read from the clock. Taking the
	// instant straight from `time.Now` made this test assert the daytime
	// single-channel plan while actually exercising the two-channel night
	// plan for nine hours out of every day, where it could not pass.
	//
	// The instant still has to be in the future, because the outbox refuses a
	// row processed before it was created and `created_at` defaults to the
	// database's own clock. So the hour is fixed and the date is the next one
	// on which that hour has not yet passed.
	for _, testCase := range []struct {
		name           string
		correlationKey string
		hour           int
		seed           byte
		wantDeliveries int
	}{
		{"day", "outbox-runtime-day", 12, 0x10, 1},
		{"night", "outbox-runtime-night", 2, 0x40, 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetAlertIntegrationData(t, ctx, admin)
			assertOutboxQueuesExactlyOnce(
				t,
				ctx,
				admin,
				maintenance,
				testCase.correlationKey,
				nextUTCHour(testCase.hour),
				testCase.seed,
				testCase.wantDeliveries,
			)
		})
	}
}

// nextUTCHour returns the next instant at the given UTC hour that is still
// ahead of the clock.
func nextUTCHour(hour int) time.Time {
	now := time.Now().UTC()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if !candidate.After(now.Add(time.Minute)) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

// assertOutboxQueuesExactlyOnce drains one incident's outbox entry twice and
// checks that the second pass queues nothing further.
func assertOutboxQueuesExactlyOnce(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	maintenance *pgxpool.Pool,
	correlationKey string,
	observedAt time.Time,
	seed byte,
	wantDeliveries int,
) {
	t.Helper()
	incidentStore, err := cloudincident.NewPostgresStore(
		maintenance,
		alertRandomBytes(seed),
	)
	if err != nil {
		t.Fatalf("NewPostgresStore(incident) error = %v", err)
	}
	incident, err := incidentStore.Reconcile(ctx, cloudincident.Signal{
		CorrelationKey: correlationKey,
		Category:       event.IncidentAvailability,
		Component:      control.ComponentRuntime,
		Severity:       event.SeverityWarning,
		RequiresAction: false,
		State:          cloudincident.ConditionDetected,
		ObservedAt:     observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	policy := Policy{
		NightStartHour: 23,
		NightEndHour:   8,
		Location:       time.UTC,
		LeaseDuration:  time.Minute,
		RetryMinimum:   time.Minute,
		RetryMaximum:   5 * time.Minute,
	}
	// Every planned channel is given its own identity. A reader that repeats
	// one byte mints one UUID however many times it is called, so a plan with
	// more than one channel would collide on the delivery primary key for a
	// reason that has nothing to do with what this test asserts.
	alertStore, err := NewPostgresStore(maintenance, policy, alertRandomBytes(seed^0xff))
	if err != nil {
		t.Fatalf("NewPostgresStore(alert) error = %v", err)
	}
	plan, err := policy.Plan(Snapshot{
		IncidentID:     incident.IncidentID,
		Generation:     incident.Generation,
		Status:         cloudincident.StatusOpen,
		Severity:       event.SeverityWarning,
		Category:       event.IncidentAvailability,
		Component:      control.ComponentRuntime,
		TransitionedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan) != wantDeliveries {
		t.Fatalf("plan has %d channels, want %d", len(plan), wantDeliveries)
	}

	processed, err := alertStore.DrainOutbox(
		ctx,
		alertWorkerOneID,
		observedAt.Add(time.Second),
		10,
	)
	if err != nil || processed != 1 {
		t.Fatalf("DrainOutbox() = %d, %v", processed, err)
	}
	processed, err = alertStore.DrainOutbox(
		ctx,
		alertWorkerOneID,
		observedAt.Add(2*time.Second),
		10,
	)
	if err != nil || processed != 0 {
		t.Fatalf("DrainOutbox(repeat) = %d, %v", processed, err)
	}

	var (
		outboxProcessedAt *time.Time
		deliveryCount     int
		snapshotStatus    cloudincident.Status
		snapshotSeverity  event.IncidentSeverity
		snapshotAt        time.Time
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			outbox.processed_at,
			count(delivery.alert_delivery_id),
			min(delivery.snapshot_status),
			min(delivery.snapshot_severity),
			min(delivery.snapshot_transitioned_at)
		FROM incident_alert_outbox outbox
		LEFT JOIN alert_deliveries delivery
		  ON delivery.incident_id = outbox.incident_id
		 AND delivery.incident_generation = outbox.incident_generation
		WHERE outbox.incident_id = $1
		  AND outbox.incident_generation = $2
		GROUP BY outbox.processed_at
	`,
		string(incident.IncidentID),
		incident.Generation,
	).Scan(
		&outboxProcessedAt,
		&deliveryCount,
		&snapshotStatus,
		&snapshotSeverity,
		&snapshotAt,
	); err != nil {
		t.Fatalf("read drained outbox: %v", err)
	}
	if outboxProcessedAt == nil ||
		deliveryCount != wantDeliveries ||
		snapshotStatus != cloudincident.StatusOpen ||
		snapshotSeverity != event.SeverityWarning ||
		!snapshotAt.Equal(observedAt) {
		t.Fatalf(
			"processed=%v count=%d want=%d status=%s severity=%s at=%s",
			outboxProcessedAt,
			deliveryCount,
			wantDeliveries,
			snapshotStatus,
			snapshotSeverity,
			snapshotAt,
		)
	}
}

// alertRandomBytes returns a deterministic reader whose bytes vary, so every
// identity minted from it differs.
func alertRandomBytes(seed byte) *bytes.Reader {
	buffer := make([]byte, 16*32)
	for index := range buffer {
		buffer[index] = seed ^ byte(index)
	}
	return bytes.NewReader(buffer)
}

func alertIntegrationPool(
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

func resetAlertIntegrationData(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	_, err := admin.Exec(ctx, `
		TRUNCATE TABLE
			alert_deliveries,
			incident_alert_outbox,
			incident_transitions,
			incidents,
			nodes
		CASCADE
	`)
	if err != nil {
		t.Fatalf("reset alert data: %v", err)
	}
}

func seedAlertIncidents(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	night time.Time,
	day time.Time,
) {
	t.Helper()
	insertIncidentFixture(
		t,
		ctx,
		admin,
		criticalIncidentID,
		"critical-tunnel",
		"critical",
		"tunnel",
		"open",
		true,
		1,
		night,
	)
	insertIncidentFixture(
		t,
		ctx,
		admin,
		recoveryIncidentID,
		"night-recovery",
		"warning",
		"pritunl",
		"resolved",
		false,
		2,
		night,
	)
	_, err := admin.Exec(ctx, `
		INSERT INTO incident_transitions (
			incident_transition_id,
			incident_id,
			generation,
			from_status,
			to_status,
			reason_code,
			transitioned_at
		) VALUES (
			'92929292-9292-4292-8292-929292929292',
			$1,
			1,
			'new',
			'open',
			'condition_detected',
			$2
		)
	`, string(recoveryIncidentID), night.Add(-time.Minute))
	if err != nil {
		t.Fatalf("insert recovery opening transition: %v", err)
	}
	insertIncidentFixture(
		t,
		ctx,
		admin,
		dayIncidentID,
		"day-warning",
		"warning",
		"telegram",
		"open",
		false,
		1,
		day,
	)
}

func insertIncidentFixture(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
	incidentID metadata.UUID,
	correlationKey string,
	severity string,
	component string,
	status string,
	requiresAction bool,
	generation int,
	transitionedAt time.Time,
) {
	t.Helper()
	resolvedAt := any(nil)
	openedAt := transitionedAt
	fromStatus := "new"
	reason := "condition_detected"
	if status == "resolved" {
		resolvedAt = transitionedAt
		openedAt = transitionedAt.Add(-time.Minute)
		fromStatus = "open"
		reason = "condition_cleared"
	}
	_, err := admin.Exec(ctx, `
		INSERT INTO incidents (
			incident_id,
			correlation_key,
			category,
			component,
			severity,
			incident_status,
			requires_action,
			generation,
			opened_at,
			last_observed_at,
			resolved_at,
			created_at,
			updated_at
		) VALUES (
			$1,
			$2,
			'availability',
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$9,
			$9
		)
	`,
		string(incidentID),
		correlationKey,
		component,
		severity,
		status,
		requiresAction,
		generation,
		openedAt,
		transitionedAt,
		resolvedAt,
	)
	if err != nil {
		t.Fatalf("insert incident %s: %v", correlationKey, err)
	}
	transitionID := map[metadata.UUID]string{
		criticalIncidentID: "91919191-9191-4191-8191-919191919191",
		recoveryIncidentID: "93939393-9393-4393-8393-939393939393",
		dayIncidentID:      "94949494-9494-4494-8494-949494949494",
	}[incidentID]
	_, err = admin.Exec(ctx, `
		INSERT INTO incident_transitions (
			incident_transition_id,
			incident_id,
			generation,
			from_status,
			to_status,
			reason_code,
			transitioned_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		transitionID,
		string(incidentID),
		generation,
		fromStatus,
		status,
		reason,
		transitionedAt,
	)
	if err != nil {
		t.Fatalf("insert transition %s: %v", correlationKey, err)
	}
}

func assertLocalAckDidNotClearTelegram(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	var (
		localStatus       DeliveryStatus
		localAcknowledged *time.Time
		telegramStatus    DeliveryStatus
	)
	if err := admin.QueryRow(ctx, `
		SELECT delivery_status, locally_acknowledged_at
		FROM alert_deliveries
		WHERE incident_id = $1 AND channel = 'local_macos'
	`, string(criticalIncidentID)).Scan(&localStatus, &localAcknowledged); err != nil {
		t.Fatalf("read local delivery: %v", err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT delivery_status
		FROM alert_deliveries
		WHERE incident_id = $1 AND channel = 'telegram'
	`, string(criticalIncidentID)).Scan(&telegramStatus); err != nil {
		t.Fatalf("read Telegram delivery: %v", err)
	}
	if localStatus != StatusDelivered ||
		localAcknowledged == nil ||
		telegramStatus != StatusPending {
		t.Fatalf(
			"local=%s acknowledged=%v telegram=%s",
			localStatus,
			localAcknowledged,
			telegramStatus,
		)
	}
}

func assertAlertFinalState(
	t *testing.T,
	ctx context.Context,
	admin *pgxpool.Pool,
) {
	t.Helper()
	var (
		delivered  int
		suppressed int
		failed     int
		claimed    int
	)
	if err := admin.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE delivery_status = 'delivered'),
			count(*) FILTER (WHERE delivery_status = 'suppressed'),
			count(*) FILTER (WHERE delivery_status = 'failed'),
			count(*) FILTER (WHERE claim_owner IS NOT NULL OR claim_until IS NOT NULL)
		FROM alert_deliveries
	`).Scan(&delivered, &suppressed, &failed, &claimed); err != nil {
		t.Fatalf("read final alert state: %v", err)
	}
	if delivered != 4 || suppressed != 1 || failed != 0 || claimed != 0 {
		t.Fatalf(
			"delivered=%d suppressed=%d failed=%d claimed=%d",
			delivered,
			suppressed,
			failed,
			claimed,
		)
	}
}
