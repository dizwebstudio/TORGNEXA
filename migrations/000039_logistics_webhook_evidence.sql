BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 171: verified logistics webhook replay evidence. The callback body is
-- untrusted and is never retained; the carrier adapter must re-verify the
-- shipment through the provider API before this row is written.
CREATE UNIQUE INDEX logistics_shipments_remote_uq
  ON logistics_shipments(organization_id,workspace_id,provider_account_id,remote_id)
  WHERE remote_id <> '';

CREATE TABLE logistics_webhook_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  shipment_id text NOT NULL,
  delivery_id text NOT NULL,
  remote_id text NOT NULL,
  event_type text NOT NULL,
  remote_status text NOT NULL,
  body_digest text NOT NULL,
  verified_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT logistics_webhook_receipts_pkey PRIMARY KEY(organization_id,workspace_id,connector_account_id,delivery_id),
  CONSTRAINT logistics_webhook_receipts_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT logistics_webhook_receipts_shipment_fk FOREIGN KEY(organization_id,workspace_id,shipment_id) REFERENCES logistics_shipments(organization_id,workspace_id,shipment_id) ON DELETE RESTRICT,
  CONSTRAINT logistics_webhook_receipts_delivery_chk CHECK(delivery_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT logistics_webhook_receipts_remote_chk CHECK(remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT logistics_webhook_receipts_event_chk CHECK(event_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT logistics_webhook_receipts_status_chk CHECK(remote_status ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT logistics_webhook_receipts_digest_chk CHECK(body_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT logistics_webhook_receipts_time_chk CHECK(verified_at <= created_at + interval '5 minutes')
);
CREATE INDEX logistics_webhook_receipts_remote_idx
  ON logistics_webhook_receipts(organization_id,workspace_id,connector_account_id,remote_id);
ALTER TABLE logistics_webhook_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE logistics_webhook_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY logistics_webhook_receipts_tenant_all ON logistics_webhook_receipts FOR ALL
  USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE FUNCTION logistics_webhook_receipts_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''logistics webhook receipts are append-only''; END';
CREATE TRIGGER logistics_webhook_receipts_append_only_guard BEFORE UPDATE OR DELETE ON logistics_webhook_receipts FOR EACH ROW EXECUTE FUNCTION logistics_webhook_receipts_append_only();
REVOKE UPDATE,DELETE,TRUNCATE ON logistics_webhook_receipts FROM PUBLIC;
COMMENT ON TABLE logistics_webhook_receipts IS 'Append-only verified carrier webhook evidence; raw callback bodies are not stored.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
