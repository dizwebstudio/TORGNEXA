BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Runtime leases are deliberately separate from domain state. They let a pool
-- of workers claim cross-tenant background work without weakening FORCE RLS on
-- the domain tables themselves.
CREATE TABLE worker_runtime_jobs (
  kind text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  item_id text NOT NULL,
  available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  lease_owner text,
  lease_token text,
  lease_until timestamptz,
  attempt_count integer NOT NULL DEFAULT 0,
  last_error_code text,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT worker_runtime_jobs_pkey PRIMARY KEY (kind,organization_id,workspace_id,item_id),
  CONSTRAINT worker_runtime_jobs_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT worker_runtime_jobs_kind_chk CHECK (kind IN ('reconciliation','upload')),
  CONSTRAINT worker_runtime_jobs_item_chk CHECK (item_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT worker_runtime_jobs_lease_chk CHECK ((lease_owner IS NULL AND lease_token IS NULL AND lease_until IS NULL) OR (length(lease_owner) BETWEEN 1 AND 128 AND lease_token ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND lease_until IS NOT NULL)),
  CONSTRAINT worker_runtime_jobs_attempt_chk CHECK (attempt_count BETWEEN 0 AND 1000000),
  CONSTRAINT worker_runtime_jobs_error_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9._-]{0,63}$')
);
CREATE INDEX worker_runtime_jobs_due_idx ON worker_runtime_jobs(kind,available_at,lease_until,updated_at,item_id);
ALTER TABLE worker_runtime_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_runtime_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY worker_runtime_jobs_tenant_all ON worker_runtime_jobs FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- Discover only scopes that currently have outbox or webhook work. The caller
-- must immediately re-apply the returned tenant scope before touching rows.
CREATE FUNCTION list_worker_active_scopes(p_limit integer)
RETURNS TABLE(organization_id text, workspace_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker scope batch''; END IF;
  RETURN QUERY
    SELECT x.organization_id,x.workspace_id FROM (
      SELECT o.organization_id,o.workspace_id,MIN(o.created_at) AS due_at
      FROM public.outbox_events o
      WHERE o.published_at IS NULL AND (o.available_at IS NULL OR o.available_at<=clock_timestamp()) AND (o.lease_expires_at IS NULL OR o.lease_expires_at<=clock_timestamp())
      GROUP BY o.organization_id,o.workspace_id
      UNION ALL
      SELECT d.organization_id,d.workspace_id,MIN(d.available_at) AS due_at
      FROM public.webhook_deliveries d
      WHERE d.status IN (''pending'',''inflight'') AND d.available_at<=clock_timestamp() AND (d.lease_expires_at IS NULL OR d.lease_expires_at<=clock_timestamp())
      GROUP BY d.organization_id,d.workspace_id
    ) x
    GROUP BY x.organization_id,x.workspace_id
    ORDER BY MIN(x.due_at),x.organization_id,x.workspace_id
    LIMIT p_limit;
END';

-- Materialize eligible domain rows into a dedicated lease queue, then claim
-- them with SKIP LOCKED. This function exposes IDs and scopes only; domain data
-- remains protected by tenant RLS during execution.
CREATE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker claim'';
  END IF;

  IF p_kind=''reconciliation'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''reconciliation'',r.organization_id,r.workspace_id,r.id,clock_timestamp(),clock_timestamp()
      FROM public.reconciliation_runs r
      WHERE r.status IN (''running'',''interrupted'')
      ON CONFLICT (kind,organization_id,workspace_id,item_id) DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j
      WHERE j.kind=''reconciliation'' AND NOT EXISTS (
        SELECT 1 FROM public.reconciliation_runs r WHERE r.organization_id=j.organization_id AND r.workspace_id=j.workspace_id AND r.id=j.item_id AND r.status IN (''running'',''interrupted'')
      );
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''upload'',u.organization_id,u.workspace_id,u.id,clock_timestamp(),clock_timestamp()
      FROM public.uploads u
      WHERE u.state IN (''quarantined'',''validated'',''scanning'',''clean'')
      ON CONFLICT (kind,organization_id,workspace_id,item_id) DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j
      WHERE j.kind=''upload'' AND NOT EXISTS (
        SELECT 1 FROM public.uploads u WHERE u.organization_id=j.organization_id AND u.workspace_id=j.workspace_id AND u.id=j.item_id AND u.state IN (''quarantined'',''validated'',''scanning'',''clean'')
      );
  END IF;

  RETURN QUERY
  WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id
    FROM public.worker_runtime_jobs j
    WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp())
    ORDER BY j.available_at,j.updated_at,j.item_id
    FOR UPDATE SKIP LOCKED LIMIT p_batch
  )
  UPDATE public.worker_runtime_jobs j
  SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp()
  FROM due
  WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id
  RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release'';
  END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp()
  WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT;
  RETURN changed=1;
END';

CREATE FUNCTION complete_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  DELETE FROM public.worker_runtime_jobs
  WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT;
  RETURN changed=1;
END';

REVOKE DELETE, TRUNCATE ON worker_runtime_jobs FROM PUBLIC;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
