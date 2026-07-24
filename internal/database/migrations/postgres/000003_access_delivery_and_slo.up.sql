CREATE TABLE dashboard_principals (
    principal_id UUID PRIMARY KEY,
    username TEXT NOT NULL UNIQUE CHECK (char_length(username) BETWEEN 1 AND 128),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 128),
    webauthn_user_handle BYTEA NOT NULL UNIQUE
        CHECK (octet_length(webauthn_user_handle) BETWEEN 16 AND 64),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_authenticated_at TIMESTAMPTZ
);

CREATE TABLE passkey_credentials (
    passkey_credential_id UUID PRIMARY KEY,
    principal_id UUID NOT NULL REFERENCES dashboard_principals(principal_id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE
        CHECK (octet_length(credential_id) BETWEEN 16 AND 1024),
    cose_public_key BYTEA NOT NULL
        CHECK (octet_length(cose_public_key) BETWEEN 1 AND 4096),
    sign_count BIGINT NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    aaguid UUID,
    transports TEXT[] NOT NULL DEFAULT '{}',
    nickname TEXT NOT NULL DEFAULT '' CHECK (char_length(nickname) <= 128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX passkey_credentials_principal_idx
    ON passkey_credentials (principal_id)
    WHERE revoked_at IS NULL;

CREATE TABLE alert_deliveries (
    alert_delivery_id UUID PRIMARY KEY,
    incident_id UUID NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    incident_generation BIGINT NOT NULL CHECK (incident_generation > 0),
    channel TEXT NOT NULL CHECK (
        channel IN ('telegram', 'local_macos', 'morning_digest')
    ),
    delivery_status TEXT NOT NULL CHECK (
        delivery_status IN ('pending', 'delivered', 'failed', 'suppressed')
    ),
    actionable BOOLEAN NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 1000),
    next_attempt_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    locally_acknowledged_at TIMESTAMPTZ,
    last_result_code TEXT CHECK (
        last_result_code IS NULL OR char_length(last_result_code) BETWEEN 1 AND 64
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (incident_id, incident_generation, channel),
    CHECK (delivered_at IS NULL OR delivery_status = 'delivered')
);

CREATE INDEX alert_deliveries_pending_idx
    ON alert_deliveries (next_attempt_at, created_at)
    WHERE delivery_status IN ('pending', 'failed');

CREATE TABLE slo_aggregates (
    slo_aggregate_id UUID PRIMARY KEY,
    granularity TEXT NOT NULL CHECK (granularity IN ('hour', 'day')),
    target_key TEXT NOT NULL CHECK (char_length(target_key) BETWEEN 1 AND 192),
    node_id UUID REFERENCES nodes(node_id) ON DELETE RESTRICT,
    service TEXT NOT NULL CHECK (
        service IN ('twilight_transport', 'codex_fallback', 'pritunl', 'telegram', 'safety')
    ),
    objective TEXT NOT NULL CHECK (char_length(objective) BETWEEN 1 AND 64),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    eligible_milliseconds BIGINT NOT NULL CHECK (eligible_milliseconds >= 0),
    good_milliseconds BIGINT NOT NULL CHECK (good_milliseconds >= 0),
    bad_milliseconds BIGINT NOT NULL CHECK (bad_milliseconds >= 0),
    excluded_milliseconds BIGINT NOT NULL CHECK (excluded_milliseconds >= 0),
    qualifying_count BIGINT NOT NULL DEFAULT 0 CHECK (qualifying_count >= 0),
    total_count BIGINT NOT NULL DEFAULT 0 CHECK (total_count >= qualifying_count),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (granularity, target_key, service, objective, window_start),
    CHECK (window_end > window_start),
    CHECK (good_milliseconds + bad_milliseconds <= eligible_milliseconds)
);

CREATE INDEX slo_aggregates_service_window_idx
    ON slo_aggregates (service, objective, window_start DESC);

CREATE TABLE slo_incident_links (
    slo_aggregate_id UUID NOT NULL
        REFERENCES slo_aggregates(slo_aggregate_id) ON DELETE CASCADE,
    incident_id UUID NOT NULL REFERENCES incidents(incident_id) ON DELETE CASCADE,
    linkage_role TEXT NOT NULL CHECK (
        linkage_role IN ('failure', 'exclusion', 'recovery')
    ),
    PRIMARY KEY (slo_aggregate_id, incident_id, linkage_role)
);
