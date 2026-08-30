BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 138: discover only tenant scopes that have an active payment account.
-- The worker immediately re-enters each returned scope before reading any
-- payment or connector row. No account identifiers or credentials cross this
-- discovery boundary.
CREATE FUNCTION list_worker_payment_scopes(p_limit integer)
RETURNS TABLE(organization_id text, workspace_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_limit NOT BETWEEN 1 AND 1000 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid payment scope batch'';
  END IF;
  RETURN QUERY
    SELECT a.organization_id,a.workspace_id
    FROM public.connector_accounts a
    WHERE a.family=''payment'' AND a.status=''active''
    GROUP BY a.organization_id,a.workspace_id
    ORDER BY a.organization_id,a.workspace_id
    LIMIT p_limit;
END';

COMMENT ON FUNCTION list_worker_payment_scopes(integer) IS 'Returns tenant scopes with active payment connector accounts; worker must re-apply tenant scope before domain access.';

-- The worker resolves refunds by provider ID on every bounded sweep. Keep the
-- lookup tenant-local and avoid a full payment_refunds scan as refund history
-- grows.
CREATE INDEX payment_refunds_by_remote_worker_idx
  ON payment_refunds(organization_id,workspace_id,remote_refund_id,payment_id)
  WHERE remote_refund_id IS NOT NULL;

INSERT INTO migration_history (
  version, name, file_name, phase, risk, checksum_sha256,
  application_version, execution_id, duration_ms
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
