BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE edo_documents (organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL, provider text NOT NULL, provider_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, kind text NOT NULL, status text NOT NULL, artifact_ref text NOT NULL, signature_ref text NOT NULL, mchd_ref text NOT NULL DEFAULT '', counterparty_ref text NOT NULL, version bigint NOT NULL CHECK(version>0), observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,document_id), UNIQUE(organization_id,workspace_id,provider,provider_account_id,remote_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE edo_status_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL, remote_status text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION edo_status_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''edo status evidence is append-only''; END';
CREATE TRIGGER edo_status_evidence_append_only_guard BEFORE UPDATE OR DELETE ON edo_status_evidence FOR EACH ROW EXECUTE FUNCTION edo_status_evidence_append_only();
ALTER TABLE edo_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE edo_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY edo_documents_tenant_policy ON edo_documents FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE edo_status_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE edo_status_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY edo_status_evidence_tenant_policy ON edo_status_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
