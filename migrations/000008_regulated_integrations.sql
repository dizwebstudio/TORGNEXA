BEGIN;

-- TORGNEXA pre-v1 baseline component 000008: regulated_integrations.
-- Squashed, statement-order-preserving source range: legacy 000041..000054.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000041_chestny_znak_connector_baseline.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE marking_status_facts (organization_id text NOT NULL, workspace_id text NOT NULL, fact_id text NOT NULL, code_fingerprint text NOT NULL CHECK(length(code_fingerprint)=64), gtin text NOT NULL DEFAULT '', remote_status text NOT NULL, source_ref text NOT NULL, observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,fact_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE marking_reconciliations (organization_id text NOT NULL, workspace_id text NOT NULL, reconciliation_id text NOT NULL, code_fingerprint text NOT NULL CHECK(length(code_fingerprint)=64), expected_status text NOT NULL, remote_status text NOT NULL, outcome text NOT NULL CHECK(outcome IN ('match','drift')), observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,reconciliation_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION marking_status_facts_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''marking status facts are append-only''; END';
CREATE TRIGGER marking_status_facts_append_only_guard BEFORE UPDATE OR DELETE ON marking_status_facts FOR EACH ROW EXECUTE FUNCTION marking_status_facts_append_only();
ALTER TABLE marking_status_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE marking_status_facts FORCE ROW LEVEL SECURITY;
CREATE POLICY marking_status_facts_tenant_policy ON marking_status_facts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE marking_reconciliations ENABLE ROW LEVEL SECURITY;
ALTER TABLE marking_reconciliations FORCE ROW LEVEL SECURITY;
CREATE POLICY marking_reconciliations_tenant_policy ON marking_reconciliations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000042_signing_ukep_mchd_foundation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE signing_certificates (organization_id text NOT NULL, workspace_id text NOT NULL, certificate_id text NOT NULL, serial text NOT NULL, thumbprint text NOT NULL CHECK(length(thumbprint)=64), subject_ref text NOT NULL, issuer_ref text NOT NULL, algorithm text NOT NULL, qualified boolean NOT NULL, not_before timestamptz NOT NULL, not_after timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,certificate_id), CHECK(not_after>not_before), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_mchd_authorities (organization_id text NOT NULL, workspace_id text NOT NULL, authority_id text NOT NULL, registry_ref text NOT NULL, principal_ref text NOT NULL, representative_ref text NOT NULL, powers jsonb NOT NULL, valid_from timestamptz NOT NULL, valid_until timestamptz NOT NULL, revoked boolean NOT NULL DEFAULT false, PRIMARY KEY(organization_id,workspace_id,authority_id), CHECK(valid_until>valid_from), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_requests (organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, artifact_ref text NOT NULL, digest_hex text NOT NULL CHECK(length(digest_hex)=64), certificate_id text NOT NULL, mchd_ref text NOT NULL DEFAULT '', purpose text NOT NULL, approval_ref text NOT NULL, idempotency_key text NOT NULL, requested_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,request_id), UNIQUE(organization_id,workspace_id,idempotency_key), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE signing_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, signature_ref text NOT NULL, certificate_id text NOT NULL, mchd_ref text NOT NULL DEFAULT '', approval_ref text NOT NULL, digest_hex text NOT NULL CHECK(length(digest_hex)=64), signed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION signing_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''signing evidence is append-only''; END';
CREATE TRIGGER signing_evidence_append_only_guard BEFORE UPDATE OR DELETE ON signing_evidence FOR EACH ROW EXECUTE FUNCTION signing_evidence_append_only();
ALTER TABLE signing_certificates ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_certificates FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_certificates_tenant_policy ON signing_certificates FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_mchd_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_mchd_authorities FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_mchd_authorities_tenant_policy ON signing_mchd_authorities FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_requests_tenant_policy ON signing_requests FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE signing_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY signing_evidence_tenant_policy ON signing_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000043_edo_connector_sdk_providers.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE edo_documents (organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL, provider text NOT NULL, provider_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, kind text NOT NULL, status text NOT NULL, artifact_ref text NOT NULL, signature_ref text NOT NULL, mchd_ref text NOT NULL DEFAULT '', counterparty_ref text NOT NULL, version bigint NOT NULL CHECK(version>0), observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,document_id), UNIQUE(organization_id,workspace_id,provider,provider_account_id,remote_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE edo_status_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL, remote_status text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION edo_status_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''edo status evidence is append-only''; END';
CREATE TRIGGER edo_status_evidence_append_only_guard BEFORE UPDATE OR DELETE ON edo_status_evidence FOR EACH ROW EXECUTE FUNCTION edo_status_evidence_append_only();
ALTER TABLE edo_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE edo_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY edo_documents_tenant_policy ON edo_documents FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE edo_status_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE edo_status_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY edo_status_evidence_tenant_policy ON edo_status_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000044_kkt_ofd_fiscalization_abstraction.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fiscal_requests (organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, external_ref text NOT NULL, idempotency_key text NOT NULL, kind text NOT NULL CHECK(kind IN ('sale','refund','correction')), total_minor_units bigint NOT NULL CHECK(total_minor_units>0), currency char(3) NOT NULL, correction_of text NOT NULL DEFAULT '', remote_id text NOT NULL DEFAULT '', state text NOT NULL, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,request_id), UNIQUE(organization_id,workspace_id,idempotency_key), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE fiscal_marking_links (organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, code_fingerprint text NOT NULL, verification_status text NOT NULL, PRIMARY KEY(organization_id,workspace_id,request_id,code_fingerprint), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE fiscal_status_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, request_id text NOT NULL, remote_id text NOT NULL, state text NOT NULL, fiscal_document_ref text NOT NULL DEFAULT '', observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION fiscal_status_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''fiscal evidence is append-only''; END';
CREATE TRIGGER fiscal_status_evidence_append_only_guard BEFORE UPDATE OR DELETE ON fiscal_status_evidence FOR EACH ROW EXECUTE FUNCTION fiscal_status_evidence_append_only();
ALTER TABLE fiscal_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE fiscal_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY fiscal_requests_tenant_policy ON fiscal_requests FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE fiscal_marking_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE fiscal_marking_links FORCE ROW LEVEL SECURITY;
CREATE POLICY fiscal_marking_links_tenant_policy ON fiscal_marking_links FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE fiscal_status_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE fiscal_status_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY fiscal_status_evidence_tenant_policy ON fiscal_status_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000045_vetis_mercury_connector.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE vetis_reconciliation_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, remote_id text NOT NULL, kind text NOT NULL, remote_status text NOT NULL, stock_ref text NOT NULL DEFAULT '', source_request_ref text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION vetis_reconciliation_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''vetis evidence is append-only''; END';
CREATE TRIGGER vetis_reconciliation_evidence_append_only_guard BEFORE UPDATE OR DELETE ON vetis_reconciliation_evidence FOR EACH ROW EXECUTE FUNCTION vetis_reconciliation_evidence_append_only();
ALTER TABLE vetis_reconciliation_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE vetis_reconciliation_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY vetis_reconciliation_evidence_tenant_policy ON vetis_reconciliation_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000046_payments_sbp_provider_sdk.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE payment_records (organization_id text NOT NULL, workspace_id text NOT NULL, payment_id text NOT NULL, provider text NOT NULL, remote_id text NOT NULL, external_id text NOT NULL, status text NOT NULL, amount_minor_units bigint NOT NULL CHECK(amount_minor_units>0), currency char(3) NOT NULL, commission_minor_units bigint NOT NULL DEFAULT 0 CHECK(commission_minor_units>=0), version bigint NOT NULL CHECK(version>0), observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,payment_id), UNIQUE(organization_id,workspace_id,provider,remote_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE payment_webhook_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, delivery_id text NOT NULL, remote_id text NOT NULL, event_type text NOT NULL, body_digest text NOT NULL CHECK(length(body_digest)=64), verified_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,delivery_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION payment_webhook_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''payment webhook evidence is append-only''; END';
CREATE TRIGGER payment_webhook_evidence_append_only_guard BEFORE UPDATE OR DELETE ON payment_webhook_evidence FOR EACH ROW EXECUTE FUNCTION payment_webhook_evidence_append_only();
ALTER TABLE payment_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_records FORCE ROW LEVEL SECURITY;
CREATE POLICY payment_records_tenant_policy ON payment_records FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE payment_webhook_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_webhook_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY payment_webhook_evidence_tenant_policy ON payment_webhook_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000047_logistics_carrier_sdk.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE logistics_shipments (organization_id text NOT NULL, workspace_id text NOT NULL, shipment_id text NOT NULL, provider_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL DEFAULT '', service_code text NOT NULL, status text NOT NULL, tracking_number text NOT NULL DEFAULT '', cost_minor_units bigint NOT NULL DEFAULT 0 CHECK(cost_minor_units>=0), currency char(3) NOT NULL, min_delivery_at timestamptz, max_delivery_at timestamptz, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,shipment_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE logistics_tracking_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, shipment_id text NOT NULL, remote_status text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION logistics_tracking_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''logistics tracking evidence is append-only''; END';
CREATE TRIGGER logistics_tracking_evidence_append_only_guard BEFORE UPDATE OR DELETE ON logistics_tracking_evidence FOR EACH ROW EXECUTE FUNCTION logistics_tracking_evidence_append_only();
ALTER TABLE logistics_shipments ENABLE ROW LEVEL SECURITY;
ALTER TABLE logistics_shipments FORCE ROW LEVEL SECURITY;
CREATE POLICY logistics_shipments_tenant_policy ON logistics_shipments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE logistics_tracking_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE logistics_tracking_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY logistics_tracking_evidence_tenant_policy ON logistics_tracking_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000048_pudo_operations.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pickup_points (organization_id text NOT NULL, workspace_id text NOT NULL, point_id text NOT NULL, external_ref text NOT NULL DEFAULT '', name text NOT NULL, kind text NOT NULL CHECK(kind IN ('own','external')), capacity bigint NOT NULL CHECK(capacity>0), active boolean NOT NULL, updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,point_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE pickup_orders (organization_id text NOT NULL, workspace_id text NOT NULL, order_id text NOT NULL, point_id text NOT NULL, external_order_ref text NOT NULL, state text NOT NULL CHECK(state IN ('created','arrived','ready','issued','expired','return_pending','returned')), payment_ref text NOT NULL DEFAULT '', fiscal_ref text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,order_id), FOREIGN KEY(organization_id,workspace_id,point_id) REFERENCES pickup_points(organization_id,workspace_id,point_id));
CREATE TABLE pickup_order_events (event_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, order_id text NOT NULL, state text NOT NULL, occurred_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION pickup_order_events_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''pickup order events are append-only''; END';
CREATE TRIGGER pickup_order_events_append_only_guard BEFORE UPDATE OR DELETE ON pickup_order_events FOR EACH ROW EXECUTE FUNCTION pickup_order_events_append_only();
ALTER TABLE pickup_points ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_points FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_points_tenant_policy ON pickup_points FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE pickup_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_orders_tenant_policy ON pickup_orders FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE pickup_order_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_order_events FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_order_events_tenant_policy ON pickup_order_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000049_incident_management_runbooks.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE incidents (organization_id text NOT NULL, workspace_id text NOT NULL, incident_id text NOT NULL, dedupe_key text NOT NULL, runbook_id text NOT NULL, severity text NOT NULL CHECK(severity IN ('p1','p2','p3','p4')), state text NOT NULL CHECK(state IN ('open','acknowledged','mitigated','resolved')), occurrences bigint NOT NULL CHECK(occurrences>0), opened_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,incident_id), UNIQUE(organization_id,workspace_id,dedupe_key,incident_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE incident_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, incident_id text NOT NULL, step_id text NOT NULL, action_ref text NOT NULL DEFAULT '', validation_ref text NOT NULL DEFAULT '', rollback_ref text NOT NULL DEFAULT '', occurred_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id,incident_id) REFERENCES incidents(organization_id,workspace_id,incident_id));
CREATE FUNCTION incident_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''incident_evidence is append-only''; END';
CREATE TRIGGER incident_evidence_append_only_guard BEFORE UPDATE OR DELETE ON incident_evidence FOR EACH ROW EXECUTE FUNCTION incident_evidence_append_only();
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY incidents_tenant_policy ON incidents FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE incident_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY incident_evidence_tenant_policy ON incident_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000050_egais_government_connector.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE regulated_government_document_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, connector_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, document_kind text NOT NULL, remote_status text NOT NULL, approval_ref text NOT NULL DEFAULT '', artifact_ref text NOT NULL DEFAULT '', idempotency_key text NOT NULL DEFAULT '', observed_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,connector_account_id,external_id,remote_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION regulated_government_document_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''regulated_government_document_evidence is append-only''; END';
CREATE TRIGGER regulated_government_document_evidence_append_only_guard BEFORE UPDATE OR DELETE ON regulated_government_document_evidence FOR EACH ROW EXECUTE FUNCTION regulated_government_document_evidence_append_only();
ALTER TABLE regulated_government_document_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE regulated_government_document_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY regulated_government_document_evidence_tenant_policy ON regulated_government_document_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000051_enterprise_iam_federation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE enterprise_iam_mappings (organization_id text NOT NULL, workspace_id text NOT NULL, mapping_id text NOT NULL, protocol text NOT NULL CHECK(protocol IN ('ldap','active_directory','saml','scim','jit')), issuer text NOT NULL, external_selector text NOT NULL, role_code text NOT NULL, privileged boolean NOT NULL DEFAULT false, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,mapping_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE enterprise_identity_links (organization_id text NOT NULL, workspace_id text NOT NULL, issuer text NOT NULL, subject_fingerprint text NOT NULL, local_subject_id text NOT NULL, active boolean NOT NULL, last_reconciled_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,issuer,subject_fingerprint), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE enterprise_service_accounts (organization_id text NOT NULL, workspace_id text NOT NULL, service_account_id text NOT NULL, client_id text NOT NULL, secret_reference text NOT NULL, roles jsonb NOT NULL, disabled boolean NOT NULL, version bigint NOT NULL CHECK(version>0), rotated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,service_account_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
ALTER TABLE enterprise_iam_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_iam_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_iam_mappings_tenant_policy ON enterprise_iam_mappings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE enterprise_identity_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_identity_links FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_identity_links_tenant_policy ON enterprise_identity_links FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE enterprise_service_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE enterprise_service_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY enterprise_service_accounts_tenant_policy ON enterprise_service_accounts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000052_siem_security_event_export.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE siem_export_queue (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, event_type text NOT NULL, severity text NOT NULL, event_json jsonb NOT NULL, attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0), next_attempt_at timestamptz, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE siem_export_dlq (dlq_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, event_type text NOT NULL, event_digest text NOT NULL, attempts integer NOT NULL, failed_at timestamptz NOT NULL, reason_code text NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE siem_export_receipts (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, exported_at timestamptz NOT NULL, sink_count integer NOT NULL CHECK(sink_count>0), PRIMARY KEY(organization_id,workspace_id,event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION siem_export_dlq_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''siem_export_dlq is append-only''; END';
CREATE TRIGGER siem_export_dlq_append_only_guard BEFORE UPDATE OR DELETE ON siem_export_dlq FOR EACH ROW EXECUTE FUNCTION siem_export_dlq_append_only();
CREATE TRIGGER siem_export_receipts_append_only_guard BEFORE UPDATE OR DELETE ON siem_export_receipts FOR EACH ROW EXECUTE FUNCTION siem_export_dlq_append_only();
ALTER TABLE siem_export_queue ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_queue FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_queue_tenant_policy ON siem_export_queue FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE siem_export_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_receipts_tenant_policy ON siem_export_receipts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE siem_export_dlq ENABLE ROW LEVEL SECURITY;
ALTER TABLE siem_export_dlq FORCE ROW LEVEL SECURITY;
CREATE POLICY siem_export_dlq_tenant_policy ON siem_export_dlq FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000053_cloud_billing_lifecycle.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE cloud_plan_versions (plan_id text NOT NULL, version bigint NOT NULL CHECK(version>0), name text NOT NULL, price_minor bigint NOT NULL, currency char(3) NOT NULL, entitlements jsonb NOT NULL, effective_at timestamptz NOT NULL, PRIMARY KEY(plan_id,version));
CREATE TABLE cloud_subscriptions (organization_id text NOT NULL, workspace_id text NOT NULL, subscription_id text NOT NULL, plan_id text NOT NULL, plan_version bigint NOT NULL, state text NOT NULL CHECK(state IN ('trial','active','past_due','grace','suspended','cancelled')), period_start timestamptz NOT NULL, period_end timestamptz NOT NULL, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,subscription_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_usage_records (organization_id text NOT NULL, workspace_id text NOT NULL, usage_id text NOT NULL, meter text NOT NULL, source_event_id text NOT NULL, quantity bigint NOT NULL CHECK(quantity>0), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,usage_id), UNIQUE(organization_id,workspace_id,source_event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_invoices (organization_id text NOT NULL, workspace_id text NOT NULL, invoice_id text NOT NULL, subscription_id text NOT NULL, amount_minor bigint NOT NULL, currency char(3) NOT NULL, state text NOT NULL, provider_payment_ref text NOT NULL DEFAULT '', version bigint NOT NULL CHECK(version>0), created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,invoice_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_invoice_adjustments (organization_id text NOT NULL, workspace_id text NOT NULL, adjustment_id text NOT NULL, invoice_id text NOT NULL, reason text NOT NULL, amount_minor bigint NOT NULL, currency char(3) NOT NULL, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,adjustment_id), FOREIGN KEY(organization_id,workspace_id,invoice_id) REFERENCES cloud_invoices(organization_id,workspace_id,invoice_id));
CREATE FUNCTION cloud_usage_records_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''cloud_usage_records is append-only''; END';
CREATE TRIGGER cloud_usage_records_append_only_guard BEFORE UPDATE OR DELETE ON cloud_usage_records FOR EACH ROW EXECUTE FUNCTION cloud_usage_records_append_only();
CREATE FUNCTION cloud_invoice_adjustments_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''cloud_invoice_adjustments is append-only''; END';
CREATE TRIGGER cloud_invoice_adjustments_append_only_guard BEFORE UPDATE OR DELETE ON cloud_invoice_adjustments FOR EACH ROW EXECUTE FUNCTION cloud_invoice_adjustments_append_only();
ALTER TABLE cloud_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_subscriptions_tenant_policy ON cloud_subscriptions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_usage_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_usage_records FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_usage_records_tenant_policy ON cloud_usage_records FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_invoices FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_invoices_tenant_policy ON cloud_invoices FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_invoice_adjustments ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_invoice_adjustments FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_invoice_adjustments_tenant_policy ON cloud_invoice_adjustments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- SOURCE 000054_sms_provider_sdk.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE sms_delivery_evidence (organization_id text NOT NULL, workspace_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL, phone_fingerprint text NOT NULL CHECK(char_length(phone_fingerprint)=64), message_class text NOT NULL CHECK(message_class IN ('transactional','marketing')), status text NOT NULL, idempotency_key text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,idempotency_key), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE sms_delivery_callbacks (organization_id text NOT NULL, workspace_id text NOT NULL, delivery_id text NOT NULL, remote_id text NOT NULL, status text NOT NULL, body_digest text NOT NULL CHECK(char_length(body_digest)=64), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,delivery_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION sms_delivery_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''sms_delivery_evidence is append-only''; END';
CREATE TRIGGER sms_delivery_evidence_append_only_guard BEFORE UPDATE OR DELETE ON sms_delivery_evidence FOR EACH ROW EXECUTE FUNCTION sms_delivery_evidence_append_only();
CREATE FUNCTION sms_delivery_callbacks_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''sms_delivery_callbacks is append-only''; END';
CREATE TRIGGER sms_delivery_callbacks_append_only_guard BEFORE UPDATE OR DELETE ON sms_delivery_callbacks FOR EACH ROW EXECUTE FUNCTION sms_delivery_callbacks_append_only();
ALTER TABLE sms_delivery_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_delivery_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY sms_delivery_evidence_tenant_policy ON sms_delivery_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE sms_delivery_callbacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_delivery_callbacks FORCE ROW LEVEL SECURITY;
CREATE POLICY sms_delivery_callbacks_tenant_policy ON sms_delivery_callbacks FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
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
