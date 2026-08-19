BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE UNIQUE INDEX connector_accounts_sync_tenant_identity_uq
  ON connector_accounts (id, organization_id, workspace_id);

CREATE TABLE sync_policies (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  entity_type text NOT NULL,
  direction text NOT NULL,
  source_of_truth text NOT NULL,
  enabled boolean NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT sync_policies_pkey PRIMARY KEY (id),
  CONSTRAINT sync_policies_tenant_identity UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT sync_policies_account_fk FOREIGN KEY (connector_account_id, organization_id, workspace_id)
    REFERENCES connector_accounts (id, organization_id, workspace_id),
  CONSTRAINT sync_policies_entity_type_chk CHECK (entity_type ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT sync_policies_direction_chk CHECK (direction IN ('inbound','outbound','bidirectional')),
  CONSTRAINT sync_policies_source_truth_chk CHECK (source_of_truth IN ('local','remote','manual')),
  CONSTRAINT sync_policies_version_chk CHECK (version >= 1),
  CONSTRAINT sync_policies_time_chk CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX sync_policies_account_entity_uq
  ON sync_policies (organization_id, workspace_id, connector_account_id, entity_type);

CREATE TABLE sync_checkpoints (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  cursor text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT sync_checkpoints_pkey PRIMARY KEY (organization_id, workspace_id, policy_id),
  CONSTRAINT sync_checkpoints_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_checkpoints_version_chk CHECK (version >= 1),
  CONSTRAINT sync_checkpoints_cursor_chk CHECK (length(cursor) <= 1024 AND cursor !~ '[[:cntrl:]]')
);

CREATE TABLE sync_entity_states (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  local_entity_id text NOT NULL,
  remote_id text NOT NULL,
  last_local_version bigint NOT NULL,
  last_remote_revision text NOT NULL,
  last_synced_fingerprint text NOT NULL,
  last_local_event_id text NOT NULL,
  last_remote_change_id text,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL,
  CONSTRAINT sync_entity_states_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, local_entity_id),
  CONSTRAINT sync_entity_states_remote_uq UNIQUE (organization_id, workspace_id, policy_id, remote_id),
  CONSTRAINT sync_entity_states_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_entity_states_local_version_chk CHECK (last_local_version >= 1),
  CONSTRAINT sync_entity_states_fingerprint_chk CHECK (last_synced_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_entity_states_revision_chk CHECK (length(last_remote_revision) BETWEEN 1 AND 256 AND last_remote_revision !~ '[[:cntrl:]]'),
  CONSTRAINT sync_entity_states_remote_id_chk CHECK (length(remote_id) BETWEEN 1 AND 512 AND remote_id !~ '[[:cntrl:]]'),
  CONSTRAINT sync_entity_states_version_chk CHECK (version >= 1)
);

CREATE TABLE sync_local_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  change_id text NOT NULL,
  fingerprint text NOT NULL,
  outcome text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT sync_local_receipts_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, change_id),
  CONSTRAINT sync_local_receipts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_local_receipts_fingerprint_chk CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_local_receipts_outcome_chk CHECK (outcome IN ('applied','duplicate','loop_suppressed','stale_suppressed','conflict_local_wins'))
);

CREATE TABLE sync_remote_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  change_id text NOT NULL,
  fingerprint text NOT NULL,
  outcome text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT sync_remote_receipts_pkey PRIMARY KEY (organization_id, workspace_id, policy_id, change_id),
  CONSTRAINT sync_remote_receipts_policy_fk FOREIGN KEY (policy_id, organization_id, workspace_id)
    REFERENCES sync_policies (id, organization_id, workspace_id),
  CONSTRAINT sync_remote_receipts_fingerprint_chk CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT sync_remote_receipts_outcome_chk CHECK (outcome IN ('applied','duplicate','loop_suppressed','stale_suppressed','conflict_local_wins'))
);

CREATE INDEX sync_entity_states_remote_revision_idx
  ON sync_entity_states (organization_id, workspace_id, policy_id, last_remote_revision, local_entity_id);
CREATE INDEX sync_local_receipts_created_idx
  ON sync_local_receipts (organization_id, workspace_id, policy_id, created_at, change_id);
CREATE INDEX sync_remote_receipts_created_idx
  ON sync_remote_receipts (organization_id, workspace_id, policy_id, created_at, change_id);

ALTER TABLE sync_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_checkpoints FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_entity_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_entity_states FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_local_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_local_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE sync_remote_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_remote_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY sync_policies_tenant_all ON sync_policies
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_checkpoints_tenant_all ON sync_checkpoints
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_entity_states_tenant_all ON sync_entity_states
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_local_receipts_tenant_all ON sync_local_receipts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY sync_remote_receipts_tenant_all ON sync_remote_receipts
  USING (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true))
  WITH CHECK (organization_id = current_setting('app.organization_id', true) AND workspace_id = current_setting('app.workspace_id', true));

CREATE FUNCTION sync_policy_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy must start at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id
     OR NEW.entity_type IS DISTINCT FROM OLD.entity_type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync policy version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER sync_policy_guard_insert BEFORE INSERT ON sync_policies FOR EACH ROW EXECUTE FUNCTION sync_policy_guard();
CREATE TRIGGER sync_policy_guard_update BEFORE UPDATE ON sync_policies FOR EACH ROW EXECUTE FUNCTION sync_policy_guard();

CREATE FUNCTION sync_state_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity state must start at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.policy_id IS DISTINCT FROM OLD.policy_id OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id
     OR NEW.remote_id IS DISTINCT FROM OLD.remote_id THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR NEW.last_local_version < OLD.last_local_version THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync entity state progression is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER sync_state_guard_insert BEFORE INSERT ON sync_entity_states FOR EACH ROW EXECUTE FUNCTION sync_state_guard();
CREATE TRIGGER sync_state_guard_update BEFORE UPDATE ON sync_entity_states FOR EACH ROW EXECUTE FUNCTION sync_state_guard();

CREATE FUNCTION sync_receipt_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''sync receipt history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER sync_local_receipts_no_update BEFORE UPDATE OR DELETE ON sync_local_receipts FOR EACH ROW EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_remote_receipts_no_update BEFORE UPDATE OR DELETE ON sync_remote_receipts FOR EACH ROW EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_local_receipts_no_clear BEFORE TRUNCATE ON sync_local_receipts FOR EACH STATEMENT EXECUTE FUNCTION sync_receipt_reject_mutation();
CREATE TRIGGER sync_remote_receipts_no_clear BEFORE TRUNCATE ON sync_remote_receipts FOR EACH STATEMENT EXECUTE FUNCTION sync_receipt_reject_mutation();

REVOKE DELETE, TRUNCATE ON sync_policies, sync_checkpoints, sync_entity_states FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON sync_local_receipts, sync_remote_receipts FROM PUBLIC;

COMMENT ON TABLE sync_policies IS 'Task-013 provider-neutral direction and source-of-truth policy; provider names are forbidden from sync semantics.';
COMMENT ON TABLE sync_checkpoints IS 'Task-013 durable remote pull cursor; advanced only after a complete page is resolved.';
COMMENT ON TABLE sync_entity_states IS 'Task-013 last synchronized local/remote versions and canonical payload fingerprint for conflict and loop prevention.';
COMMENT ON TABLE sync_local_receipts IS 'Task-013 append-only outbound event replay receipts.';
COMMENT ON TABLE sync_remote_receipts IS 'Task-013 append-only inbound remote-change replay receipts.';

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
