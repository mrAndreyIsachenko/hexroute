CREATE TABLE incident_alert_outbox (
    incident_id UUID NOT NULL,
    incident_generation BIGINT NOT NULL CHECK (incident_generation > 0),
    node_id UUID,
    snapshot_status TEXT NOT NULL CHECK (
        snapshot_status IN ('open', 'acknowledged', 'resolved')
    ),
    snapshot_severity TEXT NOT NULL CHECK (
        snapshot_severity IN ('info', 'warning', 'critical')
    ),
    snapshot_category TEXT NOT NULL
        CHECK (char_length(snapshot_category) BETWEEN 1 AND 64),
    snapshot_component TEXT NOT NULL
        CHECK (char_length(snapshot_component) BETWEEN 1 AND 64),
    snapshot_requires_action BOOLEAN NOT NULL,
    snapshot_transitioned_at TIMESTAMPTZ NOT NULL,
    claim_owner UUID,
    claim_until TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0
        CHECK (attempt_count BETWEEN 0 AND 1000),
    last_result_code TEXT
        CHECK (
            last_result_code IS NULL OR
            char_length(last_result_code) BETWEEN 1 AND 64
        ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ,
    PRIMARY KEY (incident_id, incident_generation),
    FOREIGN KEY (incident_id, incident_generation)
        REFERENCES incident_transitions (incident_id, generation)
        ON DELETE CASCADE,
    FOREIGN KEY (node_id) REFERENCES nodes(node_id) ON DELETE RESTRICT,
    CHECK ((claim_owner IS NULL) = (claim_until IS NULL)),
    CHECK (processed_at IS NULL OR processed_at >= created_at)
);

CREATE INDEX incident_alert_outbox_due_idx
    ON incident_alert_outbox (
        claim_until,
        snapshot_transitioned_at,
        incident_id,
        incident_generation
    )
    WHERE processed_at IS NULL;

CREATE INDEX incident_alert_outbox_processed_idx
    ON incident_alert_outbox (processed_at, incident_id, incident_generation)
    WHERE processed_at IS NOT NULL;

ALTER TABLE incident_alert_outbox OWNER TO hexroute_migrator;
GRANT ALL PRIVILEGES ON incident_alert_outbox TO hexroute_migrator;
GRANT SELECT, INSERT, UPDATE, DELETE ON incident_alert_outbox
    TO hexroute_maintenance;
