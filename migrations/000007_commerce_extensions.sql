BEGIN;

-- TORGNEXA pre-v1 baseline component 000007: commerce_extensions.
-- Squashed, statement-order-preserving source range: legacy 000028..000040.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000028_plugin_marketplace_governance.sql
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- Public marketplace versions are immutable reviewed facts. Private packages are
-- deliberately stored separately so global catalog rows never need tenant RLS exceptions.
CREATE TABLE plugin_marketplace_versions (
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  publisher_id text NOT NULL CHECK(publisher_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  publisher_key_id text NOT NULL CHECK(publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
  publisher_key_fingerprint_sha256 text NOT NULL CHECK(publisher_key_fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  trust text NOT NULL CHECK(trust IN ('official','verified','community')),
  license_expression text NOT NULL CHECK(char_length(license_expression) BETWEEN 1 AND 256),
  security_contact text NOT NULL CHECK(char_length(security_contact) BETWEEN 3 AND 320),
  security_descriptor jsonb NOT NULL CHECK(jsonb_typeof(security_descriptor)='object'),
  review_evidence jsonb NOT NULL CHECK(jsonb_typeof(review_evidence)='object'),
  published_at timestamptz NOT NULL,
  PRIMARY KEY(plugin_id,plugin_version,artifact_sha256)
);
CREATE INDEX plugin_marketplace_versions_latest_idx ON plugin_marketplace_versions(plugin_id,published_at DESC,plugin_version DESC);
CREATE INDEX plugin_marketplace_versions_trust_idx ON plugin_marketplace_versions(trust,published_at DESC,plugin_id);

CREATE TABLE plugin_private_versions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  publisher_id text NOT NULL CHECK(publisher_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  publisher_key_id text NOT NULL CHECK(publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
  publisher_key_fingerprint_sha256 text NOT NULL CHECK(publisher_key_fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  trust text NOT NULL CHECK(trust='private'),
  license_expression text NOT NULL CHECK(char_length(license_expression) BETWEEN 1 AND 256),
  security_contact text NOT NULL CHECK(char_length(security_contact) BETWEEN 3 AND 320),
  security_descriptor jsonb NOT NULL CHECK(jsonb_typeof(security_descriptor)='object'),
  review_evidence jsonb NOT NULL CHECK(jsonb_typeof(review_evidence)='object'),
  published_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,plugin_id,plugin_version,artifact_sha256),
  CONSTRAINT plugin_private_versions_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE INDEX plugin_private_versions_latest_idx ON plugin_private_versions(organization_id,workspace_id,plugin_id,published_at DESC,plugin_version DESC);

-- Consent is an immutable exact-artifact grant. A new digest/version always needs a
-- new row, even when authority does not grow; Task 078 separately surfaces escalation.
CREATE TABLE plugin_marketplace_consents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  consent_id text NOT NULL CHECK(char_length(consent_id) BETWEEN 1 AND 160),
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  permission_grant jsonb NOT NULL CHECK(jsonb_typeof(permission_grant)='object'),
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  granted_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,consent_id),
  CONSTRAINT plugin_marketplace_consents_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE INDEX plugin_marketplace_consents_plugin_idx ON plugin_marketplace_consents(organization_id,workspace_id,plugin_id,plugin_version,artifact_sha256,granted_at DESC);

-- Global security revocations are append-only and affect every tenant immediately.
CREATE TABLE plugin_marketplace_revocations (
  revocation_id text PRIMARY KEY CHECK(char_length(revocation_id) BETWEEN 1 AND 160),
  kind text NOT NULL CHECK(kind IN ('artifact','publisher_key')),
  plugin_id text,
  artifact_sha256 text,
  publisher_id text,
  publisher_key_id text,
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 512),
  revoked_at timestamptz NOT NULL,
  CONSTRAINT plugin_marketplace_revocation_target CHECK(
    (kind='artifact' AND plugin_id ~ '^[a-z][a-z0-9-]{1,62}$' AND artifact_sha256 ~ '^[0-9a-f]{64}$' AND publisher_id IS NULL AND publisher_key_id IS NULL)
    OR
    (kind='publisher_key' AND plugin_id IS NULL AND artifact_sha256 IS NULL AND publisher_id ~ '^[a-z][a-z0-9-]{1,62}$' AND publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$')
  )
);
CREATE INDEX plugin_marketplace_revocations_artifact_idx ON plugin_marketplace_revocations(plugin_id,artifact_sha256) WHERE kind='artifact';
CREATE INDEX plugin_marketplace_revocations_key_idx ON plugin_marketplace_revocations(publisher_id,publisher_key_id) WHERE kind='publisher_key';

CREATE TABLE plugin_installation_revocations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  revocation_id text NOT NULL CHECK(char_length(revocation_id) BETWEEN 1 AND 160),
  consent_id text NOT NULL CHECK(char_length(consent_id) BETWEEN 1 AND 160),
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 512),
  revoked_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,revocation_id),
  UNIQUE(organization_id,workspace_id,consent_id),
  CONSTRAINT plugin_installation_revocations_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT plugin_installation_revocations_consent_fk FOREIGN KEY(organization_id,workspace_id,consent_id) REFERENCES plugin_marketplace_consents(organization_id,workspace_id,consent_id)
);

CREATE FUNCTION plugin_marketplace_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''plugin marketplace governance evidence is append-only''; END';
CREATE TRIGGER plugin_marketplace_versions_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_versions FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_private_versions_append_only BEFORE UPDATE OR DELETE ON plugin_private_versions FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_marketplace_consents_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_consents FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_marketplace_revocations_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_revocations FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_installation_revocations_append_only BEFORE UPDATE OR DELETE ON plugin_installation_revocations FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();

ALTER TABLE plugin_private_versions ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_private_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE plugin_marketplace_consents ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_marketplace_consents FORCE ROW LEVEL SECURITY;
ALTER TABLE plugin_installation_revocations ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_installation_revocations FORCE ROW LEVEL SECURITY;

CREATE POLICY plugin_private_versions_select ON plugin_private_versions FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_private_versions_insert ON plugin_private_versions FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_marketplace_consents_select ON plugin_marketplace_consents FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_marketplace_consents_insert ON plugin_marketplace_consents FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_installation_revocations_select ON plugin_installation_revocations FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_installation_revocations_insert ON plugin_installation_revocations FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE UPDATE, DELETE, TRUNCATE ON plugin_marketplace_versions, plugin_private_versions, plugin_marketplace_consents, plugin_marketplace_revocations, plugin_installation_revocations FROM PUBLIC;

COMMENT ON TABLE plugin_marketplace_versions IS 'Immutable public official/verified/community plugin versions with reviewed trust metadata and signed Task-025 descriptor.';
COMMENT ON TABLE plugin_private_versions IS 'Tenant-private immutable plugin versions; FORCE RLS prevents cross-tenant discovery.';
COMMENT ON TABLE plugin_marketplace_consents IS 'Explicit tenant consent bound to exact plugin version and artifact digest; never inherited by a new artifact.';
COMMENT ON TABLE plugin_marketplace_revocations IS 'Global append-only artifact/publisher-key revocations applied before runtime admission.';
COMMENT ON TABLE plugin_installation_revocations IS 'Tenant-scoped append-only consent revocation; history is retained for auditability.';

-- SOURCE 000029_promotions_pricing_guards.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE promotions (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, name text NOT NULL, kind text NOT NULL CHECK(kind IN ('discount','coupon')), starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1), CHECK(ends_at>starts_at), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE promotion_participation (organization_id text NOT NULL, workspace_id text NOT NULL, promotion_id text NOT NULL, sku text NOT NULL, proposed_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), floor_minor bigint NOT NULL, minimum_margin_bps integer NOT NULL CHECK(minimum_margin_bps BETWEEN 0 AND 10000), approval_ref text, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,promotion_id,sku), FOREIGN KEY(promotion_id) REFERENCES promotions(id));
ALTER TABLE promotions ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotions FORCE ROW LEVEL SECURITY;
CREATE POLICY promotions_tenant_policy ON promotions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE promotion_participation ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_participation FORCE ROW LEVEL SECURITY;
CREATE POLICY promotion_participation_tenant_policy ON promotion_participation FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000030_advertising_engine.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE advertising_campaigns (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, name text NOT NULL, status text NOT NULL CHECK(status IN ('draft','active','paused','ended')), daily_budget_minor bigint NOT NULL CHECK(daily_budget_minor>=0), total_budget_minor bigint NOT NULL CHECK(total_budget_minor>=daily_budget_minor), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), attribution_source text NOT NULL, attribution_medium text NOT NULL, attribution_campaign text NOT NULL, version bigint NOT NULL CHECK(version>=1), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE advertising_actions (organization_id text NOT NULL, workspace_id text NOT NULL, action_id text NOT NULL, campaign_id text NOT NULL, requested_spend_minor bigint NOT NULL CHECK(requested_spend_minor>=0), currency char(3) NOT NULL, dry_run boolean NOT NULL, approval_ref text, executed_at timestamptz, PRIMARY KEY(organization_id,workspace_id,action_id), FOREIGN KEY(campaign_id) REFERENCES advertising_campaigns(id));
ALTER TABLE advertising_campaigns ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_campaigns FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_campaigns_tenant_policy ON advertising_campaigns FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_actions_tenant_policy ON advertising_actions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000031_procurement_core.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE procurement_suppliers (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, legal_party_id text NOT NULL, name text NOT NULL, active boolean NOT NULL, version bigint NOT NULL CHECK(version>=1), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE supplier_offers (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, supplier_id text NOT NULL REFERENCES procurement_suppliers(id), sku text NOT NULL, unit_price_minor bigint NOT NULL CHECK(unit_price_minor>=0), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), min_quantity text NOT NULL, lead_time_days integer NOT NULL CHECK(lead_time_days BETWEEN 0 AND 3650), valid_until timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1));
CREATE TABLE purchase_orders (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, supplier_id text NOT NULL REFERENCES procurement_suppliers(id), status text NOT NULL CHECK(status IN ('draft','approved','sent','partially_received','received','cancelled')), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), version bigint NOT NULL CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL CHECK(updated_at>=created_at));
CREATE TABLE purchase_order_lines (organization_id text NOT NULL, workspace_id text NOT NULL, purchase_order_id text NOT NULL REFERENCES purchase_orders(id), line_id text NOT NULL, offer_id text NOT NULL, sku text NOT NULL, quantity text NOT NULL, unit_price_minor bigint NOT NULL CHECK(unit_price_minor>=0), PRIMARY KEY(organization_id,workspace_id,purchase_order_id,line_id));
ALTER TABLE procurement_suppliers ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_suppliers FORCE ROW LEVEL SECURITY;
CREATE POLICY procurement_suppliers_tenant_policy ON procurement_suppliers FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE supplier_offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_offers FORCE ROW LEVEL SECURITY;
CREATE POLICY supplier_offers_tenant_policy ON supplier_offers FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_orders_tenant_policy ON purchase_orders FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE purchase_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_order_lines_tenant_policy ON purchase_order_lines FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000032_replenishment_planning.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE replenishment_snapshots (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, algorithm_version text NOT NULL, digest char(64) NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'), captured_at timestamptz NOT NULL, inputs jsonb NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE replenishment_recommendations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, snapshot_id text NOT NULL REFERENCES replenishment_snapshots(id), algorithm_version text NOT NULL, sku text NOT NULL, supplier_offer_id text NOT NULL, recommended_units bigint NOT NULL CHECK(recommended_units>=0), explanation text NOT NULL, auto_send_po boolean NOT NULL DEFAULT false CHECK(auto_send_po=false), created_at timestamptz NOT NULL);
ALTER TABLE replenishment_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_snapshots_tenant_policy ON replenishment_snapshots FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_recommendations FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_recommendations_tenant_policy ON replenishment_recommendations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000033_wms_inventory_ledger.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE wms_locations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, warehouse_id text NOT NULL, code text NOT NULL, kind text NOT NULL CHECK(kind IN ('storage','picking','quarantine','receiving')), active boolean NOT NULL, UNIQUE(organization_id,workspace_id,warehouse_id,code), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_lots (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, sku text NOT NULL, expires_at timestamptz, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_stock_ledger (organization_id text NOT NULL, workspace_id text NOT NULL, entry_id text NOT NULL, idempotency_key text NOT NULL, sku text NOT NULL, location_id text NOT NULL REFERENCES wms_locations(id), lot_id text REFERENCES wms_lots(id), serial text NOT NULL DEFAULT '', kind text NOT NULL CHECK(kind IN ('receive','move_in','move_out','adjust','quarantine','release','reserve','unreserve','consume')), quantity bigint NOT NULL CHECK(quantity>0), reference text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,entry_id), UNIQUE(organization_id,workspace_id,idempotency_key));
CREATE FUNCTION wms_stock_ledger_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''wms stock ledger is append-only''; END';
CREATE TRIGGER wms_stock_ledger_append_only_guard BEFORE UPDATE OR DELETE ON wms_stock_ledger FOR EACH ROW EXECUTE FUNCTION wms_stock_ledger_append_only();
ALTER TABLE wms_locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_locations FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_locations_tenant_policy ON wms_locations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_lots FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_lots_tenant_policy ON wms_lots FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_stock_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_stock_ledger FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_stock_ledger_tenant_policy ON wms_stock_ledger FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000034_wms_execution.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE wms_tasks (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, task_type text NOT NULL CHECK(task_type IN ('receiving','put_away','pick','pack','cycle_count','transfer','return_receiving')), state text NOT NULL CHECK(state IN ('pending','in_progress','completed','cancelled','exception')), warehouse_id text NOT NULL, sku text NOT NULL, expected_quantity bigint NOT NULL CHECK(expected_quantity>0), processed_quantity bigint NOT NULL CHECK(processed_quantity BETWEEN 0 AND expected_quantity), version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_task_events (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, task_id text NOT NULL REFERENCES wms_tasks(id), idempotency_key text NOT NULL, kind text NOT NULL, quantity bigint NOT NULL CHECK(quantity>0), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,event_id), UNIQUE(organization_id,workspace_id,idempotency_key));
ALTER TABLE wms_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_tasks_tenant_policy ON wms_tasks FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_task_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_task_events FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_task_events_tenant_policy ON wms_task_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000035_claims_disputes.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE claims (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, context text NOT NULL CHECK(context IN ('marketplace','carrier','supplier')), state text NOT NULL CHECK(state IN ('open','submitted','waiting','won','lost','closed')), order_id text NOT NULL DEFAULT '', provider_ref text NOT NULL DEFAULT '', carrier_ref text NOT NULL DEFAULT '', supplier_id text NOT NULL DEFAULT '', deadline timestamptz, escalation_at timestamptz, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE claim_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, claim_id text NOT NULL REFERENCES claims(id), evidence_id text NOT NULL, upload_id text NOT NULL, object_ref text NOT NULL, media_type text NOT NULL, added_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,claim_id,evidence_id));
CREATE TABLE claim_compensations (organization_id text NOT NULL, workspace_id text NOT NULL, claim_id text NOT NULL REFERENCES claims(id), amount_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), settlement_entry_id text NOT NULL DEFAULT '', payment_ref text NOT NULL DEFAULT '', PRIMARY KEY(organization_id,workspace_id,claim_id));
ALTER TABLE claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE claims FORCE ROW LEVEL SECURITY;
CREATE POLICY claims_tenant_policy ON claims FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE claim_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE claim_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY claim_evidence_tenant_policy ON claim_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE claim_compensations ENABLE ROW LEVEL SECURITY;
ALTER TABLE claim_compensations FORCE ROW LEVEL SECURITY;
CREATE POLICY claim_compensations_tenant_policy ON claim_compensations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000036_customer_service_inbox.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE support_conversations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, provider text NOT NULL, account_id text NOT NULL, remote_thread_id text NOT NULL, case_id text NOT NULL DEFAULT '', assignee_id text NOT NULL DEFAULT '', sla_deadline timestamptz, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, UNIQUE(organization_id,workspace_id,provider,account_id,remote_thread_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE support_messages (organization_id text NOT NULL, workspace_id text NOT NULL, message_id text NOT NULL, conversation_id text NOT NULL REFERENCES support_conversations(id), remote_message_id text NOT NULL, direction text NOT NULL CHECK(direction IN ('in','out')), redacted_body text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,message_id), UNIQUE(organization_id,workspace_id,conversation_id,remote_message_id));
CREATE TABLE support_cases (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, conversation_id text NOT NULL REFERENCES support_conversations(id), state text NOT NULL CHECK(state IN ('open','pending','resolved')), assignee_id text NOT NULL DEFAULT '', sla_deadline timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, UNIQUE(organization_id,workspace_id,conversation_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE support_case_assignments (organization_id text NOT NULL, workspace_id text NOT NULL, assignment_id text NOT NULL, case_id text NOT NULL REFERENCES support_cases(id), assignee_id text NOT NULL, assigned_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,assignment_id));
ALTER TABLE support_conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_conversations FORCE ROW LEVEL SECURITY;
CREATE POLICY support_conversations_tenant_policy ON support_conversations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY support_messages_tenant_policy ON support_messages FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY support_cases_tenant_policy ON support_cases FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_case_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_case_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY support_case_assignments_tenant_policy ON support_case_assignments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000037_marketplace_settlement_ledger.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE settlement_entries (organization_id text NOT NULL, workspace_id text NOT NULL, entry_id text NOT NULL, provider text NOT NULL, provider_account_id text NOT NULL, provider_entry_ref text NOT NULL, order_id text NOT NULL DEFAULT '', adjusts_entry_id text NOT NULL DEFAULT '', fee_code text NOT NULL DEFAULT '', fx_rate_ref text NOT NULL DEFAULT '', kind text NOT NULL CHECK(kind IN ('sale','fee','refund','payout','adjustment')), amount_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), occurred_at timestamptz NOT NULL, imported_at timestamptz NOT NULL, disputed boolean NOT NULL DEFAULT false, PRIMARY KEY(organization_id,workspace_id,entry_id), UNIQUE(organization_id,workspace_id,provider,provider_account_id,provider_entry_ref), CHECK((kind='adjustment' AND adjusts_entry_id<>'') OR (kind<>'adjustment' AND adjusts_entry_id='')), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION settlement_entries_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''settlement entries are append-only; use adjustment entries''; END';
CREATE TRIGGER settlement_entries_append_only_guard BEFORE UPDATE OR DELETE ON settlement_entries FOR EACH ROW EXECUTE FUNCTION settlement_entries_append_only();
ALTER TABLE settlement_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_entries_tenant_policy ON settlement_entries FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000038_settlement_payment_reconciliation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE settlement_reconciliation_runs (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, generated_at timestamptz NOT NULL, timing_window_seconds bigint NOT NULL CHECK(timing_window_seconds>=0), status text NOT NULL CHECK(status IN ('running','completed','failed')), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE settlement_reconciliation_differences (organization_id text NOT NULL, workspace_id text NOT NULL, run_id text NOT NULL REFERENCES settlement_reconciliation_runs(id), difference_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('timing','known_fee','unmatched','duplicate','disputed')), reference text NOT NULL, order_id text NOT NULL DEFAULT '', detail text NOT NULL, PRIMARY KEY(organization_id,workspace_id,run_id,difference_id));
ALTER TABLE settlement_reconciliation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_reconciliation_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_reconciliation_runs_tenant_policy ON settlement_reconciliation_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settlement_reconciliation_differences ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_reconciliation_differences FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_reconciliation_differences_tenant_policy ON settlement_reconciliation_differences FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000039_retention_subject_requests_tenant_deletion.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE privacy_subject_requests (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  request_id text NOT NULL,
  request_type text NOT NULL CHECK(request_type IN ('access','export','correction','deletion','restriction')),
  subject_kind text NOT NULL,
  subject_opaque_id text NOT NULL,
  correction_artifact_ref text NOT NULL DEFAULT '',
  status text NOT NULL CHECK(status IN ('pending','running','blocked','completed')),
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,request_id),
  CHECK((request_type='correction' AND correction_artifact_ref<>'') OR (request_type<>'correction' AND correction_artifact_ref='')),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_legal_holds (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  hold_id text NOT NULL,
  selector_kind text NOT NULL CHECK(selector_kind IN ('tenant','subject','purpose_class')),
  subject_kind text NOT NULL DEFAULT '',
  subject_opaque_id text NOT NULL DEFAULT '',
  purpose_key text NOT NULL DEFAULT '',
  data_class text NOT NULL DEFAULT '',
  reason_ref text NOT NULL,
  expires_at timestamptz,
  released_at timestamptz,
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,hold_id),
  CHECK(expires_at IS NULL OR expires_at > created_at),
  CHECK(
    (selector_kind='tenant' AND subject_kind='' AND subject_opaque_id='' AND purpose_key='' AND data_class='') OR
    (selector_kind='subject' AND subject_kind<>'' AND subject_opaque_id<>'' AND purpose_key='' AND data_class='') OR
    (selector_kind='purpose_class' AND subject_kind='' AND subject_opaque_id='' AND purpose_key<>'' AND data_class IN ('public','internal','confidential','personal','sensitive_operational','secret'))
  ),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_execution_jobs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  workflow_kind text NOT NULL CHECK(workflow_kind IN ('subject_request','retention_expiry','tenant_deletion')),
  request_id text NOT NULL DEFAULT '',
  subject_kind text NOT NULL DEFAULT '',
  subject_opaque_id text NOT NULL DEFAULT '',
  purpose_key text NOT NULL DEFAULT '',
  data_class text NOT NULL DEFAULT '',
  disposition text NOT NULL DEFAULT '',
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  hold_permitted boolean NOT NULL,
  status text NOT NULL CHECK(status IN ('pending','running','blocked','completed')),
  version bigint NOT NULL CHECK(version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,job_id),
  CHECK(
    (workflow_kind='subject_request' AND request_id<>'' AND subject_kind<>'' AND subject_opaque_id<>'' AND purpose_key='' AND data_class='' AND disposition='') OR
    (workflow_kind='retention_expiry' AND request_id='' AND subject_kind='' AND subject_opaque_id='' AND purpose_key<>'' AND data_class IN ('public','internal','confidential','personal','sensitive_operational','secret') AND disposition IN ('delete','anonymize','archive_then_delete')) OR
    (workflow_kind='tenant_deletion' AND request_id='' AND subject_kind='' AND subject_opaque_id='' AND purpose_key='' AND data_class='' AND disposition='' AND action='tenant_delete')
  ),
  FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);

