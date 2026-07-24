CREATE TABLE latest_component_states (
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    component TEXT NOT NULL CHECK (char_length(component) BETWEEN 1 AND 64),
    control_state TEXT NOT NULL CHECK (char_length(control_state) BETWEEN 1 AND 64),
    health TEXT NOT NULL CHECK (char_length(health) BETWEEN 1 AND 64),
    reason_code TEXT NOT NULL CHECK (char_length(reason_code) BETWEEN 1 AND 64),
    generation BIGINT NOT NULL CHECK (generation > 0),
    observed_at TIMESTAMPTZ NOT NULL,
    event_id UUID REFERENCES events(event_id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (node_id, component)
);

CREATE TABLE sleep_intervals (
    sleep_interval_id UUID PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    boot_session_id UUID NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    start_event_id UUID REFERENCES events(event_id) ON DELETE SET NULL,
    end_event_id UUID REFERENCES events(event_id) ON DELETE SET NULL,
    reason_code TEXT NOT NULL CHECK (char_length(reason_code) BETWEEN 1 AND 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX sleep_intervals_node_time_idx
    ON sleep_intervals (node_id, started_at DESC);

CREATE TABLE incidents (
    incident_id UUID PRIMARY KEY,
    node_id UUID REFERENCES nodes(node_id) ON DELETE RESTRICT,
    correlation_key TEXT NOT NULL CHECK (char_length(correlation_key) BETWEEN 1 AND 192),
    category TEXT NOT NULL CHECK (char_length(category) BETWEEN 1 AND 64),
    component TEXT NOT NULL CHECK (char_length(component) BETWEEN 1 AND 64),
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    incident_status TEXT NOT NULL CHECK (
        incident_status IN ('open', 'acknowledged', 'resolved')
    ),
    requires_action BOOLEAN NOT NULL DEFAULT FALSE,
    generation BIGINT NOT NULL CHECK (generation > 0),
    opened_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL,
    acknowledged_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (last_observed_at >= opened_at),
    CHECK (resolved_at IS NULL OR resolved_at >= opened_at),
    CHECK (resolved_at IS NULL OR incident_status = 'resolved')
);

CREATE UNIQUE INDEX incidents_active_correlation_idx
    ON incidents (correlation_key)
    WHERE incident_status <> 'resolved';
CREATE INDEX incidents_status_time_idx
    ON incidents (incident_status, last_observed_at DESC);

CREATE TABLE incident_events (
    incident_id UUID NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events(event_id) ON DELETE CASCADE,
    evidence_role TEXT NOT NULL CHECK (
        evidence_role IN ('trigger', 'supporting', 'recovery', 'exclusion')
    ),
    linked_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (incident_id, event_id)
);

CREATE TABLE incident_transitions (
    incident_transition_id UUID PRIMARY KEY,
    incident_id UUID NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    generation BIGINT NOT NULL CHECK (generation > 0),
    from_status TEXT NOT NULL CHECK (
        from_status IN ('new', 'open', 'acknowledged', 'resolved')
    ),
    to_status TEXT NOT NULL CHECK (
        to_status IN ('open', 'acknowledged', 'resolved')
    ),
    reason_code TEXT NOT NULL CHECK (char_length(reason_code) BETWEEN 1 AND 64),
    transitioned_at TIMESTAMPTZ NOT NULL,
    UNIQUE (incident_id, generation)
);

CREATE TABLE incident_bundles (
    incident_bundle_id UUID PRIMARY KEY,
    incident_id UUID NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    object_key TEXT NOT NULL UNIQUE CHECK (char_length(object_key) BETWEEN 1 AND 512),
    content_sha256 BYTEA NOT NULL CHECK (octet_length(content_sha256) = 32),
    compressed_bytes BIGINT NOT NULL CHECK (compressed_bytes BETWEEN 1 AND 104857600),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK (deleted_at IS NULL OR deleted_at >= created_at)
);

CREATE TABLE config_versions (
    config_version_id UUID PRIMARY KEY,
    target_kind TEXT NOT NULL CHECK (target_kind IN ('node', 'group', 'global')),
    target_key TEXT NOT NULL CHECK (char_length(target_key) BETWEEN 1 AND 192),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    version_label TEXT NOT NULL CHECK (char_length(version_label) BETWEEN 1 AND 128),
    content_sha256 BYTEA NOT NULL CHECK (octet_length(content_sha256) = 32),
    signing_key_id TEXT NOT NULL CHECK (char_length(signing_key_id) BETWEEN 1 AND 128),
    lifecycle_status TEXT NOT NULL CHECK (
        lifecycle_status IN ('staged', 'active', 'proven', 'retired', 'rejected')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at TIMESTAMPTZ,
    proven_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    UNIQUE (target_kind, target_key, version_label)
);

CREATE TABLE deployments (
    deployment_id UUID PRIMARY KEY,
    node_id UUID REFERENCES nodes(node_id) ON DELETE RESTRICT,
    target_key TEXT NOT NULL CHECK (char_length(target_key) BETWEEN 1 AND 192),
    application_version TEXT NOT NULL CHECK (char_length(application_version) BETWEEN 1 AND 128),
    artifact_sha256 BYTEA NOT NULL CHECK (octet_length(artifact_sha256) = 32),
    config_version_id UUID REFERENCES config_versions(config_version_id) ON DELETE RESTRICT,
    deployment_status TEXT NOT NULL CHECK (
        deployment_status IN ('staged', 'started', 'healthy', 'rolled_back', 'failed')
    ),
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (completed_at IS NULL OR completed_at >= started_at)
);

CREATE INDEX deployments_target_time_idx
    ON deployments (target_key, started_at DESC);

CREATE TABLE worker_heartbeats (
    worker_name TEXT PRIMARY KEY CHECK (char_length(worker_name) BETWEEN 1 AND 64),
    instance_id UUID NOT NULL,
    application_version TEXT NOT NULL CHECK (
        char_length(application_version) BETWEEN 1 AND 128
    ),
    started_at TIMESTAMPTZ NOT NULL,
    heartbeat_at TIMESTAMPTZ NOT NULL,
    CHECK (heartbeat_at >= started_at)
);
