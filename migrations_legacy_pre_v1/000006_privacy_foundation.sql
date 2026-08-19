BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION privacy_classes_valid(classes jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE
AS 'SELECT CASE
  WHEN jsonb_typeof(classes) <> ''array'' THEN false
  ELSE jsonb_array_length(classes) BETWEEN 1 AND 6
    AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements_text(classes) AS value
      WHERE value NOT IN (''public'', ''internal'', ''confidential'', ''personal'', ''sensitive_operational'', ''secret'')
    )
    AND (
      SELECT count(*) = count(DISTINCT value)
      FROM jsonb_array_elements_text(classes) AS value
    )
  END';

CREATE TABLE privacy_purposes (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  purpose_key text NOT NULL,
  description text NOT NULL,
  legal_basis text NOT NULL,
  notice_reference text NOT NULL DEFAULT '',
  consent_reference text NOT NULL DEFAULT '',
  allowed_classes jsonb NOT NULL,
  status text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT privacy_purposes_pkey PRIMARY KEY (organization_id, workspace_id, purpose_key),
  CONSTRAINT privacy_purposes_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT privacy_purposes_key_chk CHECK (purpose_key ~ '^[a-z][a-z0-9_.-]{1,62}$'),
  CONSTRAINT privacy_purposes_description_chk CHECK (description = btrim(description) AND char_length(description) BETWEEN 1 AND 512),
  CONSTRAINT privacy_purposes_legal_basis_chk CHECK (legal_basis IN ('consent','contract','legal_obligation','legitimate_interest','vital_interest','public_task','other_documented_basis')),
  CONSTRAINT privacy_purposes_notice_chk CHECK (notice_reference = btrim(notice_reference) AND char_length(notice_reference) <= 512),
  CONSTRAINT privacy_purposes_consent_chk CHECK (consent_reference = btrim(consent_reference) AND char_length(consent_reference) <= 512),
  CONSTRAINT privacy_purposes_classes_chk CHECK (privacy_classes_valid(allowed_classes)),
  CONSTRAINT privacy_purposes_status_chk CHECK (status IN ('active','retired')),
  CONSTRAINT privacy_purposes_version_chk CHECK (version >= 1),
  CONSTRAINT privacy_purposes_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT privacy_purposes_consent_basis_chk CHECK (legal_basis <> 'consent' OR consent_reference <> ''),
  CONSTRAINT privacy_purposes_pii_notice_chk CHECK (
    NOT (allowed_classes ? 'personal' OR allowed_classes ? 'sensitive_operational') OR notice_reference <> ''
  )
);

CREATE TABLE privacy_retention_policies (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  purpose_key text NOT NULL,
  data_class text NOT NULL,
  retention_days integer NOT NULL,
  disposition text NOT NULL,
  legal_hold_permitted boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT privacy_retention_policies_pkey PRIMARY KEY (organization_id, workspace_id, purpose_key, data_class),
  CONSTRAINT privacy_retention_policies_purpose_fk FOREIGN KEY (organization_id, workspace_id, purpose_key)
    REFERENCES privacy_purposes (organization_id, workspace_id, purpose_key) ON DELETE RESTRICT,
  CONSTRAINT privacy_retention_class_chk CHECK (data_class IN ('public','internal','confidential','personal','sensitive_operational','secret')),
  CONSTRAINT privacy_retention_days_chk CHECK (retention_days BETWEEN 1 AND 36500),
  CONSTRAINT privacy_retention_disposition_chk CHECK (disposition IN ('delete','anonymize','archive_then_delete','manual_review')),
  CONSTRAINT privacy_retention_status_chk CHECK (status IN ('active','retired')),
  CONSTRAINT privacy_retention_version_chk CHECK (version >= 1),
  CONSTRAINT privacy_retention_timestamps_chk CHECK (updated_at >= created_at)
);

CREATE INDEX privacy_purposes_tenant_status_idx
  ON privacy_purposes (organization_id, workspace_id, status, purpose_key);
CREATE INDEX privacy_retention_tenant_status_idx
  ON privacy_retention_policies (organization_id, workspace_id, status, data_class, purpose_key);