CREATE TABLE privacy_execution_targets (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  store_name text NOT NULL,
  store_class text NOT NULL CHECK(store_class IN ('authoritative','derived','object')),
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  cursor text NOT NULL DEFAULT '',
  status text NOT NULL CHECK(status IN ('pending','running','completed')),
  processed bigint NOT NULL CHECK(processed >= 0),
  last_digest text NOT NULL DEFAULT '',
  artifact_ref text NOT NULL DEFAULT '',
  version bigint NOT NULL CHECK(version > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,job_id,store_name),
  FOREIGN KEY(organization_id,workspace_id,job_id) REFERENCES privacy_execution_jobs(organization_id,workspace_id,job_id)
);

CREATE TABLE privacy_execution_evidence (
  evidence_id bigserial PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  store_name text NOT NULL,
  action text NOT NULL CHECK(action IN ('export','correct','delete','anonymize','restrict','archive_then_delete','tenant_delete','manual_review')),
  cursor_before text NOT NULL DEFAULT '',
  cursor_after text NOT NULL DEFAULT '',
  processed bigint NOT NULL CHECK(processed >= 0),
  digest text NOT NULL DEFAULT '',
  artifact_ref text NOT NULL DEFAULT '',
  done boolean NOT NULL,
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY(organization_id,workspace_id,job_id) REFERENCES privacy_execution_jobs(organization_id,workspace_id,job_id)
);

