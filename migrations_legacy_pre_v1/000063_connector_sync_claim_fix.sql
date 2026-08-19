BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- RETURNS TABLE output names are PL/pgSQL variables. Prefer qualified SQL
-- columns inside the already-bounded Task-108 claim function so PostgreSQL
-- does not reject its INSERT ... RETURNING clause as ambiguous.
ALTER FUNCTION claim_connector_sync_jobs(text,text,integer,integer)
  SET plpgsql.variable_conflict = 'use_column';

COMMENT ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) IS 'Bounded cross-tenant scheduler lease boundary; SQL column names take precedence over RETURNS TABLE output variables and callers must reapply returned tenant scope.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
