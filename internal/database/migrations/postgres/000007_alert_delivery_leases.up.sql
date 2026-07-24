ALTER TABLE alert_deliveries
    ADD COLUMN claim_owner UUID,
    ADD COLUMN claim_until TIMESTAMPTZ,
    ADD COLUMN snapshot_status TEXT,
    ADD COLUMN snapshot_severity TEXT,
    ADD COLUMN snapshot_category TEXT,
    ADD COLUMN snapshot_component TEXT,
    ADD COLUMN snapshot_transitioned_at TIMESTAMPTZ,
    ADD CONSTRAINT alert_deliveries_claim_pair_check CHECK (
        (claim_owner IS NULL AND claim_until IS NULL) OR
        (claim_owner IS NOT NULL AND claim_until IS NOT NULL)
    );

UPDATE alert_deliveries d
SET snapshot_status = t.to_status,
    snapshot_severity = i.severity,
    snapshot_category = i.category,
    snapshot_component = i.component,
    snapshot_transitioned_at = t.transitioned_at
FROM incidents i
JOIN incident_transitions t ON t.incident_id = i.incident_id
WHERE d.incident_id = i.incident_id
  AND d.incident_generation = t.generation;

ALTER TABLE alert_deliveries
    ALTER COLUMN snapshot_status SET NOT NULL,
    ALTER COLUMN snapshot_severity SET NOT NULL,
    ALTER COLUMN snapshot_category SET NOT NULL,
    ALTER COLUMN snapshot_component SET NOT NULL,
    ALTER COLUMN snapshot_transitioned_at SET NOT NULL,
    ADD CONSTRAINT alert_deliveries_snapshot_status_check CHECK (
        snapshot_status IN ('open', 'acknowledged', 'resolved')
    ),
    ADD CONSTRAINT alert_deliveries_snapshot_severity_check CHECK (
        snapshot_severity IN ('info', 'warning', 'critical')
    ),
    ADD CONSTRAINT alert_deliveries_snapshot_category_check CHECK (
        char_length(snapshot_category) BETWEEN 1 AND 64
    ),
    ADD CONSTRAINT alert_deliveries_snapshot_component_check CHECK (
        char_length(snapshot_component) BETWEEN 1 AND 64
    );

CREATE INDEX alert_deliveries_claimable_idx
    ON alert_deliveries (channel, next_attempt_at, claim_until, created_at)
    WHERE delivery_status IN ('pending', 'failed');