CREATE FUNCTION privacy_execution_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''privacy execution evidence is append-only''; END';
CREATE TRIGGER privacy_execution_evidence_append_only_guard BEFORE UPDATE OR DELETE ON privacy_execution_evidence FOR EACH ROW EXECUTE FUNCTION privacy_execution_evidence_append_only();

CREATE FUNCTION privacy_legal_hold_release_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF OLD.organization_id<>NEW.organization_id OR OLD.workspace_id<>NEW.workspace_id OR OLD.hold_id<>NEW.hold_id OR OLD.selector_kind<>NEW.selector_kind OR OLD.subject_kind<>NEW.subject_kind OR OLD.subject_opaque_id<>NEW.subject_opaque_id OR OLD.purpose_key<>NEW.purpose_key OR OLD.data_class<>NEW.data_class OR OLD.reason_ref<>NEW.reason_ref OR OLD.expires_at IS DISTINCT FROM NEW.expires_at OR OLD.created_at<>NEW.created_at OR OLD.released_at IS NOT NULL OR NEW.released_at IS NULL OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION ''privacy legal hold is immutable except release''; END IF; RETURN NEW; END';
CREATE TRIGGER privacy_legal_hold_release_only_guard BEFORE UPDATE ON privacy_legal_holds FOR EACH ROW EXECUTE FUNCTION privacy_legal_hold_release_only();

