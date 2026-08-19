BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE marking_status_facts (organization_id text NOT NULL, workspace_id text NOT NULL, fact_id text NOT NULL, code_fingerprint text NOT NULL CHECK(length(code_fingerprint)=64), gtin text NOT NULL DEFAULT '', remote_status text NOT NULL, source_ref text NOT NULL, observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,fact_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE marking_reconciliations (organization_id text NOT NULL, workspace_id text NOT NULL, reconciliation_id text NOT NULL, code_fingerprint text NOT NULL CHECK(length(code_fingerprint)=64), expected_status text NOT NULL, remote_status text NOT NULL, outcome text NOT NULL CHECK(outcome IN ('match','drift')), observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,reconciliation_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION marking_status_facts_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''marking status facts are append-only''; END';
CREATE TRIGGER marking_status_facts_append_only_guard BEFORE UPDATE OR DELETE ON marking_status_facts FOR EACH ROW EXECUTE FUNCTION marking_status_facts_append_only();
ALTER TABLE marking_status_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE marking_status_facts FORCE ROW LEVEL SECURITY;
CREATE POLICY marking_status_facts_tenant_policy ON marking_status_facts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE marking_reconciliations ENABLE ROW LEVEL SECURITY;
ALTER TABLE marking_reconciliations FORCE ROW LEVEL SECURITY;
CREATE POLICY marking_reconciliations_tenant_policy ON marking_reconciliations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
