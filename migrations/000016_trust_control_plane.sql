BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE mcp_client_accounts
  ADD COLUMN expires_at timestamptz NOT NULL DEFAULT (clock_timestamp() + interval '90 days'),
  ADD COLUMN rotated_from_id text,
  ADD COLUMN revoked_at timestamptz,
  ADD CONSTRAINT mcp_client_accounts_lifecycle_chk CHECK (
    expires_at > created_at AND
    (rotated_from_id IS NULL OR (rotated_from_id <> id AND char_length(rotated_from_id) BETWEEN 1 AND 64)) AND
    (revoked_at IS NULL OR (revoked_at >= created_at AND enabled = false))
  );

CREATE OR REPLACE FUNCTION mcp_client_accounts_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NOT NEW.enabled OR NEW.revoked_at IS NOT NULL THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid new MCP credential lifecycle''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.token_hash IS DISTINCT FROM OLD.token_hash OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR NEW.rotated_from_id IS DISTINCT FROM OLD.rotated_from_id THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''MCP client account identity/credential is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR (NOT OLD.enabled AND NEW.enabled) OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''MCP client account lifecycle transition is invalid'';
  END IF;
  RETURN NEW;
END';

CREATE TABLE operation_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation text NOT NULL CHECK (operation ~ '^[a-z][a-z0-9_.]{2,95}$'),
  idempotency_key text NOT NULL CHECK (idempotency_key=btrim(idempotency_key) AND char_length(idempotency_key) BETWEEN 1 AND 128),
  request_sha256 bytea NOT NULL CHECK (octet_length(request_sha256)=32),
  state text NOT NULL CHECK (state IN ('pending','completed')),
  resource_type text NOT NULL DEFAULT '' CHECK (char_length(resource_type) <= 80),
  resource_id text NOT NULL DEFAULT '' CHECK (char_length(resource_id) <= 160),
  result jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(result)='object' AND pg_column_size(result) <= 8192),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,operation,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE security_evidence (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  evidence_type text NOT NULL CHECK (evidence_type ~ '^[a-z][a-z0-9_.]{2,95}$'),
  actor_ref text NOT NULL CHECK (actor_ref=btrim(actor_ref) AND char_length(actor_ref) BETWEEN 1 AND 160),
  resource_type text NOT NULL CHECK (resource_type=btrim(resource_type) AND char_length(resource_type) BETWEEN 1 AND 80),
  resource_id text NOT NULL CHECK (resource_id=btrim(resource_id) AND char_length(resource_id) BETWEEN 1 AND 160),
  correlation_id text NOT NULL CHECK (correlation_id=btrim(correlation_id) AND char_length(correlation_id) BETWEEN 1 AND 128),
  decision text NOT NULL CHECK (decision IN ('allowed','denied','succeeded','failed','revoked','rotated')),
  request_sha256 bytea CHECK (request_sha256 IS NULL OR octet_length(request_sha256)=32),
  summary jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(summary)='object' AND pg_column_size(summary) <= 8192),
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE INDEX security_evidence_page_idx ON security_evidence(organization_id,workspace_id,occurred_at DESC,id DESC);

CREATE TABLE mcp_credential_activity (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  account_id text NOT NULL,
  first_used_at timestamptz NOT NULL,
  last_used_at timestamptz NOT NULL,
  use_count bigint NOT NULL CHECK (use_count >= 1),
  PRIMARY KEY (organization_id,workspace_id,account_id),
  FOREIGN KEY (organization_id,workspace_id,account_id) REFERENCES mcp_client_accounts(organization_id,workspace_id,id)
);

CREATE TABLE ai_egress_policy_revisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL CHECK (version >= 1),
  enabled boolean NOT NULL,
  allowed_data_classes jsonb NOT NULL CHECK (jsonb_typeof(allowed_data_classes)='array' AND jsonb_array_length(allowed_data_classes) BETWEEN 1 AND 8),
  allowed_providers jsonb NOT NULL CHECK (jsonb_typeof(allowed_providers)='array' AND jsonb_array_length(allowed_providers) BETWEEN 1 AND 16),
  allowed_models jsonb NOT NULL CHECK (jsonb_typeof(allowed_models)='array' AND jsonb_array_length(allowed_models) BETWEEN 1 AND 32),
  max_prompt_bytes integer NOT NULL CHECK (max_prompt_bytes BETWEEN 1 AND 32000),
  monthly_request_limit integer NOT NULL CHECK (monthly_request_limit BETWEEN 1 AND 1000000),
  actor_ref text NOT NULL CHECK (char_length(actor_ref) BETWEEN 1 AND 160),
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,version),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE ai_egress_usage (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_version bigint NOT NULL,
  account_id text NOT NULL CHECK (char_length(account_id) BETWEEN 1 AND 64),
  provider text NOT NULL CHECK (char_length(provider) BETWEEN 1 AND 63),
  model text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 120),
  phase text NOT NULL CHECK (phase IN ('authorized','outcome')),
  outcome text NOT NULL CHECK (outcome IN ('allowed','denied','succeeded','failed')),
  prompt_bytes integer NOT NULL CHECK (prompt_bytes BETWEEN 0 AND 32000),
  prompt_sha256 bytea NOT NULL CHECK (octet_length(prompt_sha256)=32),
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,policy_version) REFERENCES ai_egress_policy_revisions(organization_id,workspace_id,version)
);
CREATE INDEX ai_egress_usage_month_idx ON ai_egress_usage(organization_id,workspace_id,occurred_at,phase);

