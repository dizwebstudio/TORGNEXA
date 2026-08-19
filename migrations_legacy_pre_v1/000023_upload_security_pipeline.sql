BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE upload_security_evidence (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  upload_id text NOT NULL,
  attempt bigint NOT NULL,
  policy_version text NOT NULL,
  content_sha256 text NOT NULL,
  content_size_bytes bigint NOT NULL,
  detected_media_type text NOT NULL,
  extension text NOT NULL,
  decision text NOT NULL,
  reason_code text NOT NULL,
  checks jsonb NOT NULL,
  scanner_name text NOT NULL,
  scanner_engine_version text NOT NULL,
  scanner_signature_version text NOT NULL,
  scanner_status text NOT NULL,
  threat_code text,
  rescan_of text,
  created_at timestamptz NOT NULL,
  CONSTRAINT upload_security_evidence_pkey PRIMARY KEY (id),
  CONSTRAINT upload_security_evidence_tenant_attempt UNIQUE (organization_id, workspace_id, upload_id, attempt),
  CONSTRAINT upload_security_evidence_tenant_identity UNIQUE (id, organization_id, workspace_id, upload_id),
  CONSTRAINT upload_security_evidence_upload_fk FOREIGN KEY (upload_id, organization_id, workspace_id)
    REFERENCES uploads (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT upload_security_evidence_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT upload_security_evidence_id_chk CHECK (id ~ '^uev_[0-9a-f]{32}$'),
  CONSTRAINT upload_security_evidence_attempt_chk CHECK (attempt >= 1),
  CONSTRAINT upload_security_evidence_policy_chk CHECK (policy_version ~ '^[a-z][a-z0-9._-]{0,127}$'),
  CONSTRAINT upload_security_evidence_sha_chk CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT upload_security_evidence_size_chk CHECK (content_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT upload_security_evidence_media_chk CHECK (char_length(detected_media_type) BETWEEN 3 AND 255 AND detected_media_type ~ '^[A-Za-z0-9][A-Za-z0-9!#$&^_.+/-]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}$'),
  CONSTRAINT upload_security_evidence_extension_chk CHECK (extension='none' OR extension ~ '^\.[a-z0-9]{1,15}$'),
  CONSTRAINT upload_security_evidence_decision_chk CHECK (decision IN ('clean','rejected','error')),
  CONSTRAINT upload_security_evidence_reason_chk CHECK (reason_code ~ '^[a-z0-9][a-z0-9._-]{0,95}$'),
  CONSTRAINT upload_security_evidence_checks_chk CHECK (jsonb_typeof(checks)='array' AND jsonb_array_length(checks) BETWEEN 1 AND 32),
  CONSTRAINT upload_security_evidence_scanner_chk CHECK (
    scanner_name ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
    AND char_length(scanner_engine_version) BETWEEN 1 AND 128
    AND char_length(scanner_signature_version) BETWEEN 1 AND 128
    AND scanner_status IN ('clean','infected','error','not_run')
  ),
  CONSTRAINT upload_security_evidence_threat_chk CHECK (
    (scanner_status='infected' AND threat_code ~ '^[a-z0-9][a-z0-9._-]{0,127}$')
    OR (scanner_status<>'infected' AND threat_code IS NULL)
  ),
  CONSTRAINT upload_security_evidence_decision_scanner_chk CHECK (
    (decision='clean' AND scanner_status='clean')
    OR (decision='error' AND scanner_status='error')
    OR (decision='rejected' AND scanner_status IN ('infected','error','not_run'))
  ),
  CONSTRAINT upload_security_evidence_rescan_chk CHECK (rescan_of IS NULL OR rescan_of ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);

CREATE INDEX upload_security_evidence_history_idx
  ON upload_security_evidence(organization_id, workspace_id, upload_id, attempt DESC);

ALTER TABLE upload_security_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE upload_security_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY upload_security_evidence_tenant_all ON upload_security_evidence FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

ALTER TABLE uploads ADD CONSTRAINT uploads_security_evidence_fk
  FOREIGN KEY (security_evidence_id, organization_id, workspace_id, id)
  REFERENCES upload_security_evidence (id, organization_id, workspace_id, upload_id)
  DEFERRABLE INITIALLY IMMEDIATE;

DROP TRIGGER uploads_foundation_guard_update ON uploads;
DROP FUNCTION uploads_foundation_guard_update();

CREATE FUNCTION uploads_security_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE
  evidence_decision text;
BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.original_filename<>OLD.original_filename OR NEW.declared_media_type IS DISTINCT FROM OLD.declared_media_type OR NEW.declared_size_bytes<>OLD.declared_size_bytes OR NEW.received_at<>OLD.received_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload identity and client metadata are immutable'';
  END IF;

  IF OLD.state<>''received'' AND (
    NEW.quarantine_object_key IS DISTINCT FROM OLD.quarantine_object_key
    OR NEW.content_size_bytes IS DISTINCT FROM OLD.content_size_bytes
    OR NEW.content_sha256 IS DISTINCT FROM OLD.content_sha256
    OR NEW.quarantined_at IS DISTINCT FROM OLD.quarantined_at
  ) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''quarantined upload identity and content metadata are immutable'';
  END IF;

  IF NOT (
    (OLD.state=''received'' AND NEW.state=''quarantined'')
    OR (OLD.state=''quarantined'' AND NEW.state IN (''validated'',''rejected''))
    OR (OLD.state=''validated'' AND NEW.state=''scanning'')
    OR (OLD.state=''scanning'' AND NEW.state IN (''clean'',''rejected''))
    OR (OLD.state=''clean'' AND NEW.state=''released'')
    OR (OLD.state IN (''clean'',''rejected'',''released'') AND NEW.state=''quarantined'')
  ) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid upload security state transition'';
  END IF;

  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload version/time must advance exactly once'';
  END IF;

  IF NEW.state IN (''clean'',''rejected'',''released'') THEN
    SELECT decision INTO evidence_decision
      FROM upload_security_evidence
      WHERE id=NEW.security_evidence_id
        AND organization_id=NEW.organization_id
        AND workspace_id=NEW.workspace_id
        AND upload_id=NEW.id;
    IF evidence_decision IS NULL THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload terminal state requires same-tenant immutable security evidence'';
    END IF;
    IF NEW.state=''clean'' AND evidence_decision<>''clean'' THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''clean upload requires clean security evidence'';
    END IF;
    IF NEW.state=''rejected'' AND evidence_decision<>''rejected'' THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''rejected upload requires rejected security evidence'';
    END IF;
    IF NEW.state=''released'' AND (evidence_decision<>''clean'' OR NEW.security_evidence_id IS DISTINCT FROM OLD.security_evidence_id) THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''release must preserve the clean security evidence'';
    END IF;
  END IF;

  IF OLD.state IN (''clean'',''rejected'',''released'') AND NEW.state=''quarantined'' THEN
    IF NEW.security_evidence_id IS NOT NULL OR NEW.released_object_key IS NOT NULL OR NEW.released_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''rescan must revoke released capability before scanning'';
    END IF;
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER uploads_security_guard_update BEFORE UPDATE ON uploads
  FOR EACH ROW EXECUTE FUNCTION uploads_security_guard_update();

CREATE FUNCTION upload_security_evidence_guard_insert() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE
  item jsonb;
BEGIN
  FOR item IN SELECT value FROM jsonb_array_elements(NEW.checks) LOOP
    IF jsonb_typeof(item)<>''object''
      OR jsonb_object_length(item)<>2
      OR NOT (item ? ''code'')
      OR NOT (item ? ''outcome'')
      OR jsonb_typeof(item->''code'')<>''string''
      OR jsonb_typeof(item->''outcome'')<>''string''
      OR (item->>''code'') !~ ''^[a-z0-9][a-z0-9._-]{0,127}$''
      OR (item->>''outcome'') NOT IN (''pass'',''fail'') THEN
      RAISE EXCEPTION USING ERRCODE=''23514'', MESSAGE=''invalid upload security check evidence'';
    END IF;
  END LOOP;
  RETURN NEW;
END';
CREATE TRIGGER upload_security_evidence_validate_insert BEFORE INSERT ON upload_security_evidence
  FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_guard_insert();

REVOKE UPDATE, DELETE, TRUNCATE ON upload_security_evidence FROM PUBLIC;
CREATE FUNCTION upload_security_evidence_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload security evidence is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER upload_security_evidence_no_update BEFORE UPDATE ON upload_security_evidence FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_reject_mutation();
CREATE TRIGGER upload_security_evidence_no_delete BEFORE DELETE ON upload_security_evidence FOR EACH ROW EXECUTE FUNCTION upload_security_evidence_reject_mutation();
CREATE TRIGGER upload_security_evidence_no_clear BEFORE TRUNCATE ON upload_security_evidence FOR EACH STATEMENT EXECUTE FUNCTION upload_security_evidence_reject_mutation();

COMMENT ON TABLE upload_security_evidence IS 'Append-only tenant-scoped MIME/archive/parser/malware evidence for Task 088. Raw file content and client credentials are forbidden.';
COMMENT ON COLUMN upload_security_evidence.checks IS 'Bounded machine-code pass/fail checks only; never raw file content or scanner logs.';
COMMENT ON COLUMN uploads.security_evidence_id IS 'Current immutable security decision evidence. Cleared before every re-scan and required for CLEAN/REJECTED/RELEASED.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
