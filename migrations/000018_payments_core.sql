BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Payments Core: exact minor-unit amounts, remote-authoritative status,
-- idempotent create/refund, and verified webhook replay evidence (ADR-0071).
-- No raw cardholder data is modeled anywhere in this schema.

CREATE TABLE payments (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  external_id text NOT NULL,
  remote_id text,
  purpose text,
  amount_minor_units bigint NOT NULL,
  currency char(3) NOT NULL,
  commission_minor_units bigint NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'pending',
  remote_status text,
  reason_code text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at timestamptz NOT NULL,
  succeeded_at timestamptz,
  PRIMARY KEY(id),
  CONSTRAINT payments_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT payments_external_key UNIQUE(organization_id,workspace_id,external_id),
  CONSTRAINT payments_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT payments_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT payments_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT payments_external_chk CHECK(external_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payments_remote_chk CHECK(remote_id IS NULL OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payments_purpose_chk CHECK(purpose IS NULL OR char_length(purpose) BETWEEN 1 AND 210),
  CONSTRAINT payments_amount_chk CHECK(amount_minor_units>0),
  CONSTRAINT payments_currency_chk CHECK(currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payments_commission_chk CHECK(commission_minor_units>=0),
  CONSTRAINT payments_status_chk CHECK(status IN ('pending','created','succeeded','failed','canceled','refunded','partially_refunded')),
  CONSTRAINT payments_remote_status_chk CHECK(remote_status IS NULL OR remote_status ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT payments_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT payments_failure_shape_chk CHECK((status='failed' AND reason_code IS NOT NULL) OR (status<>'failed' AND reason_code IS NULL)),
  CONSTRAINT payments_remote_shape_chk CHECK((status='pending' AND remote_id IS NULL) OR (status<>'pending' AND remote_id IS NOT NULL)),
  CONSTRAINT payments_succeeded_shape_chk CHECK((status IN ('succeeded','refunded','partially_refunded') AND succeeded_at IS NOT NULL) OR (status NOT IN ('succeeded','refunded','partially_refunded') AND succeeded_at IS NULL)),
  CONSTRAINT payments_version_chk CHECK(version>=1),
  CONSTRAINT payments_time_chk CHECK(updated_at>=created_at AND (succeeded_at IS NULL OR succeeded_at>=created_at))
);
CREATE INDEX payments_list_idx ON payments(organization_id,workspace_id,created_at DESC,id DESC);
CREATE INDEX payments_status_idx ON payments(organization_id,workspace_id,status,updated_at);

CREATE TABLE payment_refunds (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  payment_id text NOT NULL,
  external_id text NOT NULL,
  remote_refund_id text,
  amount_minor_units bigint NOT NULL,
  currency char(3) NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT payment_refunds_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT payment_refunds_external_key UNIQUE(organization_id,workspace_id,external_id),
  CONSTRAINT payment_refunds_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT payment_refunds_payment_fk FOREIGN KEY(organization_id,workspace_id,payment_id) REFERENCES payments(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT payment_refunds_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT payment_refunds_external_chk CHECK(external_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payment_refunds_remote_chk CHECK(remote_refund_id IS NULL OR remote_refund_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payment_refunds_amount_chk CHECK(amount_minor_units>0),
  CONSTRAINT payment_refunds_currency_chk CHECK(currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payment_refunds_status_chk CHECK(status IN ('pending','accepted','succeeded','failed')),
  CONSTRAINT payment_refunds_remote_shape_chk CHECK((status='pending' AND remote_refund_id IS NULL) OR (status<>'pending' AND remote_refund_id IS NOT NULL)),
  CONSTRAINT payment_refunds_version_chk CHECK(version>=1),
  CONSTRAINT payment_refunds_time_chk CHECK(updated_at>=created_at)
);
CREATE INDEX payment_refunds_by_payment_idx ON payment_refunds(organization_id,workspace_id,payment_id,created_at);

-- Verified webhook delivery evidence: append-only replay-dedup proof. The
-- transport must verify authenticity before this row exists (ADR-0071); the
-- webhook body itself is never stored, only its digest.
CREATE TABLE payment_webhook_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  delivery_id text NOT NULL,
  remote_payment_id text NOT NULL,
  event_type text NOT NULL,
  body_digest text NOT NULL,
  verified_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT payment_webhook_receipts_pkey PRIMARY KEY(organization_id,workspace_id,connector_account_id,delivery_id),
  CONSTRAINT payment_webhook_receipts_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT payment_webhook_receipts_delivery_chk CHECK(delivery_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payment_webhook_receipts_remote_chk CHECK(remote_payment_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT payment_webhook_receipts_event_chk CHECK(event_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT payment_webhook_receipts_digest_chk CHECK(body_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT payment_webhook_receipts_time_chk CHECK(verified_at<=created_at + interval '5 minutes')
);
CREATE INDEX payment_webhook_receipts_remote_idx ON payment_webhook_receipts(organization_id,workspace_id,connector_account_id,remote_payment_id);

ALTER TABLE payments ENABLE ROW LEVEL SECURITY; ALTER TABLE payments FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_refunds ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_refunds FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_webhook_receipts ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_webhook_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY payments_tenant_all ON payments FOR ALL
  USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY payment_refunds_tenant_all ON payment_refunds FOR ALL
  USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY payment_webhook_receipts_tenant_all ON payment_webhook_receipts FOR ALL
  USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE UPDATE,DELETE,TRUNCATE ON payment_webhook_receipts FROM PUBLIC;

COMMENT ON TABLE payments IS 'Tenant-scoped payment rail state. Remote status is authoritative; TORGNEXA never guesses an ambiguous outcome.';
COMMENT ON TABLE payment_refunds IS 'Tenant-scoped refund state, one row per idempotent refund request.';
COMMENT ON TABLE payment_webhook_receipts IS 'Append-only verified webhook delivery evidence used only for replay dedup and audit.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
