-- Async marketplace providers may return an operation ID before a remote
-- product ID exists. Keep the observation tenant-scoped while allowing that
-- normalized identity; at least one remote identity is still mandatory.
BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE marketplace_publication_observations
  DROP CONSTRAINT marketplace_publication_observations_ref_chk;

ALTER TABLE marketplace_publication_observations
  ADD CONSTRAINT marketplace_publication_observations_ref_chk CHECK (
    observation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    ((remote_id = '' AND remote_operation_id <> '') OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    (remote_operation_id = '' OR remote_operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled') AND
    moderation IN ('unknown','pending','approved','rejected') AND
    (snapshot_digest = '' OR snapshot_digest ~ '^[0-9a-f]{64}$')
  );

ALTER TABLE marketplace_publication_drifts
  ADD COLUMN IF NOT EXISTS remote_operation_id text NOT NULL DEFAULT '';

ALTER TABLE marketplace_publication_drifts
  DROP CONSTRAINT marketplace_publication_drifts_ref_chk;

ALTER TABLE marketplace_publication_drifts
  ADD CONSTRAINT marketplace_publication_drifts_ref_chk CHECK (
    drift_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    drift_type IN ('missing_remote_product','duplicate_remote_product','content_mismatch','attribute_mismatch','media_mismatch','mapping_conflict','moderation_rejected','publication_status_mismatch','unknown_write_outcome') AND
    (remote_id = '' OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    (remote_operation_id = '' OR remote_operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    (expected_digest = '' OR expected_digest ~ '^[0-9a-f]{64}$') AND
    (observed_digest = '' OR observed_digest ~ '^[0-9a-f]{64}$') AND
    (observed_state = '' OR observed_state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled'))
  );

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
