BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 169: bounded, rebuildable assistant metadata. Raw questions,
-- provider/model payloads and credentials are deliberately not persisted.
CREATE TABLE assistant_sessions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  actor_id text NOT NULL,
  title text NOT NULL DEFAULT '',
  locale text NOT NULL DEFAULT 'ru-RU',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT assistant_session_ref_chk CHECK(id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(title) <= 120 AND title=btrim(title) AND char_length(locale) BETWEEN 2 AND 16 AND version >= 1 AND updated_at >= created_at)
);
CREATE INDEX assistant_sessions_actor_idx ON assistant_sessions(organization_id,workspace_id,actor_id,updated_at DESC,id DESC);

CREATE TABLE assistant_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  session_id text NOT NULL,
  actor_id text NOT NULL,
  state text NOT NULL,
  intent text NOT NULL,
  context_digest char(64) NOT NULL,
  answer jsonb,
  answer_digest char(64),
  error_code text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,session_id) REFERENCES assistant_sessions(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT assistant_run_ref_chk CHECK(id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND session_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND context_digest ~ '^[0-9a-f]{64}$' AND (answer_digest IS NULL OR answer_digest ~ '^[0-9a-f]{64}$') AND version >= 1 AND updated_at >= created_at),
  CONSTRAINT assistant_run_state_chk CHECK(state IN ('queued','retrieving_context','awaiting_model','streaming','awaiting_approval','action_queued','completed','partial','stale','blocked','provider_unavailable','cancelled','failed') AND intent ~ '^[a-z][a-z0-9_]{0,63}$' AND error_code ~ '^[a-z0-9._-]{0,95}$' AND (answer IS NULL OR (jsonb_typeof(answer)='object' AND pg_column_size(answer) <= 131072)))
);
CREATE INDEX assistant_runs_session_idx ON assistant_runs(organization_id,workspace_id,session_id,created_at DESC,id DESC);
CREATE INDEX assistant_runs_actor_state_idx ON assistant_runs(organization_id,workspace_id,actor_id,state,updated_at DESC,id DESC);

CREATE TABLE assistant_action_previews (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  id text NOT NULL,
  run_id text NOT NULL,
  actor_id text NOT NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id text NOT NULL,
  expected_version bigint NOT NULL,
  risk text NOT NULL,
  required_permission text NOT NULL,
  preview_digest char(64) NOT NULL,
  evidence_digest char(64) NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES assistant_runs(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT assistant_preview_ref_chk CHECK(id ~ '^ap:[0-9a-f]{26}$' AND run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND action ~ '^[a-z][a-z0-9._-]{0,95}$' AND resource_type ~ '^[a-z][a-z0-9._-]{0,95}$' AND resource_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND expected_version >= 1 AND risk IN ('read','safe_write','sensitive_write','prohibited') AND required_permission ~ '^[a-z][a-z0-9._-]{0,95}$' AND preview_digest ~ '^[0-9a-f]{64}$' AND evidence_digest ~ '^[0-9a-f]{64}$' AND status IN ('pending','approved','rejected','expired','conflict') AND expires_at > created_at)
);
CREATE INDEX assistant_previews_actor_idx ON assistant_action_previews(organization_id,workspace_id,actor_id,status,expires_at);

CREATE TABLE assistant_feedback (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  actor_id text NOT NULL,
  run_id text NOT NULL,
  kind text NOT NULL,
  reason_code text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,actor_id,run_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES assistant_runs(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT assistant_feedback_ref_chk CHECK(actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND kind IN ('useful','not_useful','incorrect') AND reason_code ~ '^[a-z0-9._-]{0,95}$')
);

ALTER TABLE assistant_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE assistant_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE assistant_action_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_action_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE assistant_feedback ENABLE ROW LEVEL SECURITY;
ALTER TABLE assistant_feedback FORCE ROW LEVEL SECURITY;

CREATE POLICY assistant_sessions_tenant_all ON assistant_sessions FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY assistant_runs_tenant_all ON assistant_runs FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY assistant_previews_tenant_all ON assistant_action_previews FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY assistant_feedback_tenant_all ON assistant_feedback FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE assistant_sessions IS 'Task 169 bounded tenant/actor-scoped assistant sessions; no raw prompt history.';
COMMENT ON TABLE assistant_runs IS 'Task 169 normalized run and answer metadata; source evidence remains authoritative.';
COMMENT ON TABLE assistant_action_previews IS 'Task 169 typed non-executing previews; approval/domain owners remain authoritative.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
