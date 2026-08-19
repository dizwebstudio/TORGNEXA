BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 010 stabilizes the tenant-owned Connector SDK account metadata without
-- introducing provider credential columns. The legacy `provider` column is the
-- canonical connector manifest id; a future contract migration may rename it,
-- but expand compatibility keeps existing readers/writers intact here.
ALTER TABLE connector_accounts
  ADD COLUMN version bigint NOT NULL DEFAULT 1,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN health_status text NOT NULL DEFAULT 'unknown',
  ADD COLUMN health_reason_code text,
  ADD COLUMN health_checked_at timestamptz;

ALTER TABLE connector_accounts
  ADD CONSTRAINT connector_accounts_family_v1_chk CHECK (
    family IN ('marketplace','classified','social','erp','edo','government','payment','logistics','pickup','fx','notification')
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_provider_v1_chk CHECK (
    provider ~ '^[a-z0-9][a-z0-9-]{0,62}$'
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_status_v1_chk CHECK (
    status IN ('disabled','active','suspended','error')
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_version_v1_chk CHECK (version >= 1) NOT VALID,
  ADD CONSTRAINT connector_accounts_updated_at_v1_chk CHECK (updated_at >= created_at) NOT VALID,
  ADD CONSTRAINT connector_accounts_health_v1_chk CHECK (
    (
      health_status = 'unknown'
      AND health_reason_code IS NULL
      AND health_checked_at IS NULL
    ) OR (
      health_status = 'healthy'
      AND health_reason_code IS NULL
      AND health_checked_at IS NOT NULL
    ) OR (
      health_status IN ('degraded','unavailable')
      AND health_reason_code ~ '^[a-z][a-z0-9._-]{0,63}$'
      AND health_checked_at IS NOT NULL
    )
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_health_time_v1_chk CHECK (
    health_checked_at IS NULL OR health_checked_at >= created_at
  ) NOT VALID;

ALTER TABLE connector_accounts
  VALIDATE CONSTRAINT connector_accounts_family_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_provider_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_status_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_version_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_updated_at_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_health_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_health_time_v1_chk;

CREATE INDEX connector_accounts_manifest_status_idx
  ON connector_accounts (organization_id, workspace_id, provider, status, id);

CREATE INDEX connector_accounts_health_idx
  ON connector_accounts (organization_id, workspace_id, health_status, health_checked_at, id);

REVOKE DELETE, TRUNCATE ON connector_accounts FROM PUBLIC;

CREATE FUNCTION connector_accounts_sdk_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''disabled'' OR NEW.health_status <> ''unknown''
       OR NEW.health_reason_code IS NOT NULL OR NEW.health_checked_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''new connector account must start disabled/unknown at version 1'';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.family IS DISTINCT FROM OLD.family
     OR NEW.provider IS DISTINCT FROM OLD.provider
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account identity is immutable'';
  END IF;

  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account version transition is invalid'';
  END IF;

  IF OLD.status = ''disabled'' AND NEW.status NOT IN (''disabled'',''active'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''active'' AND NEW.status NOT IN (''active'',''disabled'',''suspended'',''error'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''suspended'' AND NEW.status NOT IN (''suspended'',''active'',''disabled'',''error'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''error'' AND NEW.status NOT IN (''error'',''active'',''disabled'',''suspended'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  END IF;

  IF OLD.health_checked_at IS NOT NULL AND NEW.health_checked_at IS NOT NULL
     AND NEW.health_checked_at < OLD.health_checked_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector health timestamp cannot move backwards'';
  END IF;

  RETURN NEW;
END';

CREATE TRIGGER connector_accounts_sdk_guard_insert
  BEFORE INSERT ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_sdk_guard();
CREATE TRIGGER connector_accounts_sdk_guard_update
  BEFORE UPDATE ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_sdk_guard();

CREATE FUNCTION connector_accounts_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account history cannot be hard-deleted'';
  RETURN NULL;
END';

CREATE TRIGGER connector_accounts_no_delete
  BEFORE DELETE ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_reject_delete();
CREATE TRIGGER connector_accounts_no_clear
  BEFORE TRUNCATE ON connector_accounts
  FOR EACH STATEMENT EXECUTE FUNCTION connector_accounts_reject_delete();

COMMENT ON COLUMN connector_accounts.provider IS 'Canonical Connector SDK manifest id. Provider-specific branching in Core is forbidden.';
COMMENT ON COLUMN connector_accounts.secret_reference IS 'Opaque Task-021 secret handle only; plaintext credentials are forbidden.';
COMMENT ON COLUMN connector_accounts.health_reason_code IS 'Normalized safe reason code only. Raw provider errors/responses are forbidden.';
COMMENT ON TABLE connector_accounts IS 'Tenant-scoped Connector SDK account binding with optimistic version and normalized health metadata.';

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
