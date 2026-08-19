BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE warehouse_incidents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  incident_id text NOT NULL CHECK (incident_id ~ '^whinc_[a-f0-9]{32}$'),
  warehouse_id text NOT NULL,
  operational_state text NOT NULL CHECK (operational_state IN ('unavailable','lost')),
  reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','processing','completed','needs_attention','resolved')),
  cursor_offer_id text,
  routed_count integer NOT NULL DEFAULT 0 CHECK (routed_count >= 0),
  no_route_count integer NOT NULL DEFAULT 0 CHECK (no_route_count >= 0),
  opened_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,incident_id),
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK ((status IN ('completed','needs_attention','resolved')) = (completed_at IS NOT NULL))
);
CREATE UNIQUE INDEX warehouse_incidents_one_active_idx ON warehouse_incidents(organization_id,workspace_id,warehouse_id) WHERE status IN ('open','processing','needs_attention');
CREATE INDEX warehouse_incidents_status_idx ON warehouse_incidents(organization_id,workspace_id,status,updated_at,incident_id);

CREATE TABLE warehouse_incident_decisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  incident_id text NOT NULL,
  offer_id text NOT NULL,
  destination_warehouse_id text,
  result text NOT NULL CHECK (result IN ('routed','no_eligible_destination')),
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,incident_id,offer_id),
  FOREIGN KEY (organization_id,workspace_id,incident_id) REFERENCES warehouse_incidents(organization_id,workspace_id,incident_id),
  FOREIGN KEY (organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,destination_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK ((result='routed') = (destination_warehouse_id IS NOT NULL))
);

ALTER TABLE warehouse_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incidents FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incident_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incident_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY warehouse_incidents_tenant_all ON warehouse_incidents FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_incident_decisions_tenant_all ON warehouse_incident_decisions FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON warehouse_incidents,warehouse_incident_decisions FROM PUBLIC;
REVOKE UPDATE ON warehouse_incident_decisions FROM PUBLIC;

ALTER TABLE worker_runtime_jobs DROP CONSTRAINT worker_runtime_jobs_kind_chk;
ALTER TABLE worker_runtime_jobs ADD CONSTRAINT worker_runtime_jobs_kind_chk CHECK (kind IN ('reconciliation','upload','privacy','warehouse_incident'));

CREATE OR REPLACE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
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
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''warehouse_incident'',i.organization_id,i.workspace_id,i.incident_id,clock_timestamp(),clock_timestamp() FROM public.warehouse_incidents i WHERE i.status IN (''open'',''processing'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''warehouse_incident'' AND NOT EXISTS (SELECT 1 FROM public.warehouse_incidents i WHERE i.organization_id=j.organization_id AND i.workspace_id=j.workspace_id AND i.incident_id=j.item_id AND i.status IN (''open'',''processing''));
  END IF;
  RETURN QUERY WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id FROM public.worker_runtime_jobs j WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) ORDER BY j.available_at,j.updated_at,j.item_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ) UPDATE public.worker_runtime_jobs j SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp() FROM due WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE OR REPLACE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release''; END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp() WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT; RETURN changed=1;
END';

COMMENT ON TABLE warehouse_incidents IS 'Persistent automation jobs for warehouse UNAVAILABLE/LOST transitions; no row represents stock transfer.';
COMMENT ON TABLE warehouse_incident_decisions IS 'Append-only per-offer failover evidence for one warehouse incident.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
