BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The identity provider remains authoritative for username/email and the
-- application stores only a tenant-scoped editable profile projection. Raw
-- OIDC subjects are never persisted; subject_ref is a one-way issuer+subject
-- reference created by the authentication boundary.
CREATE TABLE user_profiles (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  subject_ref text NOT NULL,
  username text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  given_name text NOT NULL DEFAULT '',
  family_name text NOT NULL DEFAULT '',
  birthdate text NOT NULL DEFAULT '',
  job_title text NOT NULL DEFAULT '',
  department text NOT NULL DEFAULT '',
  phone_number text NOT NULL DEFAULT '',
  picture_upload_id text,
  version bigint NOT NULL DEFAULT 1,
  last_mutation_key text NOT NULL DEFAULT '',
  last_mutation_hash text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_profiles_pkey PRIMARY KEY (organization_id, workspace_id, subject_ref),
  CONSTRAINT user_profiles_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT user_profiles_picture_upload_fk FOREIGN KEY (picture_upload_id, organization_id, workspace_id)
    REFERENCES uploads (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT user_profiles_subject_ref_chk CHECK (subject_ref ~ '^[0-9a-f]{64}$'),
  CONSTRAINT user_profiles_username_chk CHECK (char_length(username) <= 128 AND username !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_email_chk CHECK (char_length(email) <= 254 AND email !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_given_name_chk CHECK (char_length(given_name) <= 160 AND given_name !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_family_name_chk CHECK (char_length(family_name) <= 160 AND family_name !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_birthdate_chk CHECK (birthdate = '' OR birthdate ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
  CONSTRAINT user_profiles_job_title_chk CHECK (char_length(job_title) <= 160 AND job_title !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_department_chk CHECK (char_length(department) <= 160 AND department !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_phone_number_chk CHECK (char_length(phone_number) <= 64 AND phone_number !~ '[[:cntrl:]]'),
  CONSTRAINT user_profiles_picture_upload_chk CHECK (picture_upload_id IS NULL OR picture_upload_id ~ '^upl_[0-9a-f]{32}$'),
  CONSTRAINT user_profiles_version_chk CHECK (version >= 1),
  CONSTRAINT user_profiles_mutation_key_chk CHECK (last_mutation_key = '' OR (char_length(last_mutation_key) BETWEEN 1 AND 128 AND last_mutation_key !~ '[[:cntrl:]]')),
  CONSTRAINT user_profiles_mutation_hash_chk CHECK (last_mutation_hash = '' OR last_mutation_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT user_profiles_mutation_pair_chk CHECK ((last_mutation_key = '' AND last_mutation_hash = '') OR (last_mutation_key <> '' AND last_mutation_hash <> '')),
  CONSTRAINT user_profiles_time_chk CHECK (updated_at >= created_at)
);

CREATE INDEX user_profiles_tenant_updated_idx
  ON user_profiles (organization_id, workspace_id, updated_at DESC, subject_ref);

ALTER TABLE user_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY user_profiles_tenant_select ON user_profiles
  FOR SELECT
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );
CREATE POLICY user_profiles_tenant_insert ON user_profiles
  FOR INSERT
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );
CREATE POLICY user_profiles_tenant_update ON user_profiles
  FOR UPDATE
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

CREATE FUNCTION user_profiles_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.subject_ref<>OLD.subject_ref OR NEW.username<>OLD.username OR NEW.email<>OLD.email OR NEW.created_at<>OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''user profile identity is provider-owned'';
  END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''user profile version/time must advance exactly once'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER user_profiles_guard_update BEFORE UPDATE ON user_profiles
  FOR EACH ROW EXECUTE FUNCTION user_profiles_guard_update();

REVOKE DELETE, TRUNCATE ON user_profiles FROM PUBLIC;
CREATE FUNCTION user_profiles_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''user profile records are retained for audit and privacy workflows'';
  RETURN NULL;
END';
CREATE TRIGGER user_profiles_no_delete BEFORE DELETE ON user_profiles
  FOR EACH ROW EXECUTE FUNCTION user_profiles_reject_delete();
CREATE TRIGGER user_profiles_no_clear BEFORE TRUNCATE ON user_profiles
  FOR EACH STATEMENT EXECUTE FUNCTION user_profiles_reject_delete();

COMMENT ON TABLE user_profiles IS 'Tenant-scoped current-user profile projection. OIDC username/email are provider-owned; edits to profile fields are versioned and audited.';
COMMENT ON COLUMN user_profiles.subject_ref IS 'One-way issuer+subject reference; raw OIDC subject is prohibited.';
COMMENT ON COLUMN user_profiles.picture_upload_id IS 'Released image upload only; profile removal disassociates the object while upload security evidence remains retained.';

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
