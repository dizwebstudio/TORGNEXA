BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- PostgreSQL 18 no longer exposes jsonb_object_length(). Count object keys
-- through the stable jsonb_object_keys() set-returning function instead.
-- The original trigger remains in the pre-v1 baseline for historical replay;
-- this migration replaces its executable body for current installations.
CREATE OR REPLACE FUNCTION upload_security_evidence_guard_insert() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE
  item jsonb;
BEGIN
  FOR item IN SELECT value FROM jsonb_array_elements(NEW.checks) LOOP
    IF jsonb_typeof(item)<>''object''
      OR (SELECT count(*) FROM jsonb_object_keys(item))<>2
      OR NOT (item ? ''code'')
      OR NOT (item ? ''outcome'')
      OR jsonb_typeof(item->''code'')<>''string''
      OR jsonb_typeof(item->''outcome'')<>''string''
      OR (item->>''code'') !~ ''^[a-z0-9][a-z0-9._-]{0,127}$''
      OR (item->>''outcome'') NOT IN (''pass'',''fail'') THEN
      RAISE EXCEPTION USING ERRCODE=''23514'', MESSAGE=''invalid upload security check evidence'';
    END IF;
  END LOOP;
  RETURN NEW;
END';

INSERT INTO migration_history (
  version,
  name,
  file_name,
  phase,
  risk,
  checksum_sha256,
  application_version,
  execution_id,
  duration_ms
) VALUES (
  current_setting('torgnexa.migration_version')::integer,
  current_setting('torgnexa.migration_name'),
  current_setting('torgnexa.migration_file'),
  current_setting('torgnexa.migration_phase'),
  current_setting('torgnexa.migration_risk'),
  current_setting('torgnexa.migration_checksum'),
  current_setting('torgnexa.application_version'),
  current_setting('torgnexa.migration_execution_id'),
  current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
