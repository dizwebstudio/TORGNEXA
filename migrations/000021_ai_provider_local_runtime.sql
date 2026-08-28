BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 148 admits local AI runtimes behind the provider-neutral completion
-- boundary. The base_url constraint permits HTTP only for the explicit local
-- endpoint allowlist; external provider URLs remain HTTPS-only.
ALTER TABLE ai_provider_accounts DROP CONSTRAINT ai_provider_accounts_provider_check;
ALTER TABLE ai_provider_accounts ADD CONSTRAINT ai_provider_accounts_provider_check CHECK (
  provider IN ('openai-compatible', 'gigachat', 'yandexgpt', 'kimi', 'qwen', 'deepseek', 'claude', 'ollama', 'lm-studio', 'open-webui')
);

ALTER TABLE ai_provider_accounts DROP CONSTRAINT ai_provider_accounts_base_url_check;
ALTER TABLE ai_provider_accounts ADD CONSTRAINT ai_provider_accounts_base_url_check CHECK (
  base_url = ''
  OR (base_url ~ '^https://[^[:space:]]+$' AND char_length(base_url) <= 2039)
  OR (base_url ~ '^http://(localhost|127\.0\.0\.1|ollama|lm-studio|open-webui|host\.docker\.internal)(:[0-9]{1,5})?(/[A-Za-z0-9._~!$&()*+,;=:@%/-]*)?$' AND char_length(base_url) <= 2039)
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
