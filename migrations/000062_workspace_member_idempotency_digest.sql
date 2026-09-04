BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Keep the idempotency key and its normalized request digest together. The
-- nullable column preserves expand compatibility with older writers; new
-- writers always populate it alongside last_mutation_key.
ALTER TABLE workspace_members ADD COLUMN last_mutation_hash text;
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_mutation_hash_chk
  CHECK (last_mutation_hash IS NULL OR last_mutation_hash ~ '^[0-9a-f]{64}$');

COMMENT ON COLUMN workspace_members.last_mutation_hash IS
  'SHA-256 digest of the normalized member update payload; never stores the request body.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
