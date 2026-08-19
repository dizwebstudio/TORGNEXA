BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task-122 introduced secrets.ClassAIProviderCredential for AI provider
-- account credentials but the immutable pre-v1 baseline's CHECK constraint
-- on secret_references.class predates it. Widen the constraint the same way
-- 000011 widened it for erp_credential/webhook_signing/etc: this is a purely
-- additive expand-phase change, old readers/writers are unaffected.
ALTER TABLE secret_references DROP CONSTRAINT secret_references_class_chk;
ALTER TABLE secret_references ADD CONSTRAINT secret_references_class_chk CHECK (class IN (
  'connector_token','oauth_client','oauth_state','oauth_refresh','erp_credential',
  'webhook_signing','certificate','storage_credential','ai_provider_credential'
));

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
