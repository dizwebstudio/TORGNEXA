BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workspace_members (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  email text NOT NULL CHECK (email = lower(btrim(email)) AND char_length(email) BETWEEN 3 AND 254),
  display_name text NOT NULL DEFAULT '' CHECK (display_name = btrim(display_name) AND char_length(display_name) <= 160),
  oidc_subject text CHECK (oidc_subject IS NULL OR (oidc_subject = btrim(oidc_subject) AND char_length(oidc_subject) BETWEEN 1 AND 255)),
  role_code text NOT NULL CHECK (role_code IN ('admin','manager','operator','viewer')),
  status text NOT NULL CHECK (status IN ('invited','active','disabled')),
  invitation_key text NOT NULL CHECK (char_length(invitation_key) BETWEEN 1 AND 128),
  last_mutation_key text CHECK (last_mutation_key IS NULL OR char_length(last_mutation_key) BETWEEN 1 AND 128),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  invited_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces (organization_id, id),
  UNIQUE (organization_id, workspace_id, email),
  UNIQUE (organization_id, workspace_id, invitation_key),
  UNIQUE (organization_id, workspace_id, oidc_subject)
);
CREATE INDEX workspace_members_page_idx ON workspace_members (organization_id, workspace_id, id);
CREATE INDEX workspace_members_admin_idx ON workspace_members (organization_id, workspace_id, role_code, status);

ALTER TABLE workspace_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_members FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_members_tenant_all ON workspace_members
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE, TRUNCATE ON workspace_members FROM PUBLIC;

CREATE FUNCTION workspace_members_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.email IS DISTINCT FROM OLD.email OR NEW.invitation_key IS DISTINCT FROM OLD.invitation_key OR NEW.invited_at IS DISTINCT FROM OLD.invited_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER workspace_members_guard_insert BEFORE INSERT ON workspace_members FOR EACH ROW EXECUTE FUNCTION workspace_members_guard();
CREATE TRIGGER workspace_members_guard_update BEFORE UPDATE ON workspace_members FOR EACH ROW EXECUTE FUNCTION workspace_members_guard();

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
