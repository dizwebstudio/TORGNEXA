BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE regulated_government_document_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, connector_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, document_kind text NOT NULL, remote_status text NOT NULL, approval_ref text NOT NULL DEFAULT '', artifact_ref text NOT NULL DEFAULT '', idempotency_key text NOT NULL DEFAULT '', observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,connector_account_id,external_id,remote_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION regulated_government_document_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''regulated_government_document_evidence is append-only''; END';
CREATE TRIGGER regulated_government_document_evidence_append_only_guard BEFORE UPDATE OR DELETE ON regulated_government_document_evidence FOR EACH ROW EXECUTE FUNCTION regulated_government_document_evidence_append_only();
ALTER TABLE regulated_government_document_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE regulated_government_document_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY regulated_government_document_evidence_tenant_policy ON regulated_government_document_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
