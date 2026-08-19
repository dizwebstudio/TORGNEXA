BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE webhook_subscriptions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  endpoint_url text NOT NULL,
  event_types jsonb NOT NULL,
  status text NOT NULL DEFAULT 'active',
  signing_secret_reference text NOT NULL,
  previous_signing_secret_reference text,
  previous_valid_until timestamptz,
  consecutive_failures integer NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT webhook_subscriptions_pkey PRIMARY KEY (id),
  CONSTRAINT webhook_subscriptions_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT webhook_subscriptions_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_secret_fk FOREIGN KEY (signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_previous_secret_fk FOREIGN KEY (previous_signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_subscriptions_id_chk CHECK (id = btrim(id) AND char_length(id) BETWEEN 1 AND 128 AND id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT webhook_subscriptions_endpoint_chk CHECK (endpoint_url = btrim(endpoint_url) AND char_length(endpoint_url) BETWEEN 1 AND 2048 AND endpoint_url ~ '^https://'),
  CONSTRAINT webhook_subscriptions_events_chk CHECK (jsonb_typeof(event_types)='array' AND jsonb_array_length(event_types) BETWEEN 1 AND 128),
  CONSTRAINT webhook_subscriptions_status_chk CHECK (status IN ('active','disabled')),
  CONSTRAINT webhook_subscriptions_failure_chk CHECK (consecutive_failures >= 0),
  CONSTRAINT webhook_subscriptions_rotation_chk CHECK ((previous_signing_secret_reference IS NULL) = (previous_valid_until IS NULL)),
  CONSTRAINT webhook_subscriptions_time_chk CHECK (updated_at >= created_at),
  CONSTRAINT webhook_subscriptions_version_chk CHECK (version >= 1)
);

CREATE TABLE webhook_deliveries (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  subscription_id text NOT NULL,
  event_id text NOT NULL,
  event_type text NOT NULL,
  endpoint_url text NOT NULL,
  signing_secret_reference text NOT NULL,
  body jsonb NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  attempt integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL,
  lease_token text,
  lease_expires_at timestamptz,
  replay_of text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  succeeded_at timestamptz,
  dlq_at timestamptz,
  last_http_status integer,
  last_error_code text,
  CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id),
  CONSTRAINT webhook_deliveries_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT webhook_deliveries_subscription_fk FOREIGN KEY (subscription_id, organization_id, workspace_id)
    REFERENCES webhook_subscriptions (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_secret_fk FOREIGN KEY (signing_secret_reference, organization_id, workspace_id)
    REFERENCES secret_references (reference, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_replay_fk FOREIGN KEY (replay_of, organization_id, workspace_id)
    REFERENCES webhook_deliveries (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_deliveries_body_chk CHECK (jsonb_typeof(body) = 'object' AND octet_length(body::text) <= 1081344),
  CONSTRAINT webhook_deliveries_status_chk CHECK (status IN ('pending','inflight','succeeded','dlq')),
  CONSTRAINT webhook_deliveries_attempt_chk CHECK (attempt >= 0 AND attempt <= 32),
  CONSTRAINT webhook_deliveries_lease_chk CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL)),
  CONSTRAINT webhook_deliveries_terminal_chk CHECK (
    (status='succeeded' AND succeeded_at IS NOT NULL AND dlq_at IS NULL) OR
    (status='dlq' AND dlq_at IS NOT NULL AND succeeded_at IS NULL) OR
    (status IN ('pending','inflight') AND succeeded_at IS NULL AND dlq_at IS NULL)
  ),
  CONSTRAINT webhook_deliveries_error_code_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT webhook_deliveries_http_status_chk CHECK (last_http_status IS NULL OR last_http_status BETWEEN 100 AND 599),
  CONSTRAINT webhook_deliveries_time_chk CHECK (updated_at >= created_at AND available_at >= created_at)
);
CREATE UNIQUE INDEX webhook_deliveries_initial_event_uniq
  ON webhook_deliveries (organization_id, workspace_id, subscription_id, event_id)
  WHERE replay_of IS NULL;
CREATE INDEX webhook_deliveries_claim_idx
  ON webhook_deliveries (organization_id, workspace_id, available_at, created_at, id)
  WHERE status IN ('pending','inflight');
CREATE INDEX webhook_deliveries_history_idx
  ON webhook_deliveries (organization_id, workspace_id, subscription_id, created_at DESC, id DESC);

CREATE TABLE webhook_delivery_attempts (
  delivery_id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  attempt integer NOT NULL,
  outcome text NOT NULL,
  http_status integer,
  duration_ms bigint NOT NULL,
  error_code text,
  completed_at timestamptz NOT NULL,
  CONSTRAINT webhook_delivery_attempts_pkey PRIMARY KEY (delivery_id, attempt),
  CONSTRAINT webhook_delivery_attempts_delivery_fk FOREIGN KEY (delivery_id, organization_id, workspace_id)
    REFERENCES webhook_deliveries (id, organization_id, workspace_id) ON DELETE RESTRICT,
  CONSTRAINT webhook_delivery_attempts_attempt_chk CHECK (attempt BETWEEN 1 AND 32),
  CONSTRAINT webhook_delivery_attempts_outcome_chk CHECK (outcome IN ('succeeded','retry','dlq')),
  CONSTRAINT webhook_delivery_attempts_status_chk CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
  CONSTRAINT webhook_delivery_attempts_duration_chk CHECK (duration_ms >= 0 AND duration_ms <= 3600000),
  CONSTRAINT webhook_delivery_attempts_error_chk CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_]{0,63}$')
);
CREATE INDEX webhook_delivery_attempts_tenant_history_idx
  ON webhook_delivery_attempts (organization_id, workspace_id, delivery_id, attempt DESC);

ALTER TABLE webhook_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_subscriptions FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_subscriptions_tenant_all ON webhook_subscriptions FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_deliveries_tenant_all ON webhook_deliveries FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_delivery_attempts_tenant_select ON webhook_delivery_attempts FOR SELECT
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY webhook_delivery_attempts_tenant_insert ON webhook_delivery_attempts FOR INSERT
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON webhook_subscriptions, webhook_deliveries, webhook_delivery_attempts FROM PUBLIC;
REVOKE UPDATE ON webhook_delivery_attempts FROM PUBLIC;

CREATE FUNCTION webhook_deliveries_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id
     OR NEW.subscription_id<>OLD.subscription_id OR NEW.event_id<>OLD.event_id OR NEW.event_type<>OLD.event_type
     OR NEW.endpoint_url<>OLD.endpoint_url OR NEW.signing_secret_reference<>OLD.signing_secret_reference
     OR NEW.body<>OLD.body OR NEW.replay_of IS DISTINCT FROM OLD.replay_of OR NEW.created_at<>OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery identity and request snapshot are immutable'';
  END IF;
  IF NEW.attempt < OLD.attempt THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery attempt cannot decrease'';
  END IF;
  IF OLD.status IN (''succeeded'',''dlq'') AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''terminal webhook delivery is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER webhook_deliveries_update_guard BEFORE UPDATE ON webhook_deliveries
  FOR EACH ROW EXECUTE FUNCTION webhook_deliveries_guard_update();

CREATE FUNCTION webhook_attempts_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''webhook delivery attempts are immutable'';
  RETURN NULL;
END';
CREATE TRIGGER webhook_attempts_no_update_delete BEFORE UPDATE OR DELETE ON webhook_delivery_attempts
  FOR EACH ROW EXECUTE FUNCTION webhook_attempts_reject_mutation();
CREATE TRIGGER webhook_attempts_no_clear BEFORE TRUNCATE ON webhook_delivery_attempts
  FOR EACH STATEMENT EXECUTE FUNCTION webhook_attempts_reject_mutation();

COMMENT ON TABLE webhook_deliveries IS 'Durable immutable webhook request snapshots with mutable operational delivery state; at-least-once attempts are recorded separately.';
COMMENT ON COLUMN webhook_deliveries.body IS 'Canonical webhook envelope only. Never stores response bodies, Authorization headers, plaintext signing material or arbitrary remote error text.';
COMMENT ON TABLE webhook_delivery_attempts IS 'Immutable bounded delivery history containing only outcome/status/duration/machine error code; remote response bodies and raw errors are forbidden.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