ALTER TABLE privacy_purposes ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_purposes FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_purposes_tenant_select ON privacy_purposes FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY privacy_purposes_tenant_insert ON privacy_purposes FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY privacy_purposes_tenant_update ON privacy_purposes FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

ALTER TABLE privacy_retention_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_retention_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_retention_tenant_select ON privacy_retention_policies FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY privacy_retention_tenant_insert ON privacy_retention_policies FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY privacy_retention_tenant_update ON privacy_retention_policies FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE DELETE, TRUNCATE ON privacy_purposes, privacy_retention_policies FROM PUBLIC;

CREATE FUNCTION privacy_purpose_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.purpose_key IS DISTINCT FROM OLD.purpose_key
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''privacy purpose identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''privacy purpose version must increase by one'';
  END IF;
  IF OLD.status = ''retired'' AND NEW.status IS DISTINCT FROM OLD.status THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''retired privacy purpose cannot be reactivated'';
  END IF;
  IF EXISTS (
    SELECT 1 FROM privacy_retention_policies r
    WHERE r.organization_id = OLD.organization_id
      AND r.workspace_id = OLD.workspace_id
      AND r.purpose_key = OLD.purpose_key
      AND r.status = ''active''
      AND NOT (NEW.allowed_classes ? r.data_class)
  ) THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''active retention class cannot be removed from privacy purpose'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER privacy_purpose_guard
  BEFORE UPDATE ON privacy_purposes
  FOR EACH ROW EXECUTE FUNCTION privacy_purpose_guard_update();

CREATE FUNCTION privacy_retention_validate_purpose() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM privacy_purposes p
    WHERE p.organization_id = NEW.organization_id
      AND p.workspace_id = NEW.workspace_id
      AND p.purpose_key = NEW.purpose_key
      AND (NEW.status = ''retired'' OR p.status = ''active'')
      AND p.allowed_classes ? NEW.data_class
  ) THEN
    RAISE EXCEPTION USING ERRCODE = ''23514'', MESSAGE = ''retention class is not allowed by privacy purpose'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER privacy_retention_purpose_check
  BEFORE INSERT OR UPDATE ON privacy_retention_policies
  FOR EACH ROW EXECUTE FUNCTION privacy_retention_validate_purpose();

CREATE FUNCTION privacy_retention_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.purpose_key IS DISTINCT FROM OLD.purpose_key
     OR NEW.data_class IS DISTINCT FROM OLD.data_class
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''retention policy identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''retention policy version must increase by one'';
  END IF;
  IF OLD.status = ''retired'' AND NEW.status IS DISTINCT FROM OLD.status THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''retired retention policy cannot be reactivated'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER privacy_retention_guard
  BEFORE UPDATE ON privacy_retention_policies
  FOR EACH ROW EXECUTE FUNCTION privacy_retention_guard_update();

CREATE FUNCTION privacy_registry_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''privacy registry is append/retire only'';
  RETURN NULL;
END';

CREATE TRIGGER privacy_purposes_no_delete
  BEFORE DELETE ON privacy_purposes FOR EACH ROW EXECUTE FUNCTION privacy_registry_reject_delete();
CREATE TRIGGER privacy_purposes_no_clear
  BEFORE TRUNCATE ON privacy_purposes FOR EACH STATEMENT EXECUTE FUNCTION privacy_registry_reject_delete();
CREATE TRIGGER privacy_retention_no_delete
  BEFORE DELETE ON privacy_retention_policies FOR EACH ROW EXECUTE FUNCTION privacy_registry_reject_delete();
CREATE TRIGGER privacy_retention_no_clear
  BEFORE TRUNCATE ON privacy_retention_policies FOR EACH STATEMENT EXECUTE FUNCTION privacy_registry_reject_delete();

COMMENT ON TABLE privacy_purposes IS 'Tenant-scoped processing-purpose registry with legal basis and notice/consent evidence references. No raw PII is stored here.';
COMMENT ON TABLE privacy_retention_policies IS 'Tenant-scoped retention metadata consumed by Task 061 workflows; policy rows contain no subject PII.';
COMMENT ON COLUMN privacy_purposes.allowed_classes IS 'Canonical data classes permitted for this processing purpose. Secret remains forbidden from logs/events/analytics by platform policy.';

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
