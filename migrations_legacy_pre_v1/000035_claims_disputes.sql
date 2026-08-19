BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE claims (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, context text NOT NULL CHECK(context IN ('marketplace','carrier','supplier')), state text NOT NULL CHECK(state IN ('open','submitted','waiting','won','lost','closed')), order_id text NOT NULL DEFAULT '', provider_ref text NOT NULL DEFAULT '', carrier_ref text NOT NULL DEFAULT '', supplier_id text NOT NULL DEFAULT '', deadline timestamptz, escalation_at timestamptz, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE claim_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, claim_id text NOT NULL REFERENCES claims(id), evidence_id text NOT NULL, upload_id text NOT NULL, object_ref text NOT NULL, media_type text NOT NULL, added_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,claim_id,evidence_id));
CREATE TABLE claim_compensations (organization_id text NOT NULL, workspace_id text NOT NULL, claim_id text NOT NULL REFERENCES claims(id), amount_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), settlement_entry_id text NOT NULL DEFAULT '', payment_ref text NOT NULL DEFAULT '', PRIMARY KEY(organization_id,workspace_id,claim_id));
ALTER TABLE claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE claims FORCE ROW LEVEL SECURITY;
CREATE POLICY claims_tenant_policy ON claims FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE claim_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE claim_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY claim_evidence_tenant_policy ON claim_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE claim_compensations ENABLE ROW LEVEL SECURITY;
ALTER TABLE claim_compensations FORCE ROW LEVEL SECURITY;
CREATE POLICY claim_compensations_tenant_policy ON claim_compensations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
