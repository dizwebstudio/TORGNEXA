BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Admits qwen and deepseek as two further ai-family Connector SDK v1
-- providers on the Task-122 AI provider account capability, alongside
-- openai-compatible/gigachat/yandexgpt/kimi. Widening a CHECK constraint is
-- a purely additive expand-phase change: old readers/writers are unaffected.
ALTER TABLE ai_provider_accounts DROP CONSTRAINT ai_provider_accounts_provider_check;
ALTER TABLE ai_provider_accounts ADD CONSTRAINT ai_provider_accounts_provider_check CHECK (
  provider IN ('openai-compatible', 'gigachat', 'yandexgpt', 'kimi', 'qwen', 'deepseek')
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
