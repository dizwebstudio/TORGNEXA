BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE ai_agent_policies (
  id text NOT NULL CHECK(id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL CHECK(version >= 1),
  agent_id text NOT NULL CHECK(agent_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  integration_id text NOT NULL CHECK(integration_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  rules jsonb NOT NULL CHECK(jsonb_typeof(rules)='array' AND jsonb_array_length(rules) BETWEEN 1 AND 256),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  changed_by text NOT NULL CHECK(char_length(changed_by) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 256),
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_policies_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE(organization_id,workspace_id,agent_id,integration_id,version),
  CONSTRAINT ai_agent_policy_dates CHECK(effective_until IS NULL OR effective_until > effective_from)
);
CREATE INDEX ai_agent_policy_resolve_idx ON ai_agent_policies(organization_id,workspace_id,agent_id,integration_id,effective_from,version DESC);

CREATE FUNCTION ai_agent_policy_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint; stable_id text;
BEGIN
  SELECT COALESCE(max(version),0)+1,min(id) INTO expected,stable_id FROM ai_agent_policies WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND agent_id=NEW.agent_id AND integration_id=NEW.integration_id;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''ai agent policy version invalid''; END IF;
  IF stable_id IS NOT NULL AND stable_id<>NEW.id THEN RAISE EXCEPTION ''ai agent policy identity must remain stable''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_policy_insert_guard BEFORE INSERT ON ai_agent_policies FOR EACH ROW EXECUTE FUNCTION ai_agent_policy_insert_guard();

CREATE FUNCTION ai_agent_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''ai agent governance evidence is append-only''; END';
CREATE TRIGGER ai_agent_policies_append_only BEFORE UPDATE OR DELETE ON ai_agent_policies FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

CREATE TABLE ai_agent_kill_switches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  scope_kind text NOT NULL CHECK(scope_kind IN ('tenant','agent','integration')),
  subject_id text NOT NULL CHECK(char_length(subject_id) BETWEEN 1 AND 160),
  version bigint NOT NULL CHECK(version >= 1),
  disabled boolean NOT NULL,
  changed_by text NOT NULL CHECK(char_length(changed_by) BETWEEN 1 AND 256),
  reason text NOT NULL DEFAULT '' CHECK(char_length(reason) <= 256),
  changed_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,scope_kind,subject_id,version),
  CONSTRAINT ai_agent_kill_switches_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_kill_subject CHECK((scope_kind='tenant' AND subject_id='*') OR (scope_kind<>'tenant' AND subject_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'))
);
CREATE INDEX ai_agent_kill_resolve_idx ON ai_agent_kill_switches(organization_id,workspace_id,scope_kind,subject_id,version DESC);
CREATE FUNCTION ai_agent_kill_insert_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE expected bigint;
BEGIN
  SELECT COALESCE(max(version),0)+1 INTO expected FROM ai_agent_kill_switches WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND scope_kind=NEW.scope_kind AND subject_id=NEW.subject_id;
  IF NEW.version<>expected THEN RAISE EXCEPTION ''ai agent kill-switch version invalid''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_kill_insert_guard BEFORE INSERT ON ai_agent_kill_switches FOR EACH ROW EXECUTE FUNCTION ai_agent_kill_insert_guard();
CREATE TRIGGER ai_agent_kill_append_only BEFORE UPDATE OR DELETE ON ai_agent_kill_switches FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

CREATE TABLE ai_agent_call_counters (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  agent_id text NOT NULL,
  integration_id text NOT NULL,
  tool text NOT NULL CHECK(tool ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  used bigint NOT NULL DEFAULT 0 CHECK(used >= 0),
  max_calls_snapshot bigint NOT NULL CHECK(max_calls_snapshot > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,policy_id,policy_version,agent_id,integration_id,tool,window_start),
  CONSTRAINT ai_agent_call_counters_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_call_counters_policy_fk FOREIGN KEY(organization_id,workspace_id,policy_id,policy_version) REFERENCES ai_agent_policies(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_counter_window CHECK(window_end > window_start),
  CONSTRAINT ai_agent_counter_limit CHECK(used <= max_calls_snapshot)
);
CREATE INDEX ai_agent_call_counter_current_idx ON ai_agent_call_counters(organization_id,workspace_id,agent_id,integration_id,tool,window_end);
CREATE FUNCTION ai_agent_counter_guard() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
  IF TG_OP=''INSERT'' THEN
    IF NEW.used<>0 THEN RAISE EXCEPTION ''ai agent call counter must start at zero''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.policy_version IS DISTINCT FROM OLD.policy_version OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.integration_id IS DISTINCT FROM OLD.integration_id OR NEW.tool IS DISTINCT FROM OLD.tool OR NEW.window_start IS DISTINCT FROM OLD.window_start OR NEW.window_end IS DISTINCT FROM OLD.window_end OR NEW.max_calls_snapshot IS DISTINCT FROM OLD.max_calls_snapshot THEN RAISE EXCEPTION ''ai agent call counter identity immutable''; END IF;
  IF NEW.used < OLD.used OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION ''ai agent call counter cannot move backwards''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_agent_counter_guard BEFORE INSERT OR UPDATE ON ai_agent_call_counters FOR EACH ROW EXECUTE FUNCTION ai_agent_counter_guard();

CREATE TABLE ai_agent_call_usage (
  invocation_id text NOT NULL CHECK(invocation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$'),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK(policy_version >= 1),
  agent_id text NOT NULL,
  integration_id text NOT NULL,
  tool text NOT NULL,
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  max_calls_snapshot bigint NOT NULL CHECK(max_calls_snapshot > 0),
  allowed boolean NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,invocation_id),
  CONSTRAINT ai_agent_call_usage_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT ai_agent_call_usage_policy_fk FOREIGN KEY(organization_id,workspace_id,policy_id,policy_version) REFERENCES ai_agent_policies(organization_id,workspace_id,id,version),
  CONSTRAINT ai_agent_call_usage_window CHECK(window_end > window_start AND occurred_at >= window_start AND occurred_at < window_end)
);
CREATE INDEX ai_agent_call_usage_tool_idx ON ai_agent_call_usage(organization_id,workspace_id,agent_id,integration_id,tool,window_start,occurred_at,invocation_id);
CREATE TRIGGER ai_agent_call_usage_append_only BEFORE UPDATE OR DELETE ON ai_agent_call_usage FOR EACH ROW EXECUTE FUNCTION ai_agent_append_only();

ALTER TABLE ai_agent_policies ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_kill_switches ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_kill_switches FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_call_counters ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_call_counters FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_agent_call_usage ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_agent_call_usage FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_agent_policies_select ON ai_agent_policies FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_policies_insert ON ai_agent_policies FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_kill_select ON ai_agent_kill_switches FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_kill_insert ON ai_agent_kill_switches FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_select ON ai_agent_call_counters FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_insert ON ai_agent_call_counters FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_counters_update ON ai_agent_call_counters FOR UPDATE USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_usage_select ON ai_agent_call_usage FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_agent_usage_insert ON ai_agent_call_usage FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION ai_agent_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''ai agent governance evidence cannot be hard-deleted''; END';
CREATE TRIGGER ai_agent_policies_no_clear BEFORE TRUNCATE ON ai_agent_policies FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_kill_no_clear BEFORE TRUNCATE ON ai_agent_kill_switches FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_counters_no_delete BEFORE DELETE OR TRUNCATE ON ai_agent_call_counters FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();
CREATE TRIGGER ai_agent_usage_no_clear BEFORE TRUNCATE ON ai_agent_call_usage FOR EACH STATEMENT EXECUTE FUNCTION ai_agent_no_delete();

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
