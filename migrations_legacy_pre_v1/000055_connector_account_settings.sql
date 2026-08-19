BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 105 aligns the database family vocabulary with the already reviewed
-- provider-neutral CRM SDK family. No provider id is admitted by this check.
ALTER TABLE connector_accounts DROP CONSTRAINT connector_accounts_family_v1_chk;
ALTER TABLE connector_accounts ADD CONSTRAINT connector_accounts_family_v1_chk CHECK (
  family IN ('marketplace','classified','social','erp','edo','government','payment','logistics','pickup','fx','notification','crm')
);

-- Community development needs one explicit tenant target for the local OIDC
-- administrator. Production tenant scope must come from reviewed OIDC claims;
-- these stable synthetic ids are not a production fallback.
INSERT INTO organizations (id, name)
VALUES ('0198b8d0-0000-7000-8000-000000000001', 'TORGNEXA Community')
ON CONFLICT (id) DO NOTHING;
INSERT INTO workspaces (id, organization_id, name)
VALUES ('0198b8d0-0000-7000-8000-000000000002', '0198b8d0-0000-7000-8000-000000000001', 'Community workspace')
ON CONFLICT (id) DO NOTHING;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
