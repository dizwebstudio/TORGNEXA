BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE signing_certificates (organization_id text NOT NULL, workspace_id text NOT NULL, certificate_id text NOT NULL, serial text NOT NULL, thumbprint text NOT NULL CHECK(length(thumbprint)=64), subject_ref text NOT NULL, issuer_ref text NOT NULL, algorithm text NOT NULL, qualified boolean NOT NULL, not_before timestamptz NOT NULL, not_after timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,certificate_id), CHECK(not_after>not_before), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_mchd_authorities (organization_id text NOT NULL, workspace_id text NOT NULL, authority_id text NOT NULL, registry_ref text NOT NULL, principal_ref text NOT NULL, representative_ref text NOT NULL, powers jsonb NOT NULL, valid_from timestamptz NOT NULL, valid_until timestamptz NOT NULL, revoked boolean NOT NULL DEFAULT false, PRIMARY KEY(organization_id,workspace_id,authority_id), CHECK(valid_until>valid_from), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_requests (organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, artifact_ref text NOT NULL, digest_hex text NOT NULL CHECK(length(digest_hex)=64), certificate_id text NOT NULL, mchd_ref text NOT NULL DEFAULT '', purpose text NOT NULL, approval_ref text NOT NULL, idempotency_key text NOT NULL, requested_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,request_id), UNIQUE(organization_id,workspace_id,idempotency_key), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, signature_ref text NOT NULL, certificate_id text NOT NULL, mchd_ref text NOT NULL DEFAULT '', approval_ref text NOT NULL, digest_hex text NOT NULL CHECK(length(digest_hex)=64), signed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION signing_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''signing evidence is append-only''; END';
CREATE TRIGGER signing_evidence_append_only_guard BEFORE UPDATE OR DELETE ON signing_evidence FOR EACH ROW EXECUTE FUNCTION signing_evidence_append_only();
ALTER TABLE signing_certificates ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_certificates FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_certificates_tenant_policy ON signing_certificates FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_mchd_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_mchd_authorities FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_mchd_authorities_tenant_policy ON signing_mchd_authorities FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_requests_tenant_policy ON signing_requests FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_evidence_tenant_policy ON signing_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