ALTER TABLE privacy_subject_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_subject_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_subject_requests_tenant_policy ON privacy_subject_requests FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_legal_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_legal_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_legal_holds_tenant_policy ON privacy_legal_holds FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_jobs_tenant_policy ON privacy_execution_jobs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_targets_tenant_policy ON privacy_execution_targets FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE privacy_execution_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_execution_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_execution_evidence_tenant_policy ON privacy_execution_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000040_fx_rate_provider_completion.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fx_rate_facts (
    id text PRIMARY KEY,
    base_currency text NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
    quote_currency text NOT NULL CHECK (quote_currency ~ '^[A-Z]{3}$' AND quote_currency <> base_currency),
    rate_coefficient bigint NOT NULL CHECK (rate_coefficient > 0),
    rate_scale smallint NOT NULL CHECK (rate_scale BETWEEN 0 AND 9),
    source_id text NOT NULL CHECK (source_id ~ '^[a-z][a-z0-9._-]{0,63}$'),
    source_reference text NOT NULL DEFAULT '',
    rate_type text NOT NULL CHECK (rate_type IN ('official','mid','bid','ask','closing','indicative')),
    observed_at timestamptz NOT NULL,
    effective_at timestamptz NOT NULL,
    schema_version smallint NOT NULL CHECK (schema_version = 1),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (length(source_reference) <= 256)
);
CREATE INDEX fx_rate_facts_lookup_idx ON fx_rate_facts(base_currency,quote_currency,rate_type,source_id,effective_at DESC,observed_at DESC);

