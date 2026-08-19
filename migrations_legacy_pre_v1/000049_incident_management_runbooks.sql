BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE incidents (organization_id text NOT NULL, workspace_id text NOT NULL, incident_id text NOT NULL, dedupe_key text NOT NULL, runbook_id text NOT NULL, severity text NOT NULL CHECK(severity IN ('p1','p2','p3','p4')), state text NOT NULL CHECK(state IN ('open','acknowledged','mitigated','resolved')), occurrences bigint NOT NULL CHECK(occurrences>0), opened_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,incident_id), UNIQUE(organization_id,workspace_id,dedupe_key,incident_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE incident_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, incident_id text NOT NULL, step_id text NOT NULL, action_ref text NOT NULL DEFAULT '', validation_ref text NOT NULL DEFAULT '', rollback_ref text NOT NULL DEFAULT '', occurred_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id,incident_id) REFERENCES incidents(organization_id,workspace_id,incident_id));
CREATE FUNCTION incident_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''incident_evidence is append-only''; END';
CREATE TRIGGER incident_evidence_append_only_guard BEFORE UPDATE OR DELETE ON incident_evidence FOR EACH ROW EXECUTE FUNCTION incident_evidence_append_only();
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY incidents_tenant_policy ON incidents FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE incident_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY incident_evidence_tenant_policy ON incident_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
