BEGIN;

-- TORGNEXA pre-v1 baseline component 000004: security_eventing.
-- Squashed, statement-order-preserving source range: legacy 000004..000008.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000004_audit_base.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE audit_records
  ADD COLUMN risk text NOT NULL DEFAULT 'unclassified';

ALTER TABLE audit_records
  ADD CONSTRAINT audit_records_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_actor_id_chk CHECK (
    actor_id IS NULL OR (
      actor_id = btrim(actor_id)
      AND char_length(actor_id) BETWEEN 1 AND 256
      AND actor_id !~ '[[:cntrl:]]'
    )
  ) NOT VALID,
  ADD CONSTRAINT audit_records_source_chk CHECK (
    source = btrim(source)
    AND char_length(source) BETWEEN 1 AND 128
    AND source ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_action_chk CHECK (
    action = btrim(action)
    AND char_length(action) BETWEEN 1 AND 160
    AND action ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_resource_type_chk CHECK (
    resource_type = btrim(resource_type)
    AND char_length(resource_type) BETWEEN 1 AND 128
    AND resource_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_resource_id_chk CHECK (
    resource_id = btrim(resource_id)
    AND char_length(resource_id) BETWEEN 1 AND 512
    AND resource_id !~ '[[:cntrl:]]'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_correlation_id_chk CHECK (
    correlation_id IS NULL OR (
      correlation_id = btrim(correlation_id)
      AND char_length(correlation_id) BETWEEN 1 AND 256
      AND correlation_id !~ '[[:cntrl:]]'
    )
  ) NOT VALID,
  ADD CONSTRAINT audit_records_risk_chk CHECK (
    risk IN ('unclassified', 'read', 'write_safe', 'write_sensitive', 'legally_significant')
  ) NOT VALID,
  ADD CONSTRAINT audit_records_summary_object_chk CHECK (
    jsonb_typeof(summary) = 'object'
  ) NOT VALID,
  ADD CONSTRAINT audit_records_summary_size_chk CHECK (
    octet_length(summary::text) <= 32768
  ) NOT VALID,
  ADD CONSTRAINT audit_records_summary_redaction_chk CHECK (
    lower(summary::text) !~ '"[a-z0-9._ -]*(authorization|cookie|password|passwd|token|secret|api[-_. ]?key|private[-_. ]?key|signing[-_. ]?key|credential|credentials|session|access[-_. ]?key)[a-z0-9._ -]*"[[:space:]]*:'
    AND lower(summary::text) !~ ':[[:space:]]*"(bearer|basic|digest|negotiate|aws4-hmac-sha256)[[:space:]]'
    AND summary::text !~* '-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----'
  ) NOT VALID;

ALTER TABLE audit_records
  VALIDATE CONSTRAINT audit_records_id_sortable_chk,
  VALIDATE CONSTRAINT audit_records_actor_id_chk,
  VALIDATE CONSTRAINT audit_records_source_chk,
  VALIDATE CONSTRAINT audit_records_action_chk,
  VALIDATE CONSTRAINT audit_records_resource_type_chk,
  VALIDATE CONSTRAINT audit_records_resource_id_chk,
  VALIDATE CONSTRAINT audit_records_correlation_id_chk,
  VALIDATE CONSTRAINT audit_records_risk_chk,
  VALIDATE CONSTRAINT audit_records_summary_object_chk,
  VALIDATE CONSTRAINT audit_records_summary_size_chk,
  VALIDATE CONSTRAINT audit_records_summary_redaction_chk;

DROP POLICY audit_records_tenant_isolation ON audit_records;

CREATE POLICY audit_records_tenant_select ON audit_records
  FOR SELECT
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

CREATE POLICY audit_records_tenant_insert ON audit_records
  FOR INSERT
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

REVOKE UPDATE, DELETE, TRUNCATE ON audit_records FROM PUBLIC;

CREATE FUNCTION audit_records_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''audit_records is append-only'';
  RETURN NULL;
END';

CREATE TRIGGER audit_records_no_update_delete
  BEFORE UPDATE OR DELETE ON audit_records
  FOR EACH ROW
  EXECUTE FUNCTION audit_records_reject_mutation();

CREATE TRIGGER audit_records_no_clear
  BEFORE TRUNCATE ON audit_records
  FOR EACH STATEMENT
  EXECUTE FUNCTION audit_records_reject_mutation();

COMMENT ON TABLE audit_records IS 'Tenant-scoped append-only application audit. Application roles may select/insert only; mutation requires privileged schema maintenance outside the application port.';
COMMENT ON COLUMN audit_records.risk IS 'Operation risk class. unclassified is reserved for rows/writers predating Task 003; new application writes must use read, write_safe, write_sensitive, or legally_significant.';
COMMENT ON COLUMN audit_records.summary IS 'Bounded redacted JSON object; never raw Authorization headers, credentials, secrets, full request/response payloads, or private key material.';

-- SOURCE 000005_secrets_provider.sql
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

-- SOURCE 000006_privacy_foundation.sql
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

-- SOURCE 000007_transactional_outbox.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE outbox_events
  ADD COLUMN event_envelope jsonb,
  ADD COLUMN available_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN lease_owner text,
  ADD COLUMN lease_token text,
  ADD COLUMN lease_expires_at timestamptz,
  ADD COLUMN last_attempt_at timestamptz,
  ADD COLUMN last_error_code text;

ALTER TABLE outbox_events
  ADD CONSTRAINT outbox_events_attempts_chk CHECK (attempts >= 0) NOT VALID,
  ADD CONSTRAINT outbox_events_event_type_chk CHECK (
    event_envelope IS NULL OR event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$'
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_aggregate_type_chk CHECK (
    event_envelope IS NULL OR (
    aggregate_type = btrim(aggregate_type)
    AND char_length(aggregate_type) BETWEEN 1 AND 128
    AND aggregate_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_aggregate_id_chk CHECK (
    event_envelope IS NULL OR (
    aggregate_id = btrim(aggregate_id)
    AND char_length(aggregate_id) BETWEEN 1 AND 128
    AND aggregate_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_payload_object_chk CHECK (event_envelope IS NULL OR jsonb_typeof(payload) = 'object') NOT VALID,
  ADD CONSTRAINT outbox_events_payload_size_chk CHECK (event_envelope IS NULL OR octet_length(payload::text) <= 1048576) NOT VALID,
  ADD CONSTRAINT outbox_events_envelope_chk CHECK (
    event_envelope IS NULL OR (
      jsonb_typeof(event_envelope) = 'object'
      AND octet_length(event_envelope::text) <= 1081344
      AND event_envelope ?& ARRAY[
        'event_id','event_type','occurred_at','organization_id','workspace_id',
        'correlation_id','causation_id','entity_type','entity_id','source','data'
      ]
      AND event_envelope->>'event_id' = id
      AND event_envelope->>'event_type' = event_type
      AND event_envelope->>'organization_id' = organization_id
      AND event_envelope->>'workspace_id' = workspace_id
      AND event_envelope->>'entity_type' = aggregate_type
      AND event_envelope->>'entity_id' = aggregate_id
      AND jsonb_typeof(event_envelope->'data') = 'object'
      AND event_envelope->'data' = payload
      AND (event_envelope->>'occurred_at') ~ 'Z$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_lease_chk CHECK (
    (lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
    OR (
      lease_owner = btrim(lease_owner)
      AND char_length(lease_owner) BETWEEN 1 AND 128
      AND lease_owner ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
      AND lease_token ~ '^[0-9a-f]{32}$'
      AND lease_expires_at IS NOT NULL
      AND last_attempt_at IS NOT NULL
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_publish_state_chk CHECK (
    published_at IS NULL OR (lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_error_code_chk CHECK (
    last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_timestamps_chk CHECK (
    available_at >= created_at
    AND (last_attempt_at IS NULL OR last_attempt_at >= created_at)
    AND (published_at IS NULL OR published_at >= created_at)
  ) NOT VALID;

ALTER TABLE outbox_events
  VALIDATE CONSTRAINT outbox_events_attempts_chk,
  VALIDATE CONSTRAINT outbox_events_event_type_chk,
  VALIDATE CONSTRAINT outbox_events_aggregate_type_chk,
  VALIDATE CONSTRAINT outbox_events_aggregate_id_chk,
  VALIDATE CONSTRAINT outbox_events_payload_object_chk,
  VALIDATE CONSTRAINT outbox_events_payload_size_chk,
  VALIDATE CONSTRAINT outbox_events_envelope_chk,
  VALIDATE CONSTRAINT outbox_events_lease_chk,
  VALIDATE CONSTRAINT outbox_events_publish_state_chk,
  VALIDATE CONSTRAINT outbox_events_error_code_chk,
  VALIDATE CONSTRAINT outbox_events_timestamps_chk;

DROP INDEX outbox_events_unpublished_idx;
CREATE INDEX outbox_events_unpublished_idx
  ON outbox_events (organization_id, workspace_id, available_at, created_at, id)
  WHERE published_at IS NULL AND event_envelope IS NOT NULL;
CREATE INDEX outbox_events_lease_expiry_idx
  ON outbox_events (organization_id, workspace_id, lease_expires_at, id)
  WHERE published_at IS NULL AND lease_expires_at IS NOT NULL;

DROP POLICY outbox_events_tenant_isolation ON outbox_events;
CREATE POLICY outbox_events_tenant_select ON outbox_events FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY outbox_events_tenant_insert ON outbox_events FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY outbox_events_tenant_update ON outbox_events FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE DELETE, TRUNCATE ON outbox_events FROM PUBLIC;

CREATE FUNCTION outbox_events_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.event_type IS DISTINCT FROM OLD.event_type
     OR NEW.aggregate_type IS DISTINCT FROM OLD.aggregate_type
     OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
     OR NEW.payload IS DISTINCT FROM OLD.payload
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR (OLD.event_envelope IS NOT NULL AND NEW.event_envelope IS DISTINCT FROM OLD.event_envelope) THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox event identity and body are immutable'';
  END IF;
  IF OLD.published_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''published outbox event is immutable'';
  END IF;
  IF NEW.attempts < OLD.attempts THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox attempts cannot decrease'';
  END IF;
  IF OLD.published_at IS NULL AND NEW.published_at IS NOT NULL AND NEW.published_at < OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox published_at precedes creation'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER outbox_events_update_guard
  BEFORE UPDATE ON outbox_events
  FOR EACH ROW EXECUTE FUNCTION outbox_events_guard_update();

CREATE FUNCTION outbox_events_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox events cannot be deleted by application runtime'';
  RETURN NULL;
END';

CREATE TRIGGER outbox_events_no_delete
  BEFORE DELETE ON outbox_events FOR EACH ROW EXECUTE FUNCTION outbox_events_reject_delete();
CREATE TRIGGER outbox_events_no_clear
  BEFORE TRUNCATE ON outbox_events FOR EACH STATEMENT EXECUTE FUNCTION outbox_events_reject_delete();

COMMENT ON TABLE outbox_events IS 'Tenant-scoped transactional outbox. Domain state and event intent are inserted in one PostgreSQL transaction; relay uses short SKIP LOCKED leases and at-least-once publication.';
COMMENT ON COLUMN outbox_events.event_envelope IS 'Canonical immutable EventBus envelope. NULL is reserved for pre-Task-008 legacy rows and is never claimed by the new relay.';
COMMENT ON COLUMN outbox_events.lease_token IS 'Opaque compare-by-lease token. A stale relay cannot acknowledge or reschedule a row after lease loss.';
COMMENT ON COLUMN outbox_events.last_error_code IS 'Bounded machine code only; raw broker/client error text is forbidden because it may contain credentials or PII.';
COMMENT ON COLUMN outbox_events.published_at IS 'Set only after EventBus publish succeeds. Crash after publish but before this update may duplicate the immutable event ID; Task 009 consumer inbox performs deduplication.';

-- SOURCE 000008_inbox_idempotency.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Keep the pre-Task-009 inbox_events placeholder untouched and deny-all for
-- rolling compatibility. Runtime Task-009 code uses this new tenant-scoped,
-- immutable receipt table. A later contract migration may retire the placeholder.
CREATE TABLE inbox_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  consumer text NOT NULL,
  event_id text NOT NULL,
  event_type text NOT NULL,
  event_fingerprint text NOT NULL,
  first_observed_at timestamptz NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  processed_attempt integer NOT NULL,
  CONSTRAINT inbox_receipts_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT inbox_receipts_consumer_chk CHECK (
    consumer ~ '^[a-z][a-z0-9._:-]{0,127}$'
  ),
  CONSTRAINT inbox_receipts_event_id_chk CHECK (
    event_id = btrim(event_id)
    AND char_length(event_id) BETWEEN 1 AND 128
    AND event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ),
  CONSTRAINT inbox_receipts_event_type_chk CHECK (
    event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$'
  ),
  CONSTRAINT inbox_receipts_fingerprint_chk CHECK (
    event_fingerprint ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT inbox_receipts_attempt_chk CHECK (
    processed_attempt BETWEEN 1 AND 1000
  ),
  PRIMARY KEY (organization_id, workspace_id, consumer, event_id)
);

CREATE INDEX inbox_receipts_consumer_processed_idx
  ON inbox_receipts (organization_id, workspace_id, consumer, processed_at DESC, event_id);

ALTER TABLE inbox_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbox_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY inbox_receipts_tenant_select ON inbox_receipts FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY inbox_receipts_tenant_insert ON inbox_receipts FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE UPDATE, DELETE, TRUNCATE ON inbox_receipts FROM PUBLIC;

CREATE FUNCTION inbox_receipts_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''inbox receipts are immutable after processing'';
  RETURN NULL;
END';

CREATE TRIGGER inbox_receipts_no_update
  BEFORE UPDATE ON inbox_receipts FOR EACH ROW EXECUTE FUNCTION inbox_receipts_reject_mutation();
CREATE TRIGGER inbox_receipts_no_delete
  BEFORE DELETE ON inbox_receipts FOR EACH ROW EXECUTE FUNCTION inbox_receipts_reject_mutation();
CREATE TRIGGER inbox_receipts_no_clear
  BEFORE TRUNCATE ON inbox_receipts FOR EACH STATEMENT EXECUTE FUNCTION inbox_receipts_reject_mutation();

COMMENT ON TABLE inbox_receipts IS 'Tenant-scoped immutable consumer idempotency receipts. Business PostgreSQL side effects and receipt insert commit in the same transaction after a transaction-scoped advisory lock.';
COMMENT ON COLUMN inbox_receipts.consumer IS 'Stable logical consumer identity, not an ephemeral pod/member ID. Change/version it deliberately when replay semantics must change.';
COMMENT ON COLUMN inbox_receipts.event_fingerprint IS 'SHA-256 of the canonical immutable EventBus envelope; detects event-ID reuse with different content without duplicating business payload/PII into inbox storage.';
COMMENT ON COLUMN inbox_receipts.processed_attempt IS 'EventBus delivery attempt that committed the transactional business effect. This is observability metadata, not a retry counter owned by the inbox.';
COMMENT ON TABLE inbox_events IS 'Pre-Task-009 compatibility placeholder retained deny-all during expand phase. New runtime code uses inbox_receipts; retire only in a later contract migration after fleet qualification.';
-- BASELINE_SOURCE_END

CREATE TABLE IF NOT EXISTS migration_baseline_evidence (
  baseline_id text PRIMARY KEY,
  source_head_version integer NOT NULL CHECK (source_head_version > 0),
  source_catalog_sha256 text NOT NULL CHECK (source_catalog_sha256 ~ '^[0-9a-f]{64}$'),
  source_history_rows integer NOT NULL CHECK (source_history_rows >= 0),
  mode text NOT NULL CHECK (mode IN ('fresh_baseline','legacy_rebaseline')),
  stamped_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON migration_baseline_evidence FROM PUBLIC;
INSERT INTO migration_baseline_evidence(baseline_id,source_head_version,source_catalog_sha256,source_history_rows,mode)
VALUES ('pre_v1_v1',74,'f8e30240224fe2cd2d32852f8a3f15569c8a1d5d5c08578274708120fd1e9aaa',0,'fresh_baseline')
ON CONFLICT (baseline_id) DO NOTHING;

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
