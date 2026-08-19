BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE reconciliation_runs (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  mode text NOT NULL,
  trigger_ref text,
  status text NOT NULL,
  cursor text NOT NULL DEFAULT '',
  scanned_count bigint NOT NULL DEFAULT 0,
  drift_count bigint NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  started_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  CONSTRAINT reconciliation_runs_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_runs_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_runs_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_runs_mode_chk CHECK (mode IN ('incremental','scheduled_full','on_demand')),
  CONSTRAINT reconciliation_runs_status_chk CHECK (status IN ('running','interrupted','completed')),
  CONSTRAINT reconciliation_runs_counts_chk CHECK (scanned_count >= 0 AND drift_count >= 0 AND version >= 1),
  CONSTRAINT reconciliation_runs_cursor_chk CHECK (length(cursor) <= 1024 AND cursor !~ '[[:cntrl:]]'),
  CONSTRAINT reconciliation_runs_trigger_chk CHECK (trigger_ref IS NULL OR (length(trigger_ref) BETWEEN 1 AND 128 AND trigger_ref !~ '[[:cntrl:]]')),
  CONSTRAINT reconciliation_runs_time_chk CHECK (updated_at >= started_at AND (completed_at IS NULL OR completed_at >= started_at)),
  CONSTRAINT reconciliation_runs_completion_chk CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);