CREATE TABLE fx_resolution_evidence (
    id text PRIMARY KEY,
    base_currency text NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
    quote_currency text NOT NULL CHECK (quote_currency ~ '^[A-Z]{3}$' AND quote_currency <> base_currency),
    rate_type text NOT NULL CHECK (rate_type IN ('official','mid','bid','ask','closing','indicative')),
    as_of timestamptz NOT NULL,
    precedence jsonb NOT NULL CHECK (jsonb_typeof(precedence)='array' AND jsonb_array_length(precedence)>0),
    candidate_fact_ids jsonb NOT NULL CHECK (jsonb_typeof(candidate_fact_ids)='array' AND jsonb_array_length(candidate_fact_ids)>0),
    selected_fact_id text NOT NULL REFERENCES fx_rate_facts(id) ON DELETE RESTRICT,
    resolved_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fx_resolution_lookup_idx ON fx_resolution_evidence(base_currency,quote_currency,rate_type,as_of DESC);

CREATE TABLE fx_conversion_records (
    id text PRIMARY KEY,
    source_currency text NOT NULL CHECK (source_currency ~ '^[A-Z]{3}$'),
    source_minor_units bigint NOT NULL,
    source_minor_unit_scale smallint NOT NULL CHECK (source_minor_unit_scale BETWEEN 0 AND 9),
    target_currency text NOT NULL CHECK (target_currency ~ '^[A-Z]{3}$' AND target_currency <> source_currency),
    target_minor_units bigint NOT NULL,
    target_minor_unit_scale smallint NOT NULL CHECK (target_minor_unit_scale BETWEEN 0 AND 9),
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
    resolution_evidence_ids jsonb NOT NULL CHECK (jsonb_typeof(resolution_evidence_ids)='array' AND jsonb_array_length(resolution_evidence_ids) BETWEEN 1 AND 2),
    digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    derived_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fx_conversion_records_time_idx ON fx_conversion_records(derived_at DESC,id);

CREATE FUNCTION fx_reference_facts_reject_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''FX reference facts and derivation evidence are append-only''; END';
CREATE TRIGGER fx_rate_facts_no_update BEFORE UPDATE OR DELETE ON fx_rate_facts FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
CREATE TRIGGER fx_resolution_evidence_no_update BEFORE UPDATE OR DELETE ON fx_resolution_evidence FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
CREATE TRIGGER fx_conversion_records_no_update BEFORE UPDATE OR DELETE ON fx_conversion_records FOR EACH ROW EXECUTE FUNCTION fx_reference_facts_reject_mutation();
REVOKE UPDATE, DELETE, TRUNCATE ON fx_rate_facts,fx_resolution_evidence,fx_conversion_records FROM PUBLIC;
-- BASELINE_SOURCE_END

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
