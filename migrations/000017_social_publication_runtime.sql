BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Remote receipts are operational evidence, not canonical Social Core state.
-- They are append-only and let the worker finish a confirmed publication after
-- a process crash without sending the Telegram message a second time.
CREATE TABLE social_publication_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  publication_id text NOT NULL,
  connector_account_id text NOT NULL,
  remote_publication_id text NOT NULL,
  observed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT social_publication_receipts_pkey PRIMARY KEY (organization_id,workspace_id,publication_id),
  CONSTRAINT social_publication_receipts_publication_fk FOREIGN KEY (organization_id,workspace_id,publication_id)
    REFERENCES social_publications(organization_id,workspace_id,id),
  CONSTRAINT social_publication_receipts_account_fk FOREIGN KEY (organization_id,workspace_id,connector_account_id)
    REFERENCES connector_accounts(organization_id,workspace_id,id),
  CONSTRAINT social_publication_receipts_remote_chk CHECK (length(remote_publication_id) BETWEEN 1 AND 512 AND remote_publication_id !~ '[[:cntrl:]]'),
  CONSTRAINT social_publication_receipts_time_chk CHECK (observed_at <= created_at + interval '5 minutes')
);
CREATE UNIQUE INDEX social_publication_receipts_remote_uq
  ON social_publication_receipts(organization_id,workspace_id,connector_account_id,remote_publication_id);
ALTER TABLE social_publication_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE social_publication_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY social_publication_receipts_tenant_all ON social_publication_receipts FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE,DELETE,TRUNCATE ON social_publication_receipts FROM PUBLIC;

ALTER TABLE worker_runtime_jobs DROP CONSTRAINT worker_runtime_jobs_kind_chk;
ALTER TABLE worker_runtime_jobs ADD CONSTRAINT worker_runtime_jobs_kind_chk
  CHECK (kind IN ('reconciliation','upload','privacy','warehouse_incident','social_publication'));

CREATE OR REPLACE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'',''social_publication'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker claim'';
  END IF;
  IF p_kind=''reconciliation'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''reconciliation'',r.organization_id,r.workspace_id,r.id,clock_timestamp(),clock_timestamp() FROM public.reconciliation_runs r WHERE r.status IN (''running'',''interrupted'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''reconciliation'' AND NOT EXISTS (SELECT 1 FROM public.reconciliation_runs r WHERE r.organization_id=j.organization_id AND r.workspace_id=j.workspace_id AND r.id=j.item_id AND r.status IN (''running'',''interrupted''));
  ELSIF p_kind=''upload'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''upload'',u.organization_id,u.workspace_id,u.id,clock_timestamp(),clock_timestamp() FROM public.uploads u WHERE u.state IN (''quarantined'',''validated'',''scanning'',''clean'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''upload'' AND NOT EXISTS (SELECT 1 FROM public.uploads u WHERE u.organization_id=j.organization_id AND u.workspace_id=j.workspace_id AND u.id=j.item_id AND u.state IN (''quarantined'',''validated'',''scanning'',''clean''));
  ELSIF p_kind=''privacy'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''privacy'',p.organization_id,p.workspace_id,p.job_id,clock_timestamp(),clock_timestamp() FROM public.privacy_execution_jobs p WHERE p.status IN (''pending'',''running'',''blocked'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''privacy'' AND NOT EXISTS (SELECT 1 FROM public.privacy_execution_jobs p WHERE p.organization_id=j.organization_id AND p.workspace_id=j.workspace_id AND p.job_id=j.item_id AND p.status IN (''pending'',''running'',''blocked''));
  ELSIF p_kind=''warehouse_incident'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''warehouse_incident'',i.organization_id,i.workspace_id,i.incident_id,clock_timestamp(),clock_timestamp() FROM public.warehouse_incidents i WHERE i.status IN (''open'',''processing'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''warehouse_incident'' AND NOT EXISTS (SELECT 1 FROM public.warehouse_incidents i WHERE i.organization_id=j.organization_id AND i.workspace_id=j.workspace_id AND i.incident_id=j.item_id AND i.status IN (''open'',''processing''));
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''social_publication'',p.organization_id,p.workspace_id,p.id,
        CASE WHEN p.status=''scheduled'' THEN p.scheduled_at ELSE clock_timestamp() END,clock_timestamp()
      FROM public.social_publications p
      WHERE p.status IN (''ready'',''publishing'') OR (p.status=''scheduled'' AND p.scheduled_at<=clock_timestamp())
      ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''social_publication'' AND NOT EXISTS (
      SELECT 1 FROM public.social_publications p WHERE p.organization_id=j.organization_id AND p.workspace_id=j.workspace_id AND p.id=j.item_id
        AND (p.status IN (''ready'',''publishing'') OR (p.status=''scheduled'' AND p.scheduled_at<=clock_timestamp()))
    );
  END IF;
  RETURN QUERY WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id FROM public.worker_runtime_jobs j WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) ORDER BY j.available_at,j.updated_at,j.item_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ) UPDATE public.worker_runtime_jobs j SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp() FROM due WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE OR REPLACE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'',''social_publication'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release''; END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp() WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT; RETURN changed=1;
END';

COMMENT ON TABLE social_publication_receipts IS 'Append-only remote publication evidence used for crash-safe Social Core dispatch recovery.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
