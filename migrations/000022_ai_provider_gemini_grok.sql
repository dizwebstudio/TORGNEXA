BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 159 admits Google Gemini and xAI Grok to the existing tenant-scoped
-- AI provider account boundary. This is an additive expand migration; stored
-- credentials, account identity and existing providers are unchanged.
ALTER TABLE ai_provider_accounts DROP CONSTRAINT ai_provider_accounts_provider_check;
ALTER TABLE ai_provider_accounts ADD CONSTRAINT ai_provider_accounts_provider_check CHECK (
  provider IN ('openai-compatible', 'gigachat', 'yandexgpt', 'kimi', 'qwen', 'deepseek', 'claude', 'ollama', 'lm-studio', 'open-webui', 'gemini', 'grok')
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
