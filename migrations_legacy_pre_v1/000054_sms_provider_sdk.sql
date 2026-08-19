BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE sms_delivery_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, phone_fingerprint text NOT NULL CHECK(char_length(phone_fingerprint)=64), message_class text NOT NULL CHECK(message_class IN ('transactional','marketing')), status text NOT NULL, idempotency_key text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,idempotency_key), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE sms_delivery_callbacks (organization_id text NOT NULL, workspace_id text NOT NULL, delivery_id text NOT NULL, remote_id text NOT NULL, status text NOT NULL, body_digest text NOT NULL CHECK(char_length(body_digest)=64), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,delivery_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION sms_delivery_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''sms_delivery_evidence is append-only''; END';
CREATE TRIGGER sms_delivery_evidence_append_only_guard BEFORE UPDATE OR DELETE ON sms_delivery_evidence FOR EACH ROW EXECUTE FUNCTION sms_delivery_evidence_append_only();
CREATE FUNCTION sms_delivery_callbacks_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''sms_delivery_callbacks is append-only''; END';
CREATE TRIGGER sms_delivery_callbacks_append_only_guard BEFORE UPDATE OR DELETE ON sms_delivery_callbacks FOR EACH ROW EXECUTE FUNCTION sms_delivery_callbacks_append_only();
ALTER TABLE sms_delivery_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_delivery_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY sms_delivery_evidence_tenant_policy ON sms_delivery_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE sms_delivery_callbacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_delivery_callbacks FORCE ROW LEVEL SECURITY;
CREATE POLICY sms_delivery_callbacks_tenant_policy ON sms_delivery_callbacks FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
