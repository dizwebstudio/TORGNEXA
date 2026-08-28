BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 141: admit Claude (Anthropic) to the AI provider account boundary.
-- Existing accounts and credentials remain unchanged; this only widens the
-- provider allow-list for the additive expand phase.
ALTER TABLE ai_provider_accounts DROP CONSTRAINT ai_provider_accounts_provider_check;
ALTER TABLE ai_provider_accounts ADD CONSTRAINT ai_provider_accounts_provider_check CHECK (
  provider IN ('openai-compatible', 'gigachat', 'yandexgpt', 'kimi', 'qwen', 'deepseek', 'claude')
);

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
