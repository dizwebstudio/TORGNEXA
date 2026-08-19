BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE settings_identity_providers (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  current_revision bigint NOT NULL CHECK (current_revision >= 1),
  active_revision bigint,
  enabled boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  last_correlation_id text NOT NULL CHECK (char_length(last_correlation_id) BETWEEN 1 AND 128),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,provider_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id),
  CHECK (provider_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
  CHECK (active_revision IS NULL OR active_revision >= 1),
  CHECK (NOT enabled OR active_revision IS NOT NULL)
);

CREATE TABLE settings_identity_provider_revisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision >= 1),
  protocol text NOT NULL CHECK (protocol = 'oidc'),
  display_name text NOT NULL CHECK (display_name=btrim(display_name) AND char_length(display_name) BETWEEN 1 AND 160),
  issuer_url text NOT NULL CHECK (issuer_url=btrim(issuer_url) AND char_length(issuer_url) BETWEEN 9 AND 2048),
  client_id text NOT NULL CHECK (client_id=btrim(client_id) AND char_length(client_id) BETWEEN 1 AND 256),
  callback_url text NOT NULL CHECK (callback_url=btrim(callback_url) AND char_length(callback_url) BETWEEN 9 AND 2048),
  client_secret_reference text,
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,provider_id,revision),
  FOREIGN KEY (organization_id,workspace_id,provider_id) REFERENCES settings_identity_providers (organization_id,workspace_id,provider_id),
  FOREIGN KEY (client_secret_reference,organization_id,workspace_id) REFERENCES secret_references (reference,organization_id,workspace_id)
);

ALTER TABLE settings_identity_providers
  ADD CONSTRAINT settings_identity_providers_current_revision_fk
    FOREIGN KEY (organization_id,workspace_id,provider_id,current_revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision)
    DEFERRABLE INITIALLY DEFERRED,
  ADD CONSTRAINT settings_identity_providers_active_revision_fk
    FOREIGN KEY (organization_id,workspace_id,provider_id,active_revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE settings_identity_provider_validations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  revision bigint NOT NULL,
  status text NOT NULL CHECK (status IN ('valid','invalid')),
  reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  metadata_digest text CHECK (metadata_digest IS NULL OR metadata_digest ~ '^[0-9a-f]{64}$'),
  issuer_url text,
  authorization_url text,
  token_url text,
  jwks_url text,
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
  checked_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,provider_id,revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision),
  UNIQUE (organization_id,workspace_id,provider_id,correlation_id),
  CHECK ((status='valid' AND reason_code='validated' AND metadata_digest IS NOT NULL AND issuer_url IS NOT NULL AND authorization_url IS NOT NULL AND token_url IS NOT NULL AND jwks_url IS NOT NULL)
      OR (status='invalid' AND metadata_digest IS NULL AND issuer_url IS NULL AND authorization_url IS NULL AND token_url IS NULL AND jwks_url IS NULL))
);

CREATE INDEX settings_identity_providers_list_idx ON settings_identity_providers (organization_id,workspace_id,provider_id);
CREATE INDEX settings_identity_provider_validations_latest_idx ON settings_identity_provider_validations (organization_id,workspace_id,provider_id,revision,checked_at DESC,id DESC);

ALTER TABLE settings_identity_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_providers_tenant_all ON settings_identity_providers
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_identity_provider_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_provider_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_provider_revisions_tenant_all ON settings_identity_provider_revisions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_identity_provider_validations ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_provider_validations FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_provider_validations_tenant_all ON settings_identity_provider_validations
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON settings_identity_providers FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON settings_identity_provider_revisions,settings_identity_provider_validations FROM PUBLIC;

CREATE FUNCTION settings_identity_provider_reject_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''identity provider evidence is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER settings_identity_provider_revisions_no_update_delete BEFORE UPDATE OR DELETE ON settings_identity_provider_revisions FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_revisions_no_clear BEFORE TRUNCATE ON settings_identity_provider_revisions FOR EACH STATEMENT EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_validations_no_update_delete BEFORE UPDATE OR DELETE ON settings_identity_provider_validations FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_validations_no_clear BEFORE TRUNCATE ON settings_identity_provider_validations FOR EACH STATEMENT EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();

CREATE FUNCTION settings_identity_provider_head_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.provider_id<>OLD.provider_id OR NEW.version<>OLD.version+1 OR NEW.current_revision<OLD.current_revision OR NEW.current_revision>OLD.current_revision+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''identity provider transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER settings_identity_provider_head_guard BEFORE UPDATE ON settings_identity_providers FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_head_transition();

COMMENT ON TABLE settings_identity_provider_revisions IS 'Immutable provider-neutral OIDC configuration revisions. VK and other providers are labels/configuration, never Core branches.';
COMMENT ON COLUMN settings_identity_provider_revisions.client_secret_reference IS 'Opaque SecretProvider reference; client secret plaintext is forbidden.';
COMMENT ON TABLE settings_identity_provider_validations IS 'Append-only bounded discovery validation evidence; provider response bodies are not stored.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
