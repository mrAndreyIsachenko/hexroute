CREATE TABLE hexroute_schema_migrations (
    version BIGINT PRIMARY KEY CHECK (version > 0),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    up_sha256 TEXT NOT NULL CHECK (up_sha256 ~ '^[0-9a-f]{64}$'),
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (name)
);

ALTER TABLE hexroute_schema_migrations OWNER TO hexroute_migrator;
GRANT ALL PRIVILEGES ON hexroute_schema_migrations TO hexroute_migrator;
