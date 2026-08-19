BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE outbox_events
  ADD COLUMN event_envelope jsonb,
  ADD COLUMN available_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN lease_owner text,
  ADD COLUMN lease_token text,
  ADD COLUMN lease_expires_at timestamptz,
  ADD COLUMN last_attempt_at timestamptz,
  ADD COLUMN last_error_code text;

ALTER TABLE outbox_events
  ADD CONSTRAINT outbox_events_attempts_chk CHECK (attempts >= 0) NOT VALID,
  ADD CONSTRAINT outbox_events_event_type_chk CHECK (
    event_envelope IS NULL OR event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$'
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_aggregate_type_chk CHECK (
    event_envelope IS NULL OR (
    aggregate_type = btrim(aggregate_type)
    AND char_length(aggregate_type) BETWEEN 1 AND 128
    AND aggregate_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_aggregate_id_chk CHECK (
    event_envelope IS NULL OR (
    aggregate_id = btrim(aggregate_id)
    AND char_length(aggregate_id) BETWEEN 1 AND 128
    AND aggregate_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_payload_object_chk CHECK (event_envelope IS NULL OR jsonb_typeof(payload) = 'object') NOT VALID,
  ADD CONSTRAINT outbox_events_payload_size_chk CHECK (event_envelope IS NULL OR octet_length(payload::text) <= 1048576) NOT VALID,
  ADD CONSTRAINT outbox_events_envelope_chk CHECK (
    event_envelope IS NULL OR (
      jsonb_typeof(event_envelope) = 'object'
      AND octet_length(event_envelope::text) <= 1081344
      AND event_envelope ?& ARRAY[
        'event_id','event_type','occurred_at','organization_id','workspace_id',
        'correlation_id','causation_id','entity_type','entity_id','source','data'
      ]
      AND event_envelope->>'event_id' = id
      AND event_envelope->>'event_type' = event_type
      AND event_envelope->>'organization_id' = organization_id
      AND event_envelope->>'workspace_id' = workspace_id
      AND event_envelope->>'entity_type' = aggregate_type
      AND event_envelope->>'entity_id' = aggregate_id
      AND jsonb_typeof(event_envelope->'data') = 'object'
      AND event_envelope->'data' = payload
      AND (event_envelope->>'occurred_at') ~ 'Z$'
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_lease_chk CHECK (
    (lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
    OR (
      lease_owner = btrim(lease_owner)
      AND char_length(lease_owner) BETWEEN 1 AND 128
      AND lease_owner ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'
      AND lease_token ~ '^[0-9a-f]{32}$'
      AND lease_expires_at IS NOT NULL
      AND last_attempt_at IS NOT NULL
    )
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_publish_state_chk CHECK (
    published_at IS NULL OR (lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_error_code_chk CHECK (
    last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
  ) NOT VALID,
  ADD CONSTRAINT outbox_events_timestamps_chk CHECK (
    available_at >= created_at
    AND (last_attempt_at IS NULL OR last_attempt_at >= created_at)
    AND (published_at IS NULL OR published_at >= created_at)
  ) NOT VALID;

ALTER TABLE outbox_events
  VALIDATE CONSTRAINT outbox_events_attempts_chk,
  VALIDATE CONSTRAINT outbox_events_event_type_chk,
  VALIDATE CONSTRAINT outbox_events_aggregate_type_chk,
  VALIDATE CONSTRAINT outbox_events_aggregate_id_chk,
  VALIDATE CONSTRAINT outbox_events_payload_object_chk,
  VALIDATE CONSTRAINT outbox_events_payload_size_chk,
  VALIDATE CONSTRAINT outbox_events_envelope_chk,
  VALIDATE CONSTRAINT outbox_events_lease_chk,
  VALIDATE CONSTRAINT outbox_events_publish_state_chk,
  VALIDATE CONSTRAINT outbox_events_error_code_chk,
  VALIDATE CONSTRAINT outbox_events_timestamps_chk;

DROP INDEX outbox_events_unpublished_idx;
CREATE INDEX outbox_events_unpublished_idx
  ON outbox_events (organization_id, workspace_id, available_at, created_at, id)
  WHERE published_at IS NULL AND event_envelope IS NOT NULL;
CREATE INDEX outbox_events_lease_expiry_idx
  ON outbox_events (organization_id, workspace_id, lease_expires_at, id)
  WHERE published_at IS NULL AND lease_expires_at IS NOT NULL;

DROP POLICY outbox_events_tenant_isolation ON outbox_events;
CREATE POLICY outbox_events_tenant_select ON outbox_events FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY outbox_events_tenant_insert ON outbox_events FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY outbox_events_tenant_update ON outbox_events FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE DELETE, TRUNCATE ON outbox_events FROM PUBLIC;

CREATE FUNCTION outbox_events_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.event_type IS DISTINCT FROM OLD.event_type
     OR NEW.aggregate_type IS DISTINCT FROM OLD.aggregate_type
     OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
     OR NEW.payload IS DISTINCT FROM OLD.payload
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR (OLD.event_envelope IS NOT NULL AND NEW.event_envelope IS DISTINCT FROM OLD.event_envelope) THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox event identity and body are immutable'';
  END IF;
  IF OLD.published_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''published outbox event is immutable'';
  END IF;
  IF NEW.attempts < OLD.attempts THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox attempts cannot decrease'';
  END IF;
  IF OLD.published_at IS NULL AND NEW.published_at IS NOT NULL AND NEW.published_at < OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox published_at precedes creation'';
  END IF;
  RETURN NEW;
END';

CREATE TRIGGER outbox_events_update_guard
  BEFORE UPDATE ON outbox_events
  FOR EACH ROW EXECUTE FUNCTION outbox_events_guard_update();

CREATE FUNCTION outbox_events_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''outbox events cannot be deleted by application runtime'';
  RETURN NULL;
END';

CREATE TRIGGER outbox_events_no_delete
  BEFORE DELETE ON outbox_events FOR EACH ROW EXECUTE FUNCTION outbox_events_reject_delete();
CREATE TRIGGER outbox_events_no_clear
  BEFORE TRUNCATE ON outbox_events FOR EACH STATEMENT EXECUTE FUNCTION outbox_events_reject_delete();

COMMENT ON TABLE outbox_events IS 'Tenant-scoped transactional outbox. Domain state and event intent are inserted in one PostgreSQL transaction; relay uses short SKIP LOCKED leases and at-least-once publication.';
COMMENT ON COLUMN outbox_events.event_envelope IS 'Canonical immutable EventBus envelope. NULL is reserved for pre-Task-008 legacy rows and is never claimed by the new relay.';
COMMENT ON COLUMN outbox_events.lease_token IS 'Opaque compare-by-lease token. A stale relay cannot acknowledge or reschedule a row after lease loss.';
COMMENT ON COLUMN outbox_events.last_error_code IS 'Bounded machine code only; raw broker/client error text is forbidden because it may contain credentials or PII.';
COMMENT ON COLUMN outbox_events.published_at IS 'Set only after EventBus publish succeeds. Crash after publish but before this update may duplicate the immutable event ID; Task 009 consumer inbox performs deduplication.';

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
