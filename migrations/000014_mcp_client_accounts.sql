BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Tenant-scoped MCP client accounts: the only credential a machine/AI-agent
-- caller can present to authenticate against POST /mcp. Unlike every other
-- credential table in this repository, the secret here flows inbound (an
-- external agent presents it to TORGNEXA), not outbound to a third party,
-- so it cannot use secrets.SecretProvider/Reference (that boundary is for
-- credentials TORGNEXA sends out). Only a SHA-256 hash of the raw bearer
-- token is ever stored; the raw token is generated and returned exactly
-- once, at creation time, and is never persisted or logged again.
CREATE TABLE mcp_client_accounts (
  id text NOT NULL CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  label text NOT NULL CHECK (label = btrim(label) AND char_length(label) BETWEEN 1 AND 120),
  agent_id text NOT NULL CHECK (agent_id = btrim(agent_id) AND char_length(agent_id) BETWEEN 1 AND 160),
  model_id text NOT NULL CHECK (model_id = btrim(model_id) AND char_length(model_id) BETWEEN 1 AND 160),
  integration_id text NOT NULL CHECK (integration_id = btrim(integration_id) AND char_length(integration_id) BETWEEN 1 AND 160),
  -- jsonb array of permission strings, not a native text[]: no other table
  -- in this repository scans a Postgres array through database/sql, and
  -- jsonb's containment operator lets the CHECK below validate membership
  -- without a subquery (forbidden in a CHECK constraint).
  permissions jsonb NOT NULL CHECK (
    jsonb_typeof(permissions) = 'array'
    AND jsonb_array_length(permissions) BETWEEN 1 AND 4
    AND permissions <@ '["commerce.products.read","commerce.orders.read","party.counterparties.read","commerce.price.change.request"]'::jsonb
  ),
  token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  UNIQUE (token_hash)
);

CREATE INDEX mcp_client_accounts_tenant_idx
  ON mcp_client_accounts (organization_id, workspace_id, enabled);

ALTER TABLE mcp_client_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_client_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY mcp_client_accounts_tenant_all ON mcp_client_accounts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

REVOKE DELETE, TRUNCATE ON mcp_client_accounts FROM PUBLIC;

CREATE FUNCTION mcp_client_accounts_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new MCP client account must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.token_hash IS DISTINCT FROM OLD.token_hash OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''MCP client account identity/credential is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''MCP client account version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER mcp_client_accounts_guard_insert BEFORE INSERT ON mcp_client_accounts FOR EACH ROW EXECUTE FUNCTION mcp_client_accounts_guard();
CREATE TRIGGER mcp_client_accounts_guard_update BEFORE UPDATE ON mcp_client_accounts FOR EACH ROW EXECUTE FUNCTION mcp_client_accounts_guard();

INSERT INTO migration_history (
  version,
  name,
  file_name,
  phase,
  risk,
  checksum_sha256,
  application_version,
  execution_id,
  duration_ms
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
