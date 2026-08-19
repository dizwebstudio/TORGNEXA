BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE connector_bootstrap_previews (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL,
  policy_count integer NOT NULL,
  read_count integer NOT NULL,
  write_count integer NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CONSTRAINT connector_bootstrap_previews_pkey PRIMARY KEY (id),
  CONSTRAINT connector_bootstrap_previews_tenant_identity UNIQUE (id,organization_id,workspace_id),
  CONSTRAINT connector_bootstrap_previews_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_bootstrap_previews_id_chk CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT connector_bootstrap_previews_counts_chk CHECK (account_version >= 1 AND policy_count BETWEEN 1 AND 200 AND read_count BETWEEN 0 AND policy_count AND write_count BETWEEN 0 AND policy_count),
  CONSTRAINT connector_bootstrap_previews_time_chk CHECK (expires_at > created_at AND expires_at <= created_at + interval '1 hour' AND (consumed_at IS NULL OR consumed_at >= created_at))
);

CREATE TABLE connector_sync_schedules (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  mode text NOT NULL,
  interval_minutes integer NOT NULL,
  enabled boolean NOT NULL,
  next_run_at timestamptz,
  last_enqueued_at timestamptz,
  last_job_id text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT connector_sync_schedules_pkey PRIMARY KEY (organization_id,workspace_id,connector_account_id),
  CONSTRAINT connector_sync_schedules_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_schedules_mode_chk CHECK (mode IN ('incremental','scheduled_full')),
  CONSTRAINT connector_sync_schedules_interval_chk CHECK (interval_minutes BETWEEN 15 AND 10080),
  CONSTRAINT connector_sync_schedules_enabled_chk CHECK (enabled = (next_run_at IS NOT NULL)),
  CONSTRAINT connector_sync_schedules_version_chk CHECK (version >= 1 AND updated_at >= created_at),
  CONSTRAINT connector_sync_schedules_job_chk CHECK (last_job_id IS NULL OR last_job_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);

CREATE TABLE connector_sync_jobs (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  kind text NOT NULL,
  mode text NOT NULL,
  status text NOT NULL,
  preview_id text,
  checkpoint_policy_id text,
  started_runs integer NOT NULL DEFAULT 0,
  attempt_count integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5,
  available_at timestamptz NOT NULL,
  lease_owner text,
  lease_token text,
  lease_until timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT connector_sync_jobs_pkey PRIMARY KEY (id),
  CONSTRAINT connector_sync_jobs_tenant_identity UNIQUE (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_preview_fk FOREIGN KEY (preview_id,organization_id,workspace_id)
    REFERENCES connector_bootstrap_previews (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_checkpoint_fk FOREIGN KEY (checkpoint_policy_id,organization_id,workspace_id)
    REFERENCES sync_policies (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_kind_chk CHECK (kind IN ('initial_import','scheduled_sync')),
  CONSTRAINT connector_sync_jobs_mode_chk CHECK (mode IN ('incremental','scheduled_full')),
  CONSTRAINT connector_sync_jobs_status_chk CHECK (status IN ('pending','running','retry_wait','completed','failed')),
  CONSTRAINT connector_sync_jobs_preview_kind_chk CHECK ((kind='initial_import') = (preview_id IS NOT NULL)),
  CONSTRAINT connector_sync_jobs_counts_chk CHECK (started_runs BETWEEN 0 AND 200 AND attempt_count BETWEEN 0 AND max_attempts AND max_attempts BETWEEN 1 AND 5),
  CONSTRAINT connector_sync_jobs_lease_chk CHECK ((lease_token IS NULL AND lease_owner IS NULL AND lease_until IS NULL) OR (lease_token ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND length(lease_owner) BETWEEN 1 AND 128 AND lease_until IS NOT NULL)),
  CONSTRAINT connector_sync_jobs_error_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT connector_sync_jobs_time_chk CHECK (updated_at >= created_at AND (started_at IS NULL OR started_at >= created_at) AND (completed_at IS NULL OR (started_at IS NOT NULL AND completed_at >= started_at))),
  CONSTRAINT connector_sync_jobs_state_chk CHECK (((status IN ('completed','failed')) = (completed_at IS NOT NULL)) AND (status='pending' OR started_at IS NOT NULL))
);

CREATE INDEX connector_bootstrap_previews_account_idx ON connector_bootstrap_previews (organization_id,workspace_id,connector_account_id,created_at DESC,id);
CREATE INDEX connector_sync_schedules_due_idx ON connector_sync_schedules (next_run_at,organization_id,workspace_id,connector_account_id) WHERE enabled;
CREATE INDEX connector_sync_jobs_dispatch_idx ON connector_sync_jobs (available_at,created_at,id) WHERE status IN ('pending','retry_wait','running');
CREATE INDEX connector_sync_jobs_account_idx ON connector_sync_jobs (organization_id,workspace_id,connector_account_id,created_at DESC,id);

ALTER TABLE connector_bootstrap_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_bootstrap_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_schedules FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY connector_bootstrap_previews_tenant_all ON connector_bootstrap_previews
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY connector_sync_schedules_tenant_all ON connector_sync_schedules
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY connector_sync_jobs_tenant_all ON connector_sync_jobs
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION connector_bootstrap_preview_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN RETURN NEW; END IF;
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.account_version<>OLD.account_version OR NEW.policy_count<>OLD.policy_count OR NEW.read_count<>OLD.read_count OR NEW.write_count<>OLD.write_count OR NEW.created_at<>OLD.created_at OR NEW.expires_at<>OLD.expires_at OR OLD.consumed_at IS NOT NULL OR NEW.consumed_at IS NULL THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''bootstrap preview evidence is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_bootstrap_preview_guard_update BEFORE UPDATE ON connector_bootstrap_previews FOR EACH ROW EXECUTE FUNCTION connector_bootstrap_preview_guard();

CREATE FUNCTION connector_sync_schedule_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''schedule must start at version 1''; END IF; RETURN NEW; END IF;
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid schedule transition'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_sync_schedule_guard_insert BEFORE INSERT ON connector_sync_schedules FOR EACH ROW EXECUTE FUNCTION connector_sync_schedule_guard();
CREATE TRIGGER connector_sync_schedule_guard_update BEFORE UPDATE ON connector_sync_schedules FOR EACH ROW EXECUTE FUNCTION connector_sync_schedule_guard();

-- A scheduler must discover work across tenants while ordinary queries remain
-- FORCE-RLS tenant scoped. This narrow definer function only enqueues due rows
-- and leases bounded metadata; the worker reapplies the returned tenant scope.
CREATE FUNCTION claim_connector_sync_jobs(p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(id text,organization_id text,workspace_id text,connector_account_id text,kind text,mode text,status text,preview_id text,checkpoint_policy_id text,started_runs integer,attempt_count integer,max_attempts integer,available_at timestamptz,created_at timestamptz,updated_at timestamptz,started_at timestamptz,completed_at timestamptz,last_error_code text,lease_token text,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 100 OR p_lease_seconds NOT BETWEEN 5 AND 300 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid scheduler claim'';
  END IF;
  WITH due AS (
    SELECT s.organization_id,s.workspace_id,s.connector_account_id,s.mode,s.next_run_at
    FROM connector_sync_schedules s WHERE s.enabled AND s.next_run_at<=clock_timestamp()
    ORDER BY s.next_run_at,s.organization_id,s.workspace_id,s.connector_account_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ), inserted AS (
    INSERT INTO connector_sync_jobs(id,organization_id,workspace_id,connector_account_id,kind,mode,status,available_at,created_at,updated_at)
    SELECT ''schedule-''||md5(d.organization_id||'':''||d.workspace_id||'':''||d.connector_account_id||'':''||d.next_run_at::text),d.organization_id,d.workspace_id,d.connector_account_id,''scheduled_sync'',d.mode,''pending'',d.next_run_at,clock_timestamp(),clock_timestamp() FROM due d
    ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,connector_account_id
  )
  UPDATE connector_sync_schedules s SET last_enqueued_at=s.next_run_at,last_job_id=''schedule-''||md5(s.organization_id||'':''||s.workspace_id||'':''||s.connector_account_id||'':''||s.next_run_at::text),next_run_at=s.next_run_at+make_interval(mins=>s.interval_minutes),version=s.version+1,updated_at=clock_timestamp()
  FROM due d WHERE s.organization_id=d.organization_id AND s.workspace_id=d.workspace_id AND s.connector_account_id=d.connector_account_id;

  UPDATE connector_sync_jobs j SET status=''failed'',completed_at=clock_timestamp(),updated_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error_code=''attempts_exhausted''
  WHERE j.status=''running'' AND j.lease_until<clock_timestamp() AND j.attempt_count>=j.max_attempts;

  RETURN QUERY WITH candidates AS (
    SELECT j.id FROM connector_sync_jobs j
    WHERE ((j.status IN (''pending'',''retry_wait'') AND j.available_at<=clock_timestamp()) OR (j.status=''running'' AND j.lease_until<clock_timestamp())) AND j.attempt_count<j.max_attempts
    ORDER BY j.available_at,j.created_at,j.id FOR UPDATE SKIP LOCKED LIMIT p_batch
  )
  UPDATE connector_sync_jobs j SET status=''running'',attempt_count=j.attempt_count+1,lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),started_at=coalesce(j.started_at,clock_timestamp()),completed_at=NULL,updated_at=clock_timestamp(),last_error_code=NULL
  FROM candidates c WHERE j.id=c.id
  RETURNING j.id,j.organization_id,j.workspace_id,j.connector_account_id,j.kind,j.mode,j.status,j.preview_id,j.checkpoint_policy_id,j.started_runs,j.attempt_count,j.max_attempts,j.available_at,j.created_at,j.updated_at,j.started_at,j.completed_at,j.last_error_code,j.lease_token,j.lease_until;
END';

REVOKE ALL ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) FROM PUBLIC;
REVOKE DELETE,TRUNCATE ON connector_bootstrap_previews,connector_sync_schedules,connector_sync_jobs FROM PUBLIC;

COMMENT ON TABLE connector_bootstrap_previews IS 'Task-108 immutable dry-run summaries; metadata only, no remote payloads or credentials; 30 minute authorization evidence.';
COMMENT ON TABLE connector_sync_schedules IS 'Task-108 durable per-account interval schedules with optimistic versions; never browser-local state.';
COMMENT ON TABLE connector_sync_jobs IS 'Task-108 tenant-scoped resumable initial-import and scheduled fan-out jobs. Entity/page cursors remain in reconciliation_runs.';
COMMENT ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) IS 'Bounded cross-tenant scheduler lease boundary; callers must reapply returned tenant scope before processing.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
