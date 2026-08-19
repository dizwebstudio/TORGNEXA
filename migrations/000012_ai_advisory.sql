BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Tenant-scoped configured AI provider accounts. Credential material never
-- lives in this table: it is stored through the existing secrets provider
-- (secrets.ClassAIProviderCredential) and only a Reference is kept here.
CREATE TABLE ai_provider_accounts (
  id text NOT NULL CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider text NOT NULL CHECK (provider IN ('openai-compatible', 'gigachat', 'yandexgpt', 'kimi')),
  label text NOT NULL CHECK (label = btrim(label) AND char_length(label) BETWEEN 1 AND 120),
  model text NOT NULL CHECK (model = btrim(model) AND char_length(model) BETWEEN 1 AND 120),
  base_url text NOT NULL DEFAULT '' CHECK (base_url = '' OR (base_url ~ '^https://[^[:space:]]+$' AND char_length(base_url) <= 2039)),
  folder_id text NOT NULL DEFAULT '' CHECK (folder_id = btrim(folder_id) AND char_length(folder_id) <= 120),
  secret_reference text NOT NULL CHECK (char_length(secret_reference) <= 200),
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  UNIQUE (organization_id, workspace_id, provider, label)
);

CREATE INDEX ai_provider_accounts_tenant_idx
  ON ai_provider_accounts (organization_id, workspace_id, enabled, provider);

ALTER TABLE ai_provider_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_provider_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_provider_accounts_tenant_all ON ai_provider_accounts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

REVOKE DELETE, TRUNCATE ON ai_provider_accounts FROM PUBLIC;

CREATE FUNCTION ai_provider_accounts_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new AI provider account must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.provider IS DISTINCT FROM OLD.provider OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''AI provider account identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''AI provider account version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER ai_provider_accounts_guard_insert BEFORE INSERT ON ai_provider_accounts FOR EACH ROW EXECUTE FUNCTION ai_provider_accounts_guard();
CREATE TRIGGER ai_provider_accounts_guard_update BEFORE UPDATE ON ai_provider_accounts FOR EACH ROW EXECUTE FUNCTION ai_provider_accounts_guard();

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
