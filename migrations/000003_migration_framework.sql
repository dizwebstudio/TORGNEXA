BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE migration_history (
  version integer PRIMARY KEY,
  name text NOT NULL,
  file_name text NOT NULL,
  phase text NOT NULL,
  risk text NOT NULL,
  checksum_sha256 text NOT NULL,
  application_version text NOT NULL,
  execution_id text NOT NULL,
  duration_ms bigint NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT migration_history_version_chk CHECK (version BETWEEN 1 AND 999999),
  CONSTRAINT migration_history_name_chk CHECK (name ~ '^[a-z][a-z0-9_]{1,62}$'),
  CONSTRAINT migration_history_file_chk CHECK (file_name ~ '^[0-9]{6}_[a-z][a-z0-9_]{1,62}[.]sql$'),
  CONSTRAINT migration_history_phase_chk CHECK (phase IN ('expand', 'migrate', 'contract')),
  CONSTRAINT migration_history_risk_chk CHECK (risk IN ('low', 'medium', 'high', 'critical')),
  CONSTRAINT migration_history_checksum_chk CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT migration_history_application_version_chk CHECK (
    application_version ~ '^[0-9]+[.][0-9]+[.][0-9]+(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$'
  ),
  CONSTRAINT migration_history_execution_id_chk CHECK (
    execution_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR execution_id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT migration_history_duration_chk CHECK (duration_ms >= 0)
);

CREATE TABLE backfill_jobs (
  id text PRIMARY KEY,
  job_key text NOT NULL,
  organization_id text,
  workspace_id text,
  state text NOT NULL DEFAULT 'pending',
  checkpoint text NOT NULL DEFAULT '',
  batch_size integer NOT NULL,
  lease_owner text,
  lease_until timestamptz,
  lease_generation bigint NOT NULL DEFAULT 0,
  attempts bigint NOT NULL DEFAULT 0,
  processed_count bigint NOT NULL DEFAULT 0,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  CONSTRAINT backfill_jobs_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT backfill_jobs_key_chk CHECK (job_key ~ '^[a-z][a-z0-9_.-]{2,127}$'),
  CONSTRAINT backfill_jobs_scope_chk CHECK (
    (organization_id IS NULL AND workspace_id IS NULL)
    OR (organization_id IS NOT NULL AND workspace_id IS NOT NULL)
  ),
  CONSTRAINT backfill_jobs_state_chk CHECK (state IN ('pending', 'running', 'failed', 'paused', 'completed')),
  CONSTRAINT backfill_jobs_checkpoint_chk CHECK (
    octet_length(checkpoint) <= 512 AND checkpoint !~ '[[:cntrl:]]'
  ),
  CONSTRAINT backfill_jobs_batch_size_chk CHECK (batch_size BETWEEN 1 AND 10000),
  CONSTRAINT backfill_jobs_lease_owner_chk CHECK (
    lease_owner IS NULL OR lease_owner ~ '^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$'
  ),
  CONSTRAINT backfill_jobs_lease_state_chk CHECK (
    (state = 'running' AND lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    OR (state <> 'running' AND lease_owner IS NULL AND lease_until IS NULL)
  ),
  CONSTRAINT backfill_jobs_lease_generation_chk CHECK (lease_generation >= 0),
  CONSTRAINT backfill_jobs_attempts_chk CHECK (attempts >= 0),
  CONSTRAINT backfill_jobs_processed_count_chk CHECK (processed_count >= 0),
  CONSTRAINT backfill_jobs_error_code_chk CHECK (
    last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{2,63}$'
  ),
  CONSTRAINT backfill_jobs_version_chk CHECK (version >= 1),
  CONSTRAINT backfill_jobs_timestamps_chk CHECK (
    updated_at >= created_at AND (completed_at IS NULL OR completed_at >= created_at)
  ),
  CONSTRAINT backfill_jobs_completion_chk CHECK (
    (state = 'completed' AND completed_at IS NOT NULL)
    OR (state <> 'completed' AND completed_at IS NULL)
  ),
  CONSTRAINT backfill_jobs_workspace_scope_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT backfill_jobs_scope_key UNIQUE NULLS NOT DISTINCT (job_key, organization_id, workspace_id)
);

CREATE INDEX backfill_jobs_claim_idx
  ON backfill_jobs (state, lease_until, job_key, id)
  WHERE state IN ('pending', 'running', 'failed');

REVOKE ALL ON migration_history FROM PUBLIC;
REVOKE ALL ON backfill_jobs FROM PUBLIC;

COMMENT ON TABLE migration_history IS 'Append-only applied migration metadata; checksum drift or unknown applied versions block startup/upgrade.';
COMMENT ON TABLE backfill_jobs IS 'Resumable at-least-once backfill checkpoints with expiring fenced leases; checkpoint values contain stable cursors only, never payloads or secrets.';
COMMENT ON COLUMN backfill_jobs.lease_generation IS 'Monotonic fencing token; stale workers cannot commit after another worker reclaims an expired lease.';

COMMIT;
