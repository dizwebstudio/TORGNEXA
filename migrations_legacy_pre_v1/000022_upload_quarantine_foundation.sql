BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE uploads (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  original_filename text NOT NULL,
  declared_media_type text,
  declared_size_bytes bigint NOT NULL,
  state text NOT NULL,
  quarantine_object_key text,
  released_object_key text,
  content_size_bytes bigint,
  content_sha256 text,
  security_evidence_id text,
  version bigint NOT NULL DEFAULT 1,
  received_at timestamptz NOT NULL,
  quarantined_at timestamptz,
  released_at timestamptz,
  updated_at timestamptz NOT NULL,
  CONSTRAINT uploads_pkey PRIMARY KEY (id),
  CONSTRAINT uploads_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT uploads_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT uploads_id_chk CHECK (id ~ '^upl_[0-9a-f]{32}$'),
  CONSTRAINT uploads_filename_chk CHECK (original_filename=btrim(original_filename) AND char_length(original_filename) BETWEEN 1 AND 512),
  CONSTRAINT uploads_media_type_chk CHECK (declared_media_type IS NULL OR (char_length(declared_media_type) BETWEEN 3 AND 255 AND declared_media_type ~ '^[A-Za-z0-9][A-Za-z0-9!#$&^_.+/-]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}$')),
  CONSTRAINT uploads_declared_size_chk CHECK (declared_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT uploads_content_size_chk CHECK (content_size_bytes IS NULL OR content_size_bytes BETWEEN 0 AND 10737418240),
  CONSTRAINT uploads_sha256_chk CHECK (content_sha256 IS NULL OR content_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT uploads_evidence_chk CHECK (security_evidence_id IS NULL OR (security_evidence_id=btrim(security_evidence_id) AND char_length(security_evidence_id) BETWEEN 1 AND 128 AND security_evidence_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT uploads_state_chk CHECK (state IN ('received','quarantined','validated','scanning','clean','rejected','released')),
  CONSTRAINT uploads_version_chk CHECK (version >= 1),
  CONSTRAINT uploads_time_chk CHECK (updated_at >= received_at AND (quarantined_at IS NULL OR quarantined_at >= received_at) AND (released_at IS NULL OR (quarantined_at IS NOT NULL AND released_at >= quarantined_at))),
  CONSTRAINT uploads_quarantine_key_chk CHECK (quarantine_object_key IS NULL OR quarantine_object_key = 'quarantine/'||organization_id||'/'||workspace_id||'/'||id||'/object'),
  CONSTRAINT uploads_release_key_chk CHECK (released_object_key IS NULL OR released_object_key = 'released/'||organization_id||'/'||workspace_id||'/'||id||'/object'),
  CONSTRAINT uploads_lifecycle_shape_chk CHECK (
    (state='received' AND quarantine_object_key IS NULL AND released_object_key IS NULL AND content_size_bytes IS NULL AND content_sha256 IS NULL AND security_evidence_id IS NULL AND quarantined_at IS NULL AND released_at IS NULL)
    OR
    (state IN ('quarantined','validated','scanning') AND quarantine_object_key IS NOT NULL AND released_object_key IS NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NULL AND quarantined_at IS NOT NULL AND released_at IS NULL)
    OR
    (state IN ('clean','rejected') AND quarantine_object_key IS NOT NULL AND released_object_key IS NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NOT NULL AND quarantined_at IS NOT NULL AND released_at IS NULL)
    OR
    (state='released' AND quarantine_object_key IS NOT NULL AND released_object_key IS NOT NULL AND content_size_bytes IS NOT NULL AND content_sha256 IS NOT NULL AND security_evidence_id IS NOT NULL AND quarantined_at IS NOT NULL AND released_at IS NOT NULL)
  )
);

CREATE INDEX uploads_tenant_state_idx ON uploads(organization_id, workspace_id, state, updated_at DESC, id DESC);

ALTER TABLE uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY uploads_tenant_all ON uploads FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- Task 088a is intentionally fail-closed. This trigger permits only the
-- foundation transition. Task 088b must replace it when security evidence,
-- scanner/parser/archive controls and release authorization exist.
CREATE FUNCTION uploads_foundation_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.original_filename<>OLD.original_filename OR NEW.declared_media_type IS DISTINCT FROM OLD.declared_media_type OR NEW.declared_size_bytes<>OLD.declared_size_bytes OR NEW.received_at<>OLD.received_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload identity and client metadata are immutable'';
  END IF;
  IF NOT (OLD.state=''received'' AND NEW.state=''quarantined'') THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload security pipeline incomplete: only received to quarantined is allowed before task 088b'';
  END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload version/time must advance exactly once'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER uploads_foundation_guard_update BEFORE UPDATE ON uploads
  FOR EACH ROW EXECUTE FUNCTION uploads_foundation_guard_update();

REVOKE DELETE, TRUNCATE ON uploads FROM PUBLIC;
CREATE FUNCTION uploads_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''upload records are retained through the security pipeline'';
  RETURN NULL;
END';
CREATE TRIGGER uploads_no_delete BEFORE DELETE ON uploads FOR EACH ROW EXECUTE FUNCTION uploads_reject_delete();
CREATE TRIGGER uploads_no_clear BEFORE TRUNCATE ON uploads FOR EACH STATEMENT EXECUTE FUNCTION uploads_reject_delete();

COMMENT ON TABLE uploads IS 'Tenant-scoped upload quarantine state. Task 088a permits only RECEIVED->QUARANTINED; downstream access requires RELEASED plus security evidence introduced by Task 088b.';
COMMENT ON COLUMN uploads.original_filename IS 'Untrusted display metadata only. It must never be interpreted as an object-storage or filesystem path.';
COMMENT ON COLUMN uploads.security_evidence_id IS 'Reserved for immutable Task-088b validation/scan evidence. Foundation writes NULL only.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
