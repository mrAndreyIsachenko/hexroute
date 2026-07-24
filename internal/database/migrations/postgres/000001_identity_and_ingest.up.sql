CREATE TABLE nodes (
    node_id UUID PRIMARY KEY,
    node_name TEXT NOT NULL CHECK (char_length(node_name) BETWEEN 1 AND 128),
    node_kind TEXT NOT NULL CHECK (node_kind IN ('mac', 'vps', 'cloud')),
    lifecycle_status TEXT NOT NULL DEFAULT 'active'
        CHECK (lifecycle_status IN ('active', 'retired', 'revoked')),
    expected_heartbeat_seconds INTEGER NOT NULL DEFAULT 60
        CHECK (expected_heartbeat_seconds BETWEEN 10 AND 86400),
    first_seen_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (revoked_at IS NULL OR lifecycle_status = 'revoked')
);

CREATE TABLE node_public_keys (
    public_key_id UUID PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    key_id TEXT NOT NULL CHECK (char_length(key_id) BETWEEN 1 AND 128),
    algorithm TEXT NOT NULL DEFAULT 'ed25519' CHECK (algorithm = 'ed25519'),
    public_key BYTEA NOT NULL CHECK (octet_length(public_key) = 32),
    key_status TEXT NOT NULL DEFAULT 'active'
        CHECK (key_status IN ('active', 'retired', 'revoked')),
    valid_from TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (node_id, key_id),
    CHECK (valid_until IS NULL OR valid_until > valid_from),
    CHECK (revoked_at IS NULL OR key_status = 'revoked')
);

CREATE INDEX node_public_keys_active_lookup_idx
    ON node_public_keys (node_id, key_id)
    WHERE key_status = 'active';

CREATE TABLE batches (
    batch_id UUID PRIMARY KEY,
    request_id UUID NOT NULL UNIQUE,
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    signing_key_id TEXT NOT NULL,
    protocol_version SMALLINT NOT NULL CHECK (protocol_version > 0),
    first_sequence BIGINT NOT NULL CHECK (first_sequence > 0),
    last_sequence BIGINT NOT NULL CHECK (last_sequence >= first_sequence),
    event_count INTEGER NOT NULL CHECK (event_count BETWEEN 1 AND 256),
    compressed_bytes INTEGER NOT NULL CHECK (compressed_bytes BETWEEN 1 AND 1048576),
    content_sha256 BYTEA NOT NULL CHECK (octet_length(content_sha256) = 32),
    signed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (batch_id, node_id),
    FOREIGN KEY (node_id, signing_key_id)
        REFERENCES node_public_keys(node_id, key_id) ON DELETE RESTRICT
);

CREATE INDEX batches_node_received_idx ON batches (node_id, received_at DESC);

CREATE TABLE events (
    event_id UUID PRIMARY KEY,
    batch_id UUID NOT NULL,
    node_id UUID NOT NULL,
    boot_session_id UUID NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    occurred_at TIMESTAMPTZ NOT NULL,
    monotonic_offset_ns BIGINT NOT NULL CHECK (monotonic_offset_ns >= 0),
    schema_name TEXT NOT NULL CHECK (char_length(schema_name) BETWEEN 1 AND 128),
    schema_version SMALLINT NOT NULL CHECK (schema_version > 0),
    priority TEXT NOT NULL CHECK (priority IN ('critical', 'operational', 'diagnostic')),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (batch_id, node_id)
        REFERENCES batches(batch_id, node_id) ON DELETE RESTRICT,
    UNIQUE (node_id, boot_session_id, sequence)
);

CREATE INDEX events_node_occurred_idx ON events (node_id, occurred_at DESC);
CREATE INDEX events_schema_occurred_idx ON events (schema_name, occurred_at DESC);
CREATE INDEX events_received_idx ON events (received_at);

CREATE TABLE node_sequence_cursors (
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    boot_session_id UUID NOT NULL,
    highest_sequence BIGINT NOT NULL CHECK (highest_sequence >= 0),
    next_expected_sequence BIGINT NOT NULL CHECK (next_expected_sequence > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (node_id, boot_session_id),
    CHECK (next_expected_sequence = highest_sequence + 1)
);

CREATE TABLE sequence_gaps (
    sequence_gap_id UUID PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
    boot_session_id UUID NOT NULL,
    first_sequence BIGINT NOT NULL CHECK (first_sequence > 0),
    last_sequence BIGINT NOT NULL CHECK (last_sequence >= first_sequence),
    detected_batch_id UUID NOT NULL REFERENCES batches(batch_id) ON DELETE RESTRICT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMPTZ,
    UNIQUE (node_id, boot_session_id, first_sequence, last_sequence),
    CHECK (resolved_at IS NULL OR resolved_at >= detected_at)
);

CREATE INDEX sequence_gaps_open_idx
    ON sequence_gaps (node_id, detected_at)
    WHERE resolved_at IS NULL;

CREATE TABLE security_audit_records (
    audit_record_id UUID PRIMARY KEY,
    node_id UUID REFERENCES nodes(node_id) ON DELETE RESTRICT,
    request_id UUID,
    category TEXT NOT NULL CHECK (
        category IN ('signature', 'replay', 'timestamp', 'schema', 'size', 'credential_canary')
    ),
    reason_code TEXT NOT NULL CHECK (char_length(reason_code) BETWEEN 1 AND 64),
    source_fingerprint BYTEA CHECK (
        source_fingerprint IS NULL OR octet_length(source_fingerprint) = 32
    ),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX security_audit_records_occurred_idx
    ON security_audit_records (occurred_at);
