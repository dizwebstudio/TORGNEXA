BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 103 deliberately stores irreversible references only. Raw OIDC sid/sub,
-- bearer tokens, IP addresses and User-Agent strings never enter these tables.
CREATE TABLE settings_identity_sessions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  session_ref text NOT NULL,
  subject_ref text NOT NULL,
  status text NOT NULL,
  client_kind text NOT NULL,
  authenticated_at timestamptz NOT NULL,
  first_seen_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CONSTRAINT settings_identity_sessions_pkey PRIMARY KEY (organization_id,workspace_id,session_ref),
  CONSTRAINT settings_identity_sessions_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id),
  CONSTRAINT settings_identity_sessions_ref_chk CHECK (session_ref ~ '^[0-9a-f]{64}$' AND subject_ref ~ '^[0-9a-f]{64}$'),
  CONSTRAINT settings_identity_sessions_status_chk CHECK (status IN ('active','revoked')),
  CONSTRAINT settings_identity_sessions_client_chk CHECK (client_kind IN ('browser','mobile','api','unknown')),
  CONSTRAINT settings_identity_sessions_time_chk CHECK (authenticated_at <= first_seen_at AND first_seen_at <= last_seen_at AND expires_at > authenticated_at),
  CONSTRAINT settings_identity_sessions_revoke_chk CHECK ((status='active' AND revoked_at IS NULL) OR (status='revoked' AND revoked_at IS NOT NULL))
);
CREATE INDEX settings_identity_sessions_recent_idx ON settings_identity_sessions (organization_id,workspace_id,last_seen_at DESC,session_ref DESC);

CREATE TABLE settings_login_events (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  session_ref text NOT NULL,
  event_type text NOT NULL,
  client_kind text NOT NULL,
  occurred_at timestamptz NOT NULL,
  CONSTRAINT settings_login_events_pkey PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT settings_login_events_session_fk FOREIGN KEY (organization_id,workspace_id,session_ref) REFERENCES settings_identity_sessions (organization_id,workspace_id,session_ref),
  CONSTRAINT settings_login_events_unique_kind UNIQUE (organization_id,workspace_id,session_ref,event_type),
  CONSTRAINT settings_login_events_id_chk CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  CONSTRAINT settings_login_events_type_chk CHECK (event_type IN ('session_observed','session_revoked')),
  CONSTRAINT settings_login_events_client_chk CHECK (client_kind IN ('browser','mobile','api','unknown'))
);
CREATE INDEX settings_login_events_recent_idx ON settings_login_events (organization_id,workspace_id,id DESC);

ALTER TABLE settings_identity_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_sessions_tenant_isolation ON settings_identity_sessions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_login_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_login_events FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_login_events_tenant_isolation ON settings_login_events
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON settings_identity_sessions FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON settings_login_events FROM PUBLIC;

CREATE FUNCTION settings_identity_sessions_enforce_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.session_ref<>OLD.session_ref OR NEW.subject_ref<>OLD.subject_ref OR NEW.client_kind<>OLD.client_kind OR NEW.authenticated_at<>OLD.authenticated_at OR NEW.first_seen_at<>OLD.first_seen_at OR NEW.last_seen_at<OLD.last_seen_at OR NEW.expires_at<OLD.expires_at OR OLD.status=''revoked'' OR (OLD.status=''active'' AND NEW.status NOT IN (''active'',''revoked'')) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid identity session transition'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER settings_identity_sessions_transition BEFORE UPDATE ON settings_identity_sessions FOR EACH ROW EXECUTE FUNCTION settings_identity_sessions_enforce_transition();

CREATE FUNCTION settings_security_reject_event_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''settings login events are append-only''; RETURN NULL; END';
CREATE TRIGGER settings_login_events_no_update BEFORE UPDATE ON settings_login_events FOR EACH ROW EXECUTE FUNCTION settings_security_reject_event_mutation();
CREATE TRIGGER settings_login_events_no_delete BEFORE DELETE ON settings_login_events FOR EACH ROW EXECUTE FUNCTION settings_security_reject_event_mutation();
CREATE TRIGGER settings_login_events_no_clear BEFORE TRUNCATE ON settings_login_events FOR EACH STATEMENT EXECUTE FUNCTION settings_security_reject_event_mutation();

COMMENT ON TABLE settings_identity_sessions IS 'Minimized tenant-scoped OIDC sessions observed by TORGNEXA; application revocation does not claim provider-wide logout.';
COMMENT ON COLUMN settings_identity_sessions.session_ref IS 'SHA-256 of issuer, subject and OIDC sid (or issuance fallback); raw provider identifiers are prohibited.';
COMMENT ON COLUMN settings_identity_sessions.subject_ref IS 'SHA-256 of issuer and OIDC sub; display and logging are prohibited.';
COMMENT ON TABLE settings_login_events IS 'Append-only TORGNEXA-observed session history. Retention class: security evidence, 180 days minimum, policy-controlled thereafter.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
