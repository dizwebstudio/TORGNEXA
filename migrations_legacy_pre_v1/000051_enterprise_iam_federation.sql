BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE enterprise_iam_mappings (organization_id text NOT NULL, workspace_id text NOT NULL, mapping_id text NOT NULL, protocol text NOT NULL CHECK(protocol IN ('ldap','active_directory','saml','scim','jit')), issuer text NOT NULL, external_selector text NOT NULL, role_code text NOT NULL, privileged boolean NOT NULL DEFAULT false, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,mapping_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE enterprise_identity_links (organization_id text NOT NULL, workspace_id text NOT NULL, issuer text NOT NULL, subject_fingerprint text NOT NULL, local_subject_id text NOT NULL, active boolean NOT NULL, last_reconciled_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,issuer,subject_fingerprint), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE enterprise_service_accounts (organization_id text NOT NULL, workspace_id text NOT NULL, service_account_id text NOT NULL, client_id text NOT NULL, secret_reference text NOT NULL, roles jsonb NOT NULL, disabled boolean NOT NULL, version bigint NOT NULL CHECK(version>0), rotated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,service_account_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
ALTER TABLE enterprise_iam_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_iam_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_iam_mappings_tenant_policy ON enterprise_iam_mappings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE enterprise_identity_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_identity_links FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_identity_links_tenant_policy ON enterprise_identity_links FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE enterprise_service_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_service_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_service_accounts_tenant_policy ON enterprise_service_accounts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