CREATE TABLE reconciliation_drifts (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  policy_id text NOT NULL,
  kind text NOT NULL,
  local_entity_id text,
  remote_id text,
  local_fingerprint text,
  remote_fingerprint text,
  local_status text,
  remote_status text,
  local_version bigint NOT NULL DEFAULT 0,
  remote_revision text,
  mapping_local_count integer NOT NULL DEFAULT 0,
  mapping_remote_count integer NOT NULL DEFAULT 0,
  detected_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'open',
  recommended_action text NOT NULL DEFAULT 'none',
  version bigint NOT NULL DEFAULT 1,
  resolved_at timestamptz,
  CONSTRAINT reconciliation_drifts_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_drifts_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_run_fk FOREIGN KEY (run_id, organization_id, workspace_id)
    REFERENCES reconciliation_runs (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_drifts_kind_chk CHECK (kind IN ('content_drift','missing_mapping','orphan_mapping','duplicate_mapping','status_mismatch','stale_connector')),
  CONSTRAINT reconciliation_drifts_status_chk CHECK (status IN ('open','auto_fixed','notified','approval_pending','ignored')),
  CONSTRAINT reconciliation_drifts_action_chk CHECK (recommended_action IN ('none','auto_fix','notify','approval','ignore')),
  CONSTRAINT reconciliation_drifts_counts_chk CHECK (local_version >= 0 AND mapping_local_count BETWEEN 0 AND 1000 AND mapping_remote_count BETWEEN 0 AND 1000 AND version >= 1),
  CONSTRAINT reconciliation_drifts_local_fp_chk CHECK (local_fingerprint IS NULL OR local_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT reconciliation_drifts_remote_fp_chk CHECK (remote_fingerprint IS NULL OR remote_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT reconciliation_drifts_remote_revision_chk CHECK (remote_revision IS NULL OR (length(remote_revision) BETWEEN 1 AND 256 AND remote_revision !~ '[[:cntrl:]]')),
  CONSTRAINT reconciliation_drifts_resolution_chk CHECK ((status = 'open') = (resolved_at IS NULL)),
  CONSTRAINT reconciliation_drifts_resolution_time_chk CHECK (resolved_at IS NULL OR resolved_at >= detected_at)
);

CREATE TABLE reconciliation_actions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  drift_id text NOT NULL,
  action text NOT NULL,
  idempotency_key text NOT NULL,
  result text NOT NULL,
  error_code text,
  created_at timestamptz NOT NULL,
  CONSTRAINT reconciliation_actions_pkey PRIMARY KEY (id),
  CONSTRAINT reconciliation_actions_drift_fk FOREIGN KEY (drift_id, organization_id, workspace_id)
    REFERENCES reconciliation_drifts (id, organization_id, workspace_id),
  CONSTRAINT reconciliation_actions_action_chk CHECK (action IN ('auto_fix','notify','approval','ignore')),
  CONSTRAINT reconciliation_actions_result_chk CHECK (result IN ('succeeded','failed')),
  CONSTRAINT reconciliation_actions_error_chk CHECK ((result = 'succeeded' AND error_code IS NULL) OR (result = 'failed' AND error_code ~ '^[a-z][a-z0-9_]{0,63}$')),
  CONSTRAINT reconciliation_actions_idempotency_chk CHECK (length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key !~ '[[:cntrl:]]')
);

CREATE INDEX reconciliation_runs_policy_status_idx ON reconciliation_runs (organization_id, workspace_id, policy_id, status, updated_at, id);
CREATE INDEX reconciliation_drifts_run_kind_idx ON reconciliation_drifts (organization_id, workspace_id, run_id, kind, status, detected_at, id);
CREATE INDEX reconciliation_drifts_open_idx ON reconciliation_drifts (organization_id, workspace_id, policy_id, status, detected_at, id) WHERE status = 'open';
CREATE INDEX reconciliation_actions_drift_idx ON reconciliation_actions (organization_id, workspace_id, drift_id, created_at, id);

ALTER TABLE reconciliation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_drifts ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_drifts FORCE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_actions FORCE ROW LEVEL SECURITY;

CREATE POLICY reconciliation_runs_tenant_all ON reconciliation_runs
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY reconciliation_drifts_tenant_all ON reconciliation_drifts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY reconciliation_actions_tenant_all ON reconciliation_actions
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

CREATE FUNCTION reconciliation_run_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''running'' OR NEW.scanned_count <> 0 OR NEW.drift_count <> 0 OR NEW.completed_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run initial state is invalid'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.mode IS DISTINCT FROM OLD.mode OR NEW.trigger_ref IS DISTINCT FROM OLD.trigger_ref OR NEW.started_at IS DISTINCT FROM OLD.started_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR NEW.scanned_count < OLD.scanned_count OR NEW.drift_count < OLD.drift_count THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation run progression is invalid'';
  END IF;
  IF OLD.status = ''completed'' THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''completed reconciliation run is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER reconciliation_run_guard_insert BEFORE INSERT ON reconciliation_runs FOR EACH ROW EXECUTE FUNCTION reconciliation_run_guard();
CREATE TRIGGER reconciliation_run_guard_update BEFORE UPDATE ON reconciliation_runs FOR EACH ROW EXECUTE FUNCTION reconciliation_run_guard();

CREATE FUNCTION reconciliation_drift_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''open'' OR NEW.resolved_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift initial state is invalid'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.run_id IS DISTINCT FROM OLD.run_id OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.remote_id IS DISTINCT FROM OLD.remote_id
     OR NEW.local_fingerprint IS DISTINCT FROM OLD.local_fingerprint OR NEW.remote_fingerprint IS DISTINCT FROM OLD.remote_fingerprint
     OR NEW.local_status IS DISTINCT FROM OLD.local_status OR NEW.remote_status IS DISTINCT FROM OLD.remote_status
     OR NEW.local_version IS DISTINCT FROM OLD.local_version OR NEW.remote_revision IS DISTINCT FROM OLD.remote_revision
     OR NEW.mapping_local_count IS DISTINCT FROM OLD.mapping_local_count OR NEW.mapping_remote_count IS DISTINCT FROM OLD.mapping_remote_count
     OR NEW.detected_at IS DISTINCT FROM OLD.detected_at OR NEW.recommended_action IS DISTINCT FROM OLD.recommended_action THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift evidence is immutable'';
  END IF;
  IF OLD.status <> ''open'' OR NEW.status = ''open'' OR NEW.version <> OLD.version + 1 OR NEW.resolved_at IS NULL THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation drift transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER reconciliation_drift_guard_insert BEFORE INSERT ON reconciliation_drifts FOR EACH ROW EXECUTE FUNCTION reconciliation_drift_guard();
CREATE TRIGGER reconciliation_drift_guard_update BEFORE UPDATE ON reconciliation_drifts FOR EACH ROW EXECUTE FUNCTION reconciliation_drift_guard();

CREATE FUNCTION reconciliation_action_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''reconciliation action history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER reconciliation_actions_no_update BEFORE UPDATE OR DELETE ON reconciliation_actions FOR EACH ROW EXECUTE FUNCTION reconciliation_action_reject_mutation();
CREATE TRIGGER reconciliation_actions_no_clear BEFORE TRUNCATE ON reconciliation_actions FOR EACH STATEMENT EXECUTE FUNCTION reconciliation_action_reject_mutation();

REVOKE DELETE, TRUNCATE ON reconciliation_runs, reconciliation_drifts FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON reconciliation_actions FROM PUBLIC;

COMMENT ON TABLE reconciliation_runs IS 'Task-014 resumable incremental/scheduled-full/on-demand reconciliation progress; no remote payloads or raw errors.';
COMMENT ON TABLE reconciliation_drifts IS 'Task-014 bounded immutable drift evidence with one-way resolution state transitions.';
COMMENT ON TABLE reconciliation_actions IS 'Task-014 append-only remediation attempt receipts; external effects use deterministic idempotency keys.';

INSERT INTO migration_history (
  version, name, file_name, phase, risk, checksum_sha256,
  application_version, execution_id, duration_ms
) VALUES (
  current_setting('torgnexa.migration_version')::integer,
  current_setting('torgnexa.migration_name'),
  current_setting('torgnexa.migration_file'),
  current_setting('torgnexa.migration_phase'),
  current_setting('torgnexa.migration_risk'),
  current_setting('torgnexa.migration_checksum'),
  current_setting('torgnexa.application_version'),
  current_setting('torgnexa.migration_execution_id'),
  current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
