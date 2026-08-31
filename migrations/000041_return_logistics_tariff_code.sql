BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE return_logistics_operations
  ADD COLUMN tariff_code integer NOT NULL DEFAULT 0;

ALTER TABLE return_logistics_operations
  ADD CONSTRAINT return_logistics_operations_tariff_code_chk CHECK (tariff_code >= 0);

COMMENT ON COLUMN return_logistics_operations.tariff_code IS 'Optional provider-native return tariff; zero means the selected return service does not require one.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
