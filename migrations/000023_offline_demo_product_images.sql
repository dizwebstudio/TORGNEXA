BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Product cards may use the frontend-bundled demo assets in closed or
-- offline environments. Released upload content paths remain explicit and
-- externally hosted images remain HTTPS-only.
ALTER TABLE catalog_product_images DROP CONSTRAINT catalog_product_images_url_check;
ALTER TABLE catalog_product_images ADD CONSTRAINT catalog_product_images_url_check CHECK (
  char_length(url) <= 2039 AND (
    url ~ '^https://[^[:space:]]+$'
    OR url ~ '^/api/v1/uploads/upl_[0-9a-f]{32}/content$'
    OR url ~ '^/demo-images/demo-[0-9]+\.svg$'
  )
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
