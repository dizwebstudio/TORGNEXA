BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Shipment creation payloads contain purpose-limited address/contact data.
-- Keep only an opaque SecretProvider reference in the operational projection;
-- the encrypted payload is opened by the worker for the single remote call.
ALTER TABLE logistics_shipments ADD COLUMN create_request_ref text NOT NULL DEFAULT '' CHECK(create_request_ref='' OR create_request_ref ~ '^sec:v1:[0-9a-f]{32}$');

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
