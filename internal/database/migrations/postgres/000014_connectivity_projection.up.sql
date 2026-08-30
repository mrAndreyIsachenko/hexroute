-- The redacted connectivity projection, stored as the latest state per node.
--
-- Every text column is constrained to the bounded-token alphabet the local
-- projection schema allows. That is deliberate duplication: the host already
-- refuses to serialize anything else, and this makes an address, a path, a
-- selector or a digest unstorable here even if a future encoder regressed.
--
-- There is no control column anywhere in this file. Nothing the cloud writes
-- is read back by a host.

CREATE TABLE connectivity_snapshots (
    node_id UUID PRIMARY KEY REFERENCES nodes(node_id) ON DELETE RESTRICT,
    event_id UUID REFERENCES events(event_id) ON DELETE SET NULL,
    boot_session_id UUID NOT NULL,
    node_sequence BIGINT NOT NULL CHECK (node_sequence > 0),
    observed_at TIMESTAMPTZ NOT NULL,
    snapshot_generation BIGINT NOT NULL CHECK (snapshot_generation > 0),
    reducer_version INTEGER NOT NULL CHECK (reducer_version > 0),
    bundle_generation BIGINT NOT NULL CHECK (bundle_generation >= 0),
    root_generation BIGINT NOT NULL CHECK (root_generation >= 0),
    user_generation BIGINT NOT NULL CHECK (user_generation >= 0),
    aggregate_state TEXT NOT NULL CHECK (aggregate_state ~ '^[a-z0-9_]{1,48}$'),
    authorization_state TEXT NOT NULL CHECK (authorization_state ~ '^[a-z0-9_]{1,48}$'),
    authorization_reason TEXT NOT NULL CHECK (authorization_reason ~ '^[a-z0-9_]{1,48}$'),
    open_gaps INTEGER NOT NULL CHECK (open_gaps >= 0),
    gap_overflow BOOLEAN NOT NULL,
    source_conflicts INTEGER NOT NULL CHECK (source_conflicts >= 0),
    awaiting_baseline INTEGER NOT NULL CHECK (awaiting_baseline >= 0),
    conflict_overflow BOOLEAN NOT NULL,
    -- A host whose read-model lineage was unrecoverable starts counting
    -- snapshot generations again. That is worth showing rather than smoothing
    -- over, so a generation that moved backwards under a later host position
    -- is recorded instead of being treated as a stale arrival.
    lineage_reset BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE connectivity_snapshot_components (
    node_id UUID NOT NULL
        REFERENCES connectivity_snapshots(node_id) ON DELETE CASCADE,
    component TEXT NOT NULL CHECK (component ~ '^[a-z0-9_]{1,48}$'),
    component_state TEXT NOT NULL CHECK (component_state ~ '^[a-z0-9_]{1,48}$'),
    freshness TEXT NOT NULL
        CHECK (freshness IN ('fresh', 'stale', 'never_observed')),
    diff_reason TEXT NOT NULL CHECK (diff_reason ~ '^[a-z0-9_]{1,48}$'),
    PRIMARY KEY (node_id, component)
);

CREATE TABLE connectivity_snapshot_proposal_classes (
    node_id UUID NOT NULL
        REFERENCES connectivity_snapshots(node_id) ON DELETE CASCADE,
    proposal_class TEXT NOT NULL CHECK (proposal_class ~ '^[a-z0-9_]{1,48}$'),
    proposal_count INTEGER NOT NULL CHECK (proposal_count > 0),
    PRIMARY KEY (node_id, proposal_class)
);

-- The worker finds unprojected events by comparing each node's stored host
-- position against the events it has not consumed yet.
CREATE INDEX connectivity_projection_pending_idx
    ON events (node_id, occurred_at, sequence)
    WHERE schema_name = 'connectivity.projection';

ALTER TABLE connectivity_snapshots OWNER TO hexroute_migrator;
ALTER TABLE connectivity_snapshot_components OWNER TO hexroute_migrator;
ALTER TABLE connectivity_snapshot_proposal_classes OWNER TO hexroute_migrator;

REVOKE ALL ON connectivity_snapshots FROM PUBLIC;
REVOKE ALL ON connectivity_snapshot_components FROM PUBLIC;
REVOKE ALL ON connectivity_snapshot_proposal_classes FROM PUBLIC;

-- The dashboard reads. It has no write grant on any of these, so a rendering
-- path cannot become a writing path.
GRANT SELECT ON
    connectivity_snapshots,
    connectivity_snapshot_components,
    connectivity_snapshot_proposal_classes
TO hexroute_dashboard;

-- The ingest role never derives the read model; it only stores signed events.
GRANT SELECT ON
    connectivity_snapshots,
    connectivity_snapshot_components,
    connectivity_snapshot_proposal_classes
TO hexroute_maintenance;
GRANT INSERT, UPDATE, DELETE ON
    connectivity_snapshots,
    connectivity_snapshot_components,
    connectivity_snapshot_proposal_classes
TO hexroute_maintenance;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'connectivity_snapshots',
        'connectivity_snapshot_components',
        'connectivity_snapshot_proposal_classes'
    ]
    LOOP
        EXECUTE format(
            'CREATE TRIGGER hexroute_write_gate BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION hexroute_enforce_writable()',
            table_name
        );
    END LOOP;
END
$$;
