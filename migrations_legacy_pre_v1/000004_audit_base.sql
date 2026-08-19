BEGIN;

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
