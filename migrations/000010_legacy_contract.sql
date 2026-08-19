BEGIN;

-- TORGNEXA pre-v1 baseline component 000010: legacy_contract.
-- Squashed, statement-order-preserving source range: legacy 000064..000064.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000064_retire_legacy_inbox_events.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The real consumer inbox has used inbox_receipts since migration 000008.
-- Validate an impossible constraint before dropping the compatibility table:
-- validation succeeds only when no legacy row exists and otherwise rolls the
-- entire contract migration back without losing evidence.
LOCK TABLE inbox_events IN ACCESS EXCLUSIVE MODE;
ALTER TABLE inbox_events
  ADD CONSTRAINT inbox_events_retirement_empty_chk CHECK (false) NOT VALID;
ALTER TABLE inbox_events
  VALIDATE CONSTRAINT inbox_events_retirement_empty_chk;
DROP TABLE inbox_events;
-- BASELINE_SOURCE_END

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