CREATE TABLE connector_replay_runs (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_family text NOT NULL CHECK (connector_family ~ '^[a-z][a-z0-9_-]{1,62}$'),
  capability text NOT NULL CHECK (capability ~ '^[a-z][a-z0-9_.-]{2,95}$'),
  fixture_sha256 bytea NOT NULL CHECK (octet_length(fixture_sha256)=32),
  fixture jsonb NOT NULL CHECK (jsonb_typeof(fixture) IN ('object','array') AND pg_column_size(fixture) <= 65536),
  result jsonb NOT NULL CHECK (jsonb_typeof(result)='object' AND pg_column_size(result) <= 65536),
  status text NOT NULL CHECK (status IN ('passed','rejected')),
  actor_ref text NOT NULL CHECK (char_length(actor_ref) BETWEEN 1 AND 160),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE profitability_scenarios (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  name text NOT NULL CHECK (name=btrim(name) AND char_length(name) BETWEEN 1 AND 120),
  algorithm_version text NOT NULL CHECK (algorithm_version ~ '^profitability-v[0-9]+$'),
  input_snapshot jsonb NOT NULL CHECK (jsonb_typeof(input_snapshot)='object' AND pg_column_size(input_snapshot) <= 16384),
  result_snapshot jsonb NOT NULL CHECK (jsonb_typeof(result_snapshot)='object' AND pg_column_size(result_snapshot) <= 16384),
  input_sha256 bytea NOT NULL CHECK (octet_length(input_sha256)=32),
  actor_ref text NOT NULL CHECK (char_length(actor_ref) BETWEEN 1 AND 160),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

ALTER TABLE operation_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE operation_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE security_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE security_evidence FORCE ROW LEVEL SECURITY;
ALTER TABLE mcp_credential_activity ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_credential_activity FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_egress_policy_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_egress_policy_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_egress_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_egress_usage FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_replay_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_replay_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE profitability_scenarios ENABLE ROW LEVEL SECURITY;
ALTER TABLE profitability_scenarios FORCE ROW LEVEL SECURITY;

CREATE POLICY operation_receipts_tenant_all ON operation_receipts USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY security_evidence_tenant_all ON security_evidence USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mcp_credential_activity_tenant_all ON mcp_credential_activity USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_egress_policy_revisions_tenant_all ON ai_egress_policy_revisions USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ai_egress_usage_tenant_all ON ai_egress_usage USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY connector_replay_runs_tenant_all ON connector_replay_runs USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY profitability_scenarios_tenant_all ON profitability_scenarios USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION trust_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''trust evidence is append-only''; END';
CREATE TRIGGER security_evidence_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON security_evidence FOR EACH STATEMENT EXECUTE FUNCTION trust_append_only();
CREATE TRIGGER ai_egress_policy_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON ai_egress_policy_revisions FOR EACH STATEMENT EXECUTE FUNCTION trust_append_only();
CREATE TRIGGER ai_egress_usage_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON ai_egress_usage FOR EACH STATEMENT EXECUTE FUNCTION trust_append_only();
CREATE TRIGGER connector_replay_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON connector_replay_runs FOR EACH STATEMENT EXECUTE FUNCTION trust_append_only();
CREATE TRIGGER profitability_scenario_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON profitability_scenarios FOR EACH STATEMENT EXECUTE FUNCTION trust_append_only();

CREATE FUNCTION operation_receipts_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN IF NEW.state<>''pending'' OR NEW.completed_at IS NOT NULL THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''receipt must start pending''; END IF; RETURN NEW; END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.operation IS DISTINCT FROM OLD.operation OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR NEW.request_sha256 IS DISTINCT FROM OLD.request_sha256 OR OLD.state<>''pending'' OR NEW.state<>''completed'' OR NEW.completed_at IS NULL THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid receipt transition''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER operation_receipts_guard_insert BEFORE INSERT ON operation_receipts FOR EACH ROW EXECUTE FUNCTION operation_receipts_guard();
CREATE TRIGGER operation_receipts_guard_update BEFORE UPDATE ON operation_receipts FOR EACH ROW EXECUTE FUNCTION operation_receipts_guard();

REVOKE DELETE,TRUNCATE ON operation_receipts,mcp_client_accounts,mcp_credential_activity FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON security_evidence,ai_egress_policy_revisions,ai_egress_usage,connector_replay_runs,profitability_scenarios FROM PUBLIC;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
