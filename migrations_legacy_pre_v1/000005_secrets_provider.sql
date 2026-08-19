BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE secret_references (
  reference text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  class text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  current_version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  CONSTRAINT secret_references_pkey PRIMARY KEY (reference),
  CONSTRAINT secret_references_tenant_key UNIQUE (reference, organization_id, workspace_id),
  CONSTRAINT secret_references_reference_chk CHECK (reference ~ '^sec:v1:[0-9a-f]{32}$'),
  CONSTRAINT secret_references_class_chk CHECK (class IN (
    'connector_token', 'oauth_client', 'oauth_refresh', 'erp_credential',
    'webhook_signing', 'certificate', 'storage_credential'
  )),
  CONSTRAINT secret_references_status_chk CHECK (status IN ('active', 'revoked')),
  CONSTRAINT secret_references_version_chk CHECK (current_version >= 1),
  CONSTRAINT secret_references_timestamps_chk CHECK (
    updated_at >= created_at
    AND ((status = 'active' AND revoked_at IS NULL) OR
         (status = 'revoked' AND revoked_at IS NOT NULL AND revoked_at >= created_at AND updated_at >= revoked_at))
  ),
  CONSTRAINT secret_references_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT
);

CREATE TABLE secret_versions (
  reference text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL,
  algorithm text NOT NULL,
  key_id text NOT NULL,
  nonce bytea NOT NULL,
  ciphertext bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT secret_versions_pkey PRIMARY KEY (reference, version),
  CONSTRAINT secret_versions_reference_fk FOREIGN KEY (reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT secret_versions_version_chk CHECK (version >= 1),
  CONSTRAINT secret_versions_algorithm_chk CHECK (algorithm = 'aes-256-gcm'),
  CONSTRAINT secret_versions_key_id_chk CHECK (
    key_id = btrim(key_id)
    AND char_length(key_id) BETWEEN 1 AND 128
    AND key_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ),
  CONSTRAINT secret_versions_nonce_chk CHECK (octet_length(nonce) = 12),
  CONSTRAINT secret_versions_ciphertext_chk CHECK (octet_length(ciphertext) BETWEEN 17 AND 65568)
);

ALTER TABLE connector_accounts
  ADD COLUMN secret_reference text,
  ADD CONSTRAINT connector_accounts_secret_reference_fk
    FOREIGN KEY (secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id)
    ON DELETE RESTRICT;

CREATE INDEX connector_accounts_secret_reference_idx
  ON connector_accounts (organization_id, workspace_id, secret_reference)
  WHERE secret_reference IS NOT NULL;

CREATE INDEX secret_references_tenant_status_idx
  ON secret_references (organization_id, workspace_id, status, reference);
CREATE INDEX secret_versions_tenant_reference_idx
  ON secret_versions (organization_id, workspace_id, reference, version DESC);

ALTER TABLE secret_references ENABLE ROW LEVEL SECURITY;
ALTER TABLE secret_references FORCE ROW LEVEL SECURITY;
CREATE POLICY secret_references_tenant_select ON secret_references
  FOR SELECT
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );
CREATE POLICY secret_references_tenant_insert ON secret_references
  FOR INSERT
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );
CREATE POLICY secret_references_tenant_update ON secret_references
  FOR UPDATE
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

ALTER TABLE secret_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE secret_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY secret_versions_tenant_select ON secret_versions
  FOR SELECT
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );
CREATE POLICY secret_versions_tenant_insert ON secret_versions
  FOR INSERT
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

REVOKE DELETE, TRUNCATE ON secret_references, secret_versions FROM PUBLIC;
REVOKE UPDATE ON secret_versions FROM PUBLIC;

CREATE FUNCTION secret_references_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.reference IS DISTINCT FROM OLD.reference
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.class IS DISTINCT FROM OLD.class
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''secret reference identity is immutable'';
  END IF;
  IF NEW.current_version < OLD.current_version OR NEW.current_version > OLD.current_version + 1 THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''secret version transition is invalid'';
  END IF;
  IF NEW.current_version = OLD.current_version + 1 AND NOT EXISTS (
    SELECT 1 FROM secret_versions
    WHERE reference = OLD.reference
      AND organization_id = OLD.organization_id
      AND workspace_id = OLD.workspace_id
      AND version = NEW.current_version
  ) THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''secret version must exist before activation'';
  END IF;
  IF OLD.status = ''revoked'' AND NEW.status IS DISTINCT FROM OLD.status THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''revoked secret cannot be reactivated'';
  END IF;
  IF OLD.status = ''active'' AND NEW.status NOT IN (''active'', ''revoked'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''secret status transition is invalid'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER secret_references_guard
  BEFORE UPDATE ON secret_references
  FOR EACH ROW
  EXECUTE FUNCTION secret_references_guard_update();

CREATE FUNCTION secret_versions_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''secret_versions is immutable'';
  RETURN NULL;
END';

CREATE TRIGGER secret_versions_no_update_delete
  BEFORE UPDATE OR DELETE ON secret_versions
  FOR EACH ROW
  EXECUTE FUNCTION secret_versions_reject_mutation();
CREATE TRIGGER secret_versions_no_clear
  BEFORE TRUNCATE ON secret_versions
  FOR EACH STATEMENT
  EXECUTE FUNCTION secret_versions_reject_mutation();

COMMENT ON COLUMN connector_accounts.secret_reference IS 'Opaque tenant-bound SecretProvider handle. Provider credentials must never be stored directly on connector_accounts.';
COMMENT ON TABLE secret_references IS 'Tenant-scoped stable opaque handles. Business tables store the reference, never provider credentials.';
COMMENT ON TABLE secret_versions IS 'Immutable encrypted-at-rest secret versions. Plaintext and master keys are forbidden; key_id identifies an external master-key source.';
COMMENT ON COLUMN secret_versions.ciphertext IS 'AES-256-GCM ciphertext including authentication tag; plaintext provider credentials must never be written to PostgreSQL.';
COMMENT ON COLUMN secret_versions.key_id IS 'Non-secret identifier for a master key supplied outside PostgreSQL (secret mount, Vault, KMS, HSM, or equivalent).';

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
