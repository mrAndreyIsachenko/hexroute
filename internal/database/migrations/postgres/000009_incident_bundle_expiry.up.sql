ALTER TABLE incident_bundles
    ADD COLUMN delete_claim_owner UUID,
    ADD COLUMN delete_claim_until TIMESTAMPTZ,
    ADD COLUMN delete_attempt_count INTEGER NOT NULL DEFAULT 0
        CHECK (delete_attempt_count BETWEEN 0 AND 1000),
    ADD COLUMN next_delete_attempt_at TIMESTAMPTZ,
    ADD COLUMN last_delete_result_code TEXT
        CHECK (
            last_delete_result_code IS NULL OR
            char_length(last_delete_result_code) BETWEEN 1 AND 64
        ),
    ADD CONSTRAINT incident_bundles_delete_claim_pair_check CHECK (
        (delete_claim_owner IS NULL) = (delete_claim_until IS NULL)
    );

UPDATE incident_bundles
SET next_delete_attempt_at = expires_at
WHERE next_delete_attempt_at IS NULL;

ALTER TABLE incident_bundles
    ALTER COLUMN next_delete_attempt_at SET NOT NULL;

CREATE UNIQUE INDEX incident_bundles_incident_content_uidx
    ON incident_bundles (incident_id, content_sha256);

CREATE INDEX incident_bundles_delete_due_idx
    ON incident_bundles (
        next_delete_attempt_at,
        expires_at,
        delete_claim_until,
        incident_bundle_id
    )
    WHERE deleted_at IS NULL;
