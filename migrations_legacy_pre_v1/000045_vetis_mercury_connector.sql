BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE vetis_reconciliation_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, remote_id text NOT NULL, kind text NOT NULL, remote_status text NOT NULL, stock_ref text NOT NULL DEFAULT '', source_request_ref text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION vetis_reconciliation_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''vetis evidence is append-only''; END';
CREATE TRIGGER vetis_reconciliation_evidence_append_only_guard BEFORE UPDATE OR DELETE ON vetis_reconciliation_evidence FOR EACH ROW EXECUTE FUNCTION vetis_reconciliation_evidence_append_only();
ALTER TABLE vetis_reconciliation_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE vetis_reconciliation_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY vetis_reconciliation_evidence_tenant_policy ON vetis_reconciliation_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
