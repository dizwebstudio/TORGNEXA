BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 173 is additive. The original procurement tables remain the sole
-- PurchaseOrder lifecycle; these columns and evidence tables add the operator
-- workflow around it.
ALTER TABLE procurement_suppliers
  ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS payment_terms text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS default_currency char(3) NOT NULL DEFAULT 'RUB',
  ADD COLUMN IF NOT EXISTS lead_time_days integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS minimum_order_minor bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS minimum_order_currency char(3) NOT NULL DEFAULT 'RUB',
  ADD COLUMN IF NOT EXISTS contacts jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS contracts jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE procurement_suppliers
  ADD CONSTRAINT procurement_suppliers_status_chk CHECK (status IN ('active','blocked','archived')),
  ADD CONSTRAINT procurement_suppliers_currency_chk CHECK (default_currency ~ '^[A-Z]{3}$' AND minimum_order_currency ~ '^[A-Z]{3}$'),
  ADD CONSTRAINT procurement_suppliers_range_chk CHECK (lead_time_days BETWEEN 0 AND 3650 AND minimum_order_minor >= 0),
  ADD CONSTRAINT procurement_suppliers_json_chk CHECK (jsonb_typeof(contacts) = 'array' AND jsonb_typeof(contracts) = 'array');

ALTER TABLE supplier_offers
  ADD COLUMN IF NOT EXISTS canonical_offer_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS supplier_sku text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS gtin text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS unit text NOT NULL DEFAULT 'PCS',
  ADD COLUMN IF NOT EXISTS case_pack text NOT NULL DEFAULT '1',
  ADD COLUMN IF NOT EXISTS priority integer NOT NULL DEFAULT 100,
  ADD COLUMN IF NOT EXISTS minimum_order_minor bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS minimum_order_currency char(3) NOT NULL DEFAULT 'RUB',
  ADD COLUMN IF NOT EXISTS valid_from timestamptz NOT NULL DEFAULT TIMESTAMPTZ '1970-01-01 00:00:00+00',
  ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE supplier_offers
  ADD CONSTRAINT supplier_offers_ref_chk CHECK ((canonical_offer_id = '' OR canonical_offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (supplier_sku = '' OR supplier_sku ~ '^[^\r\n]{1,200}$') AND (gtin = '' OR gtin ~ '^[0-9]{8,14}$') AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  ADD CONSTRAINT supplier_offers_range_chk CHECK (case_pack <> '' AND priority BETWEEN 0 AND 100000 AND minimum_order_minor >= 0);
CREATE INDEX supplier_offers_gtin_lookup_idx ON supplier_offers (organization_id,workspace_id,supplier_id,gtin) WHERE gtin <> '';
CREATE INDEX supplier_offers_supplier_sku_lookup_idx ON supplier_offers (organization_id,workspace_id,supplier_id,supplier_sku) WHERE supplier_sku <> '';

ALTER TABLE purchase_orders
  ADD COLUMN IF NOT EXISTS warehouse_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS recommendation_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS recommendation_digest char(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS idempotency_key text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS approval_request_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS send_state text NOT NULL DEFAULT 'not_sent',
  ADD COLUMN IF NOT EXISTS error_code text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS expected_receipt_at timestamptz,
  ADD COLUMN IF NOT EXISTS creator_id text NOT NULL DEFAULT '';
ALTER TABLE purchase_orders
  ADD CONSTRAINT purchase_orders_ref_chk CHECK ((recommendation_id = '' OR recommendation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (recommendation_digest = '' OR recommendation_digest ~ '^[0-9a-f]{64}$') AND (idempotency_key = '' OR idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')),
  ADD CONSTRAINT purchase_orders_send_state_chk CHECK (send_state IN ('not_sent','queued','sent','unknown','failed'));
CREATE UNIQUE INDEX purchase_orders_idempotency_uq ON purchase_orders (organization_id,workspace_id,idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX purchase_orders_workspace_filter_idx ON purchase_orders (organization_id,workspace_id,status,supplier_id,expected_receipt_at,id);

ALTER TABLE purchase_order_lines
  ADD COLUMN IF NOT EXISTS unit text NOT NULL DEFAULT 'PCS',
  ADD COLUMN IF NOT EXISTS supplier_sku text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS received_quantity text NOT NULL DEFAULT '0';
ALTER TABLE purchase_order_lines
  ADD CONSTRAINT purchase_order_lines_ref_chk CHECK (unit ~ '^[A-Z][A-Z0-9._-]{0,15}$' AND received_quantity <> '');

-- The legacy tables use a single-column primary key. Composite foreign keys
-- below retain tenant scope, so provide the corresponding unique keys first.
CREATE UNIQUE INDEX procurement_suppliers_scope_id_uq ON procurement_suppliers (organization_id,workspace_id,id);
CREATE UNIQUE INDEX supplier_offers_scope_id_uq ON supplier_offers (organization_id,workspace_id,id);
CREATE UNIQUE INDEX purchase_orders_scope_id_uq ON purchase_orders (organization_id,workspace_id,id);

CREATE TABLE procurement_supplier_offer_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  history_id text NOT NULL,
  supplier_offer_id text NOT NULL,
  version bigint NOT NULL,
  unit_price_minor bigint NOT NULL,
  currency char(3) NOT NULL,
  minimum_quantity text NOT NULL,
  case_pack text NOT NULL,
  lead_time_days integer NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  changed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,history_id),
  FOREIGN KEY (organization_id,workspace_id,supplier_offer_id) REFERENCES supplier_offers (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT procurement_offer_history_ref_chk CHECK (history_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND currency ~ '^[A-Z]{3}$' AND unit_price_minor >= 0 AND lead_time_days BETWEEN 0 AND 3650 AND valid_until >= valid_from AND version >= 1)
);

CREATE TABLE procurement_price_list_previews (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  preview_id text NOT NULL,
  supplier_id text NOT NULL,
  upload_id text NOT NULL,
  source_sha256 char(64) NOT NULL,
  mapping_fingerprint char(64) NOT NULL,
  status text NOT NULL,
  total_rows integer NOT NULL,
  valid_rows integer NOT NULL,
  invalid_rows integer NOT NULL,
  unresolved_rows integer NOT NULL,
  errors jsonb NOT NULL DEFAULT '[]'::jsonb,
  rows jsonb NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,preview_id),
  FOREIGN KEY (organization_id,workspace_id,supplier_id) REFERENCES procurement_suppliers (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT procurement_price_list_previews_ref_chk CHECK (preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND upload_id ~ '^upl_[0-9a-f]{32}$' AND source_sha256 ~ '^[0-9a-f]{64}$' AND mapping_fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT procurement_price_list_previews_range_chk CHECK (total_rows >= 0 AND valid_rows >= 0 AND invalid_rows >= 0 AND unresolved_rows >= 0 AND valid_rows + invalid_rows <= total_rows AND version >= 1),
  CONSTRAINT procurement_price_list_previews_json_chk CHECK (jsonb_typeof(errors) = 'array' AND jsonb_typeof(rows) = 'array'),
  CONSTRAINT procurement_price_list_previews_status_chk CHECK (status IN ('preview','ready','committed','rejected'))
);
CREATE INDEX procurement_price_list_previews_supplier_idx ON procurement_price_list_previews (organization_id,workspace_id,supplier_id,created_at DESC);

CREATE TABLE procurement_purchase_order_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  purchase_order_id text NOT NULL,
  from_status text NOT NULL,
  to_status text NOT NULL,
  action text NOT NULL,
  actor_id text NOT NULL,
  idempotency_key text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,event_id),
  FOREIGN KEY (organization_id,workspace_id,purchase_order_id) REFERENCES purchase_orders (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT procurement_po_events_ref_chk CHECK (event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND action ~ '^[a-z][a-z0-9._-]{0,63}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);
CREATE UNIQUE INDEX procurement_po_events_idempotency_uq ON procurement_purchase_order_events (organization_id,workspace_id,purchase_order_id,idempotency_key) WHERE idempotency_key <> '';

CREATE TABLE procurement_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  receipt_id text NOT NULL,
  purchase_order_id text NOT NULL,
  warehouse_id text NOT NULL,
  line_id text NOT NULL,
  quantity text NOT NULL,
  unit text NOT NULL,
  status text NOT NULL,
  discrepancy_code text NOT NULL DEFAULT '',
  note text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,receipt_id),
  FOREIGN KEY (organization_id,workspace_id,purchase_order_id) REFERENCES purchase_orders (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT procurement_receipts_ref_chk CHECK (receipt_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^$|^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$' AND quantity <> '' AND status IN ('accepted','partial','discrepancy','rejected'))
);
CREATE INDEX procurement_receipts_po_idx ON procurement_receipts (organization_id,workspace_id,purchase_order_id,occurred_at,receipt_id);
CREATE UNIQUE INDEX procurement_receipts_idempotency_uq ON procurement_receipts (organization_id,workspace_id,purchase_order_id,idempotency_key) WHERE idempotency_key <> '';

CREATE TABLE procurement_reconciliation_findings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  kind text NOT NULL,
  purchase_order_id text NOT NULL DEFAULT '',
  supplier_offer_id text NOT NULL DEFAULT '',
  expected text NOT NULL,
  observed text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  detected_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,finding_id),
  CONSTRAINT procurement_reconciliation_ref_chk CHECK (finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (purchase_order_id = '' OR purchase_order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (supplier_offer_id = '' OR supplier_offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND kind IN ('stale_price','offer_changed','order_without_supplier','receipt_mismatch','duplicate_purchase_order','overdue_receipt','unknown_send_outcome') AND status IN ('open','acknowledged','resolved'))
);
CREATE INDEX procurement_reconciliation_open_idx ON procurement_reconciliation_findings (organization_id,workspace_id,status,detected_at DESC,finding_id);

CREATE FUNCTION procurement_evidence_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''procurement evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER procurement_offer_history_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON procurement_supplier_offer_history FOR EACH STATEMENT EXECUTE FUNCTION procurement_evidence_no_mutation();
CREATE TRIGGER procurement_po_events_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON procurement_purchase_order_events FOR EACH STATEMENT EXECUTE FUNCTION procurement_evidence_no_mutation();
CREATE TRIGGER procurement_receipts_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON procurement_receipts FOR EACH STATEMENT EXECUTE FUNCTION procurement_evidence_no_mutation();
CREATE TRIGGER procurement_reconciliation_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON procurement_reconciliation_findings FOR EACH STATEMENT EXECUTE FUNCTION procurement_evidence_no_mutation();
REVOKE DELETE,TRUNCATE ON procurement_supplier_offer_history,procurement_purchase_order_events,procurement_receipts,procurement_reconciliation_findings FROM PUBLIC;

ALTER TABLE procurement_supplier_offer_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_supplier_offer_history FORCE ROW LEVEL SECURITY;
ALTER TABLE procurement_price_list_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_price_list_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE procurement_purchase_order_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_purchase_order_events FORCE ROW LEVEL SECURITY;
ALTER TABLE procurement_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE procurement_reconciliation_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_reconciliation_findings FORCE ROW LEVEL SECURITY;
CREATE POLICY procurement_supplier_offer_history_tenant_all ON procurement_supplier_offer_history FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY procurement_price_list_previews_tenant_all ON procurement_price_list_previews FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY procurement_purchase_order_events_tenant_all ON procurement_purchase_order_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY procurement_receipts_tenant_all ON procurement_receipts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY procurement_reconciliation_findings_tenant_all ON procurement_reconciliation_findings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE procurement_price_list_previews IS 'Released-upload price-list previews; raw source bytes are never stored here.';
COMMENT ON TABLE procurement_receipts IS 'Append-only receiving facts consumed by WMS; this table never mutates inventory directly.';
COMMENT ON TABLE procurement_reconciliation_findings IS 'Redacted procurement drift findings for operator reconciliation.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
