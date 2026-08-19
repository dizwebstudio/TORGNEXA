BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE siem_export_queue (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, event_type text NOT NULL, severity text NOT NULL, event_json jsonb NOT NULL, attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0), next_attempt_at timestamptz, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE siem_export_dlq (dlq_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, event_type text NOT NULL, event_digest text NOT NULL, attempts integer NOT NULL, failed_at timestamptz NOT NULL, reason_code text NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE siem_export_receipts (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, exported_at timestamptz NOT NULL, sink_count integer NOT NULL CHECK(sink_count>0), PRIMARY KEY(organization_id,workspace_id,event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION siem_export_dlq_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''siem_export_dlq is append-only''; END';
CREATE TRIGGER siem_export_dlq_append_only_guard BEFORE UPDATE OR DELETE ON siem_export_dlq FOR EACH ROW EXECUTE FUNCTION siem_export_dlq_append_only();
CREATE TRIGGER siem_export_receipts_append_only_guard BEFORE UPDATE OR DELETE ON siem_export_receipts FOR EACH ROW EXECUTE FUNCTION siem_export_dlq_append_only();
ALTER TABLE siem_export_queue ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_queue FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_queue_tenant_policy ON siem_export_queue FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE siem_export_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_receipts_tenant_policy ON siem_export_receipts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE siem_export_dlq ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_dlq FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_dlq_tenant_policy ON siem_export_dlq FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
