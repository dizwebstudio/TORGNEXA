BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE privacy_subject_requests (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  request_id text NOT NULL,
  request_type text NOT NULL CHECK(request_type IN ('access','export','correction','deletion','restriction')),
  subject_kind text NOT NULL,
  subject_opaque_id text NOT NULL,
  correction_artifact_ref text NOT NULL DEFAULT '',
  status text NOT NULL CHECK(status IN ('pending','running','blocked','completed')),
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,request_id),
  CHECK((request_type='correction' AND correction_artifact_ref<>'') OR (request_type<>'correction' AND correction_artifact_ref='')),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_legal_holds (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  hold_id text NOT NULL,
  selector_kind text NOT NULL CHECK(selector_kind IN ('tenant','subject','purpose_class')),
  subject_kind text NOT NULL DEFAULT '',
  subject_opaque_id text NOT NULL DEFAULT '',
  purpose_key text NOT NULL DEFAULT '',
  data_class text NOT NULL DEFAULT '',
  reason_ref text NOT NULL,
  expires_at timestamptz,
  released_at timestamptz,
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,hold_id),
  CHECK(expires_at IS NULL OR expires_at > created_at),
  CHECK(
    (selector_kind='tenant' AND subject_kind='' AND subject_opaque_id='' AND purpose_key='' AND data_class='') OR
    (selector_kind='subject' AND subject_kind<>'' AND subject_opaque_id<>'' AND purpose_key='' AND data_class='') OR
    (selector_kind='purpose_class' AND subject_kind='' AND subject_opaque_id='' AND purpose_key<>'' AND data_class IN ('public','internal','confidential','personal','sensitive_operational','secret'))
  ),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_execution_jobs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  workflow_kind text NOT NULL CHECK(workflow_kind IN ('subject_request','retention_expiry','tenant_deletion')),
  request_id text NOT NULL DEFAULT '',
  subject_kind text NOT NULL DEFAULT '',
  subject_opaque_id text NOT NULL DEFAULT '',
  purpose_key text NOT NULL DEFAULT '',
  data_class text NOT NULL DEFAULT '',
  disposition text NOT NULL DEFAULT '',
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  hold_permitted boolean NOT NULL,
  status text NOT NULL CHECK(status IN ('pending','running','blocked','completed')),
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,job_id),
  CHECK(
    (workflow_kind='subject_request' AND request_id<>'' AND subject_kind<>'' AND subject_opaque_id<>'' AND purpose_key='' AND data_class='' AND disposition='') OR
    (workflow_kind='retention_expiry' AND request_id='' AND subject_kind='' AND subject_opaque_id='' AND purpose_key<>'' AND data_class IN ('public','internal','confidential','personal','sensitive_operational','secret') AND disposition IN ('delete','anonymize','archive_then_delete')) OR
    (workflow_kind='tenant_deletion' AND request_id='' AND subject_kind='' AND subject_opaque_id='' AND purpose_key='' AND data_class='' AND disposition='' AND action='tenant_delete')
  ),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_execution_targets (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  store_name text NOT NULL,
  store_class text NOT NULL CHECK(store_class IN ('authoritative','derived','object')),
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  cursor text NOT NULL DEFAULT '',
  status text NOT NULL CHECK(status IN ('pending','running','completed')),
  processed bigint NOT NULL CHECK(processed >= 0),
  last_digest text NOT NULL DEFAULT '',
  artifact_ref text NOT NULL DEFAULT '',
  version bigint NOT NULL CHECK(version > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,job_id,store_name),
  FOREIGN KEY(organization_id,workspace_id,job_id) REFERENCES privacy_execution_jobs(organization_id,workspace_id,job_id)
);

CREATE TABLE privacy_execution_evidence (
  evidence_id bigserial PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  store_name text NOT NULL,
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  cursor_before text NOT NULL DEFAULT '',
  cursor_after text NOT NULL DEFAULT '',
  processed bigint NOT NULL CHECK(processed >= 0),
  digest text NOT NULL DEFAULT '',
  artifact_ref text NOT NULL DEFAULT '',
  done boolean NOT NULL,
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY(organization_id,workspace_id,job_id) REFERENCES privacy_execution_jobs(organization_id,workspace_id,job_id)
);

CREATE FUNCTION privacy_execution_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''privacy execution evidence is append-only''; END';
CREATE TRIGGER privacy_execution_evidence_append_only_guard BEFORE UPDATE OR DELETE ON privacy_execution_evidence FOR EACH ROW EXECUTE FUNCTION privacy_execution_evidence_append_only();

CREATE FUNCTION privacy_legal_hold_release_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF OLD.organization_id<>NEW.organization_id OR OLD.workspace_id<>NEW.workspace_id OR OLD.hold_id<>NEW.hold_id OR OLD.selector_kind<>NEW.selector_kind OR OLD.subject_kind<>NEW.subject_kind OR OLD.subject_opaque_id<>NEW.subject_opaque_id OR OLD.purpose_key<>NEW.purpose_key OR OLD.data_class<>NEW.data_class OR OLD.reason_ref<>NEW.reason_ref OR OLD.expires_at IS DISTINCT FROM NEW.expires_at OR OLD.created_at<>NEW.created_at OR OLD.released_at IS NOT NULL OR NEW.released_at IS NULL OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION ''privacy legal hold is immutable except release''; END IF; RETURN NEW; END';
CREATE TRIGGER privacy_legal_hold_release_only_guard BEFORE UPDATE ON privacy_legal_holds FOR EACH ROW EXECUTE FUNCTION privacy_legal_hold_release_only();

ALTER TABLE privacy_subject_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_subject_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_subject_requests_tenant_policy ON privacy_subject_requests FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_legal_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_legal_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_legal_holds_tenant_policy ON privacy_legal_holds FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_jobs_tenant_policy ON privacy_execution_jobs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_targets_tenant_policy ON privacy_execution_targets FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_evidence_tenant_policy ON privacy_execution_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
