BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 163: bounded, provider-neutral workflow control plane.  Definitions
-- and progress are tenant-owned PostgreSQL truth; Kafka carries only trigger
-- notifications and never stores scheduler or execution state.
CREATE TABLE workflows (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  current_version bigint NOT NULL DEFAULT 1,
  version bigint NOT NULL DEFAULT 1,
  trigger_kind text NOT NULL,
  trigger_event_type text NOT NULL DEFAULT '',
  trigger_interval_minutes integer NOT NULL DEFAULT 0,
  trigger_enabled boolean NOT NULL DEFAULT false,
  next_run_at timestamptz,
  schedule_lease_token text,
  schedule_lease_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  UNIQUE (organization_id, workspace_id, id, current_version),
  CONSTRAINT workflows_name_chk CHECK (name=btrim(name) AND char_length(name) BETWEEN 1 AND 120 AND name !~ '[[:cntrl:]]'),
  CONSTRAINT workflows_description_chk CHECK (char_length(description)<=4000 AND description !~ '[[:cntrl:]]'),
  CONSTRAINT workflows_status_chk CHECK (status IN ('draft','published','paused','archived')),
  CONSTRAINT workflows_version_chk CHECK (version>=1 AND current_version>=1),
  CONSTRAINT workflows_trigger_kind_chk CHECK (trigger_kind IN ('event','schedule')),
  CONSTRAINT workflows_event_type_chk CHECK (trigger_event_type='' OR trigger_event_type ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.v[1-9][0-9]{0,2}$'),
  CONSTRAINT workflows_interval_chk CHECK ((trigger_kind='event' AND trigger_interval_minutes=0 AND next_run_at IS NULL AND NOT trigger_enabled) OR (trigger_kind='schedule' AND trigger_interval_minutes BETWEEN 1 AND 10080 AND ((trigger_enabled AND next_run_at IS NOT NULL) OR (NOT trigger_enabled AND next_run_at IS NULL)))),
  CONSTRAINT workflows_schedule_lease_chk CHECK ((schedule_lease_token IS NULL AND schedule_lease_until IS NULL) OR (schedule_lease_token IS NOT NULL AND schedule_lease_until IS NOT NULL)),
  CONSTRAINT workflows_time_chk CHECK (updated_at>=created_at)
);

CREATE TABLE workflow_versions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  workflow_id text NOT NULL,
  version bigint NOT NULL,
  definition jsonb NOT NULL,
  plan_node_ids jsonb NOT NULL,
  plan_digest text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at timestamptz,
  PRIMARY KEY (organization_id, workspace_id, id),
  UNIQUE (organization_id, workspace_id, workflow_id, version),
  FOREIGN KEY (organization_id, workspace_id, workflow_id) REFERENCES workflows (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT workflow_versions_version_chk CHECK (version>=1),
  CONSTRAINT workflow_versions_digest_chk CHECK (plan_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT workflow_versions_definition_size_chk CHECK (pg_column_size(definition)<=16384),
  CONSTRAINT workflow_versions_time_chk CHECK (published_at IS NULL OR published_at>=created_at)
);

CREATE TABLE workflow_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  workflow_id text NOT NULL,
  workflow_version bigint NOT NULL,
  trigger_kind text NOT NULL,
  trigger_ref text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL,
  input_digest text NOT NULL,
  status text NOT NULL DEFAULT 'queued',
  attempt_count integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  started_at timestamptz,
  completed_at timestamptz,
  last_error_code text NOT NULL DEFAULT '',
  lease_token text,
  lease_until timestamptz,
  version bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (organization_id, workspace_id, id),
  UNIQUE (organization_id, workspace_id, workflow_id, idempotency_key),
  FOREIGN KEY (organization_id, workspace_id, workflow_id, workflow_version) REFERENCES workflow_versions (organization_id, workspace_id, workflow_id, version) ON DELETE RESTRICT,
  CONSTRAINT workflow_runs_trigger_chk CHECK (trigger_kind IN ('event','schedule')),
  CONSTRAINT workflow_runs_idempotency_chk CHECK (char_length(idempotency_key) BETWEEN 1 AND 256 AND idempotency_key !~ '[[:cntrl:]]'),
  CONSTRAINT workflow_runs_digest_chk CHECK (input_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT workflow_runs_status_chk CHECK (status IN ('queued','running','waiting_approval','waiting_retry','completed','failed','cancelled')),
  CONSTRAINT workflow_runs_attempt_chk CHECK (attempt_count BETWEEN 0 AND 64),
  CONSTRAINT workflow_runs_error_chk CHECK (last_error_code='' OR last_error_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT workflow_runs_time_chk CHECK ((completed_at IS NULL OR started_at IS NULL OR completed_at>=started_at) AND ((lease_token IS NULL AND lease_until IS NULL) OR (lease_token IS NOT NULL AND lease_until IS NOT NULL)))
);

CREATE TABLE workflow_step_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  run_id text NOT NULL,
  node_id text NOT NULL,
  status text NOT NULL DEFAULT 'queued',
  attempt_count integer NOT NULL DEFAULT 0,
  output_digest text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  started_at timestamptz,
  completed_at timestamptz,
  version bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (organization_id, workspace_id, id),
  UNIQUE (organization_id, workspace_id, run_id, node_id),
  FOREIGN KEY (organization_id, workspace_id, run_id) REFERENCES workflow_runs (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT workflow_step_runs_status_chk CHECK (status IN ('queued','running','waiting_approval','waiting_retry','completed','failed','skipped')),
  CONSTRAINT workflow_step_runs_attempt_chk CHECK (attempt_count BETWEEN 0 AND 64),
  CONSTRAINT workflow_step_runs_digest_chk CHECK (output_digest='' OR output_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT workflow_step_runs_error_chk CHECK (error_code='' OR error_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT workflow_step_runs_time_chk CHECK (completed_at IS NULL OR started_at IS NULL OR completed_at>=started_at)
);

CREATE TABLE workflow_step_evidence (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  run_id text NOT NULL,
  node_id text NOT NULL,
  attempt integer NOT NULL,
  outcome text NOT NULL,
  output_digest text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  FOREIGN KEY (organization_id, workspace_id, run_id) REFERENCES workflow_runs (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT workflow_step_evidence_attempt_chk CHECK (attempt BETWEEN 1 AND 64),
  CONSTRAINT workflow_step_evidence_outcome_chk CHECK (outcome IN ('completed','failed','skipped','waiting_approval','waiting_retry')),
  CONSTRAINT workflow_step_evidence_digest_chk CHECK (output_digest='' OR output_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT workflow_step_evidence_error_chk CHECK (error_code='' OR error_code ~ '^[a-z][a-z0-9._-]{0,63}$')
);

CREATE TABLE workflow_event_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  workflow_id text NOT NULL,
  workflow_version bigint NOT NULL,
  run_id text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, event_id, workflow_id, workflow_version),
  FOREIGN KEY (organization_id, workspace_id, run_id) REFERENCES workflow_runs (organization_id, workspace_id, id) ON DELETE RESTRICT
);

CREATE INDEX workflows_due_schedule_idx ON workflows (organization_id, workspace_id, next_run_at, id) WHERE status='published' AND trigger_kind='schedule' AND trigger_enabled;
CREATE INDEX workflow_versions_active_idx ON workflow_versions (organization_id, workspace_id, workflow_id, version DESC);
CREATE INDEX workflow_runs_operator_idx ON workflow_runs (organization_id, workspace_id, status, available_at DESC, id DESC);
CREATE INDEX workflow_runs_workflow_idx ON workflow_runs (organization_id, workspace_id, workflow_id, started_at DESC, id DESC);
CREATE INDEX workflow_step_evidence_run_idx ON workflow_step_evidence (organization_id, workspace_id, run_id, observed_at, id);

ALTER TABLE workflows ENABLE ROW LEVEL SECURITY; ALTER TABLE workflows FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_versions ENABLE ROW LEVEL SECURITY; ALTER TABLE workflow_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_runs ENABLE ROW LEVEL SECURITY; ALTER TABLE workflow_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_step_runs ENABLE ROW LEVEL SECURITY; ALTER TABLE workflow_step_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_step_evidence ENABLE ROW LEVEL SECURITY; ALTER TABLE workflow_step_evidence FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_event_receipts ENABLE ROW LEVEL SECURITY; ALTER TABLE workflow_event_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY workflows_tenant_all ON workflows USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY workflow_versions_tenant_all ON workflow_versions USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY workflow_runs_tenant_all ON workflow_runs USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY workflow_step_runs_tenant_all ON workflow_step_runs USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY workflow_step_evidence_tenant_all ON workflow_step_evidence USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY workflow_event_receipts_tenant_all ON workflow_event_receipts USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON workflows,workflow_runs,workflow_step_runs FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON workflow_versions,workflow_step_evidence,workflow_event_receipts FROM PUBLIC;

CREATE FUNCTION workflow_head_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 OR NEW.current_version<OLD.current_version OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''workflow head transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER workflows_head_guard BEFORE UPDATE ON workflows FOR EACH ROW EXECUTE FUNCTION workflow_head_guard();

CREATE FUNCTION workflow_immutable_evidence() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''workflow historical evidence is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER workflow_versions_no_update_delete BEFORE UPDATE OR DELETE ON workflow_versions FOR EACH ROW EXECUTE FUNCTION workflow_immutable_evidence();
CREATE TRIGGER workflow_versions_no_clear BEFORE TRUNCATE ON workflow_versions FOR EACH STATEMENT EXECUTE FUNCTION workflow_immutable_evidence();
CREATE TRIGGER workflow_step_evidence_no_update_delete BEFORE UPDATE OR DELETE ON workflow_step_evidence FOR EACH ROW EXECUTE FUNCTION workflow_immutable_evidence();
CREATE TRIGGER workflow_step_evidence_no_clear BEFORE TRUNCATE ON workflow_step_evidence FOR EACH STATEMENT EXECUTE FUNCTION workflow_immutable_evidence();
CREATE TRIGGER workflow_event_receipts_no_update_delete BEFORE UPDATE OR DELETE ON workflow_event_receipts FOR EACH ROW EXECUTE FUNCTION workflow_immutable_evidence();
CREATE TRIGGER workflow_event_receipts_no_clear BEFORE TRUNCATE ON workflow_event_receipts FOR EACH STATEMENT EXECUTE FUNCTION workflow_immutable_evidence();

INSERT INTO migration_history (version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES (current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
