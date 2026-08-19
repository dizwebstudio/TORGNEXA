BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Keep the pre-Task-009 inbox_events placeholder untouched and deny-all for
-- rolling compatibility. Runtime Task-009 code uses this new tenant-scoped,
-- immutable receipt table. A later contract migration may retire the placeholder.
CREATE TABLE inbox_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  consumer text NOT NULL,
  event_id text NOT NULL,
  event_type text NOT NULL,
  event_fingerprint text NOT NULL,
  first_observed_at timestamptz NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  processed_attempt integer NOT NULL,
  CONSTRAINT inbox_receipts_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT inbox_receipts_consumer_chk CHECK (
    consumer ~ '^[a-z][a-z0-9._:-]{0,127}$'
  ),
  CONSTRAINT inbox_receipts_event_id_chk CHECK (
    event_id = btrim(event_id)
    AND char_length(event_id) BETWEEN 1 AND 128
    AND event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
  ),
  CONSTRAINT inbox_receipts_event_type_chk CHECK (
    event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$'
  ),
  CONSTRAINT inbox_receipts_fingerprint_chk CHECK (
    event_fingerprint ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT inbox_receipts_attempt_chk CHECK (
    processed_attempt BETWEEN 1 AND 1000
  ),
  PRIMARY KEY (organization_id, workspace_id, consumer, event_id)
);

CREATE INDEX inbox_receipts_consumer_processed_idx
  ON inbox_receipts (organization_id, workspace_id, consumer, processed_at DESC, event_id);

ALTER TABLE inbox_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbox_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY inbox_receipts_tenant_select ON inbox_receipts FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY inbox_receipts_tenant_insert ON inbox_receipts FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE UPDATE, DELETE, TRUNCATE ON inbox_receipts FROM PUBLIC;

CREATE FUNCTION inbox_receipts_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''inbox receipts are immutable after processing'';
  RETURN NULL;
END';

CREATE TRIGGER inbox_receipts_no_update
  BEFORE UPDATE ON inbox_receipts FOR EACH ROW EXECUTE FUNCTION inbox_receipts_reject_mutation();
CREATE TRIGGER inbox_receipts_no_delete
  BEFORE DELETE ON inbox_receipts FOR EACH ROW EXECUTE FUNCTION inbox_receipts_reject_mutation();
CREATE TRIGGER inbox_receipts_no_clear
  BEFORE TRUNCATE ON inbox_receipts FOR EACH STATEMENT EXECUTE FUNCTION inbox_receipts_reject_mutation();

COMMENT ON TABLE inbox_receipts IS 'Tenant-scoped immutable consumer idempotency receipts. Business PostgreSQL side effects and receipt insert commit in the same transaction after a transaction-scoped advisory lock.';
COMMENT ON COLUMN inbox_receipts.consumer IS 'Stable logical consumer identity, not an ephemeral pod/member ID. Change/version it deliberately when replay semantics must change.';
COMMENT ON COLUMN inbox_receipts.event_fingerprint IS 'SHA-256 of the canonical immutable EventBus envelope; detects event-ID reuse with different content without duplicating business payload/PII into inbox storage.';
COMMENT ON COLUMN inbox_receipts.processed_attempt IS 'EventBus delivery attempt that committed the transactional business effect. This is observability metadata, not a retry counter owned by the inbox.';
COMMENT ON TABLE inbox_events IS 'Pre-Task-009 compatibility placeholder retained deny-all during expand phase. New runtime code uses inbox_receipts; retire only in a later contract migration after fleet qualification.';

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
