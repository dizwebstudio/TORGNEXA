BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE secret_references DROP CONSTRAINT secret_references_class_chk;
ALTER TABLE secret_references ADD CONSTRAINT secret_references_class_chk CHECK (class IN (
  'connector_token','oauth_client','oauth_state','oauth_refresh','erp_credential',
  'webhook_signing','certificate','storage_credential'
));

CREATE TABLE connector_oauth_sessions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL CHECK (account_version >= 1),
  actor_id text NOT NULL CHECK (actor_id=btrim(actor_id) AND char_length(actor_id) BETWEEN 1 AND 512),
  state_sha256 text NOT NULL CHECK (state_sha256 ~ '^[0-9a-f]{64}$'),
  pending_secret_reference text NOT NULL,
  callback_url text NOT NULL CHECK (callback_url=btrim(callback_url) AND char_length(callback_url) BETWEEN 9 AND 2048),
  correlation_id text NOT NULL CHECK (correlation_id=btrim(correlation_id) AND char_length(correlation_id) BETWEEN 1 AND 128),
  status text NOT NULL CHECK (status IN ('pending','consumed')),
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,connector_account_id)
    REFERENCES connector_accounts (organization_id,workspace_id,id),
  FOREIGN KEY (pending_secret_reference,organization_id,workspace_id)
    REFERENCES secret_references (reference,organization_id,workspace_id),
  UNIQUE (organization_id,workspace_id,state_sha256),
  UNIQUE (organization_id,workspace_id,actor_id,correlation_id),
  CHECK (expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
  CHECK ((status='pending' AND consumed_at IS NULL) OR (status='consumed' AND consumed_at IS NOT NULL))
);

CREATE INDEX connector_oauth_sessions_expiry_idx
  ON connector_oauth_sessions (organization_id,workspace_id,status,expires_at);

ALTER TABLE connector_oauth_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_oauth_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_oauth_sessions_tenant_all ON connector_oauth_sessions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON connector_oauth_sessions FROM PUBLIC;

CREATE FUNCTION connector_oauth_session_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id
     OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.account_version<>OLD.account_version
     OR NEW.actor_id<>OLD.actor_id OR NEW.state_sha256<>OLD.state_sha256
     OR NEW.pending_secret_reference<>OLD.pending_secret_reference OR NEW.callback_url<>OLD.callback_url
     OR NEW.correlation_id<>OLD.correlation_id OR NEW.created_at<>OLD.created_at OR NEW.expires_at<>OLD.expires_at
     OR OLD.status<>''pending'' OR NEW.status<>''consumed'' OR OLD.consumed_at IS NOT NULL
     OR NEW.consumed_at IS NULL OR NEW.consumed_at<OLD.created_at OR NEW.consumed_at>OLD.expires_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''connector oauth session transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_oauth_sessions_transition_guard BEFORE UPDATE ON connector_oauth_sessions
  FOR EACH ROW EXECUTE FUNCTION connector_oauth_session_transition();

COMMENT ON TABLE connector_oauth_sessions IS 'One-time OAuth state evidence. Plain state and PKCE verifier exist only in encrypted SecretProvider material.';
COMMENT ON COLUMN connector_oauth_sessions.state_sha256 IS 'SHA-256 digest only; raw OAuth state is forbidden in PostgreSQL.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
