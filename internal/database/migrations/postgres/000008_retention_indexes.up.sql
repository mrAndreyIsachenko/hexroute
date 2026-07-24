CREATE INDEX incident_transitions_retention_idx
    ON incident_transitions (transitioned_at);

CREATE INDEX sleep_intervals_retention_idx
    ON sleep_intervals (ended_at)
    WHERE ended_at IS NOT NULL;

CREATE INDEX sequence_gaps_retention_idx
    ON sequence_gaps (resolved_at)
    WHERE resolved_at IS NOT NULL;

CREATE INDEX alert_deliveries_terminal_retention_idx
    ON alert_deliveries (updated_at)
    WHERE delivery_status IN ('delivered', 'suppressed');

CREATE INDEX batches_retention_idx
    ON batches (received_at);
