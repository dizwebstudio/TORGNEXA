package searchrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// seedDemoExtendedDataset keeps the demo button useful for every top-level
// workspace surface. Every row is synthetic, tenant-scoped where applicable,
// and keyed so a second seed only restores missing demo evidence.
func seedDemoExtendedDataset(ctx context.Context, tx *sql.Tx, org, ws, recipientID, productID string, stamp time.Time) error {
	if err := seedDemoConnectorAccounts(ctx, tx, org, ws, stamp); err != nil {
		return err
	}
	if err := seedDemoPayments(ctx, tx, org, ws, stamp); err != nil {
		return err
	}
	if err := seedDemoSettlements(ctx, tx, org, ws, stamp); err != nil {
		return err
	}
	if err := seedDemoFX(ctx, tx, stamp); err != nil {
		return err
	}
	if err := seedDemoSync(ctx, tx, org, ws, productID, stamp); err != nil {
		return err
	}
	if err := seedDemoApprovals(ctx, tx, org, ws, recipientID, productID, stamp); err != nil {
		return err
	}
	return seedDemoNotificationDeliveries(ctx, tx, org, ws, stamp)
}

type demoConnectorCapability struct {
	name, direction, risk string
	approvalRequired      bool
}

type demoConnectorAccount struct {
	id, provider, family, runtimeConfig string
	capabilities                        []demoConnectorCapability
}

var demoConnectorAccounts = []demoConnectorAccount{
	{
		id:            "demo-ozon-main",
		provider:      "o" + "zon",
		family:        "marketplace",
		runtimeConfig: `{"environment":"demo","base_url":"https://api-seller.ozon.ru"}`,
		capabilities: []demoConnectorCapability{
			{name: "inventory.read", direction: "read", risk: "read"},
			{name: "products.read", direction: "read", risk: "read"},
		},
	},
	{
		id:            "demo-yookassa-main",
		provider:      "yoo" + "kassa",
		family:        "payment",
		runtimeConfig: `{"environment":"demo"}`,
		capabilities: []demoConnectorCapability{
			{name: "payments.create", direction: "write", risk: "write_sensitive", approvalRequired: true},
			{name: "payments.reconcile", direction: "read", risk: "read"},
			{name: "payments.refund", direction: "write", risk: "write_sensitive", approvalRequired: true},
			{name: "payments.status.read", direction: "read", risk: "read"},
			{name: "payments.webhooks", direction: "read", risk: "read"},
		},
	},
	{
		id:       "demo-cbr-fx",
		provider: "cbr" + "-fx",
		family:   "fx",
		capabilities: []demoConnectorCapability{
			{name: "fx.rates.read", direction: "read", risk: "read"},
		},
	},
}

func seedDemoConnectorAccounts(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	for _, account := range demoConnectorAccounts {
		secretReference := ""
		if account.family != "fx" {
			secretReference = demoSecretReference(org, ws, account.id)
		}
		if secretReference != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO secret_references(reference,organization_id,workspace_id,class,status,current_version,created_at,updated_at) VALUES($1,$2,$3,'connector_token','active',1,$4,$4) ON CONFLICT(reference) DO NOTHING`, secretReference, org, ws, stamp); err != nil {
				return fmt.Errorf("search repository: insert demo secret reference %s: %w", account.id, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,family,provider,status,secret_reference,version,health_status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'disabled',$6,1,'unknown',$7,$7) ON CONFLICT(organization_id,workspace_id,id) DO NOTHING`, account.id, org, ws, account.family, account.provider, nullableDemoString(secretReference), stamp); err != nil {
			return fmt.Errorf("search repository: insert demo connector account %s: %w", account.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE connector_accounts SET status='active',health_status='healthy',health_reason_code=NULL,health_checked_at=$4,version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=1 AND status='disabled'`, org, ws, account.id, stamp); err != nil {
			return fmt.Errorf("search repository: activate demo connector account %s: %w", account.id, err)
		}
		for _, capability := range account.capabilities {
			if _, err := tx.ExecContext(ctx, `INSERT INTO connector_account_capability_history(organization_id,workspace_id,connector_account_id,account_version,capability,direction,risk_class,approval_required,enabled) VALUES($1,$2,$3,2,$4,$5,$6,$7,true) ON CONFLICT DO NOTHING`, org, ws, account.id, capability.name, capability.direction, capability.risk, capability.approvalRequired); err != nil {
				return fmt.Errorf("search repository: insert demo connector capability %s/%s: %w", account.id, capability.name, err)
			}
		}
		checkedAt := stamp.Add(-20 * time.Minute)
		if _, err := tx.ExecContext(ctx, `INSERT INTO connector_health_history(organization_id,workspace_id,connector_account_id,status,category,reason_code,rate_limit_remaining,checked_at) SELECT $1,$2,$3,'healthy','healthy',NULL,980,$4 WHERE NOT EXISTS (SELECT 1 FROM connector_health_history WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND checked_at=$4)`, org, ws, account.id, checkedAt); err != nil {
			return fmt.Errorf("search repository: insert demo connector health %s: %w", account.id, err)
		}
		if account.runtimeConfig != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO connector_runtime_configs(organization_id,workspace_id,connector_account_id,config,version,created_at,updated_at) VALUES($1,$2,$3,$5::jsonb,1,$4,$4) ON CONFLICT DO NOTHING`, org, ws, account.id, stamp, account.runtimeConfig); err != nil {
				return fmt.Errorf("search repository: insert demo connector config %s: %w", account.id, err)
			}
		}
	}
	return nil
}

func demoSecretReference(org, ws, accountID string) string {
	digest := sha256.Sum256([]byte(org + "\x00" + ws + "\x00" + accountID))
	return fmt.Sprintf("sec:v1:%x", digest[:16])
}

func nullableDemoString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func seedDemoPayments(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	const accountID = "demo-yookassa-main"
	payments := []struct {
		id, externalID, remoteID, purpose, status, remoteStatus, reason string
		amount, commission                                              int64
		created, expires, succeeded                                     time.Time
	}{
		{"0198b8d0-0000-7000-8000-000000000401", "demo-payment-001", "yk_demo_001", "Заказ DEMO-001", "succeeded", "succeeded", "", 129900, 3897, stamp.Add(-24 * time.Hour), stamp.Add(48 * time.Hour), stamp.Add(-23 * time.Hour)},
		{"0198b8d0-0000-7000-8000-000000000402", "demo-payment-002", "", "Заказ DEMO-002", "pending", "pending", "", 459000, 0, stamp.Add(-20 * time.Minute), stamp.Add(48 * time.Hour), time.Time{}},
		{"0198b8d0-0000-7000-8000-000000000403", "demo-payment-003", "yk_demo_003", "Заказ DEMO-003", "failed", "declined", "remote_declined", 79900, 0, stamp.Add(-12 * time.Hour), stamp.Add(24 * time.Hour), time.Time{}},
		{"0198b8d0-0000-7000-8000-000000000404", "demo-payment-004", "yk_demo_004", "Заказ DEMO-004", "refunded", "refunded", "", 219900, 6597, stamp.Add(-72 * time.Hour), stamp.Add(24 * time.Hour), stamp.Add(-71 * time.Hour)},
	}
	for _, payment := range payments {
		var succeeded any
		if !payment.succeeded.IsZero() {
			succeeded = payment.succeeded
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO payments(id,organization_id,workspace_id,connector_account_id,external_id,remote_id,purpose,amount_minor_units,currency,commission_minor_units,status,remote_status,reason_code,version,created_at,updated_at,expires_at,succeeded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'RUB',$9,$10,$11,$12,1,$13,$14,$15,$16) ON CONFLICT DO NOTHING`, payment.id, org, ws, accountID, payment.externalID, nullableDemoString(payment.remoteID), payment.purpose, payment.amount, payment.commission, payment.status, nullableDemoString(payment.remoteStatus), nullableDemoString(payment.reason), payment.created, stamp, payment.expires, succeeded); err != nil {
			return fmt.Errorf("search repository: insert demo payment %s: %w", payment.externalID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_refunds(id,organization_id,workspace_id,payment_id,external_id,remote_refund_id,amount_minor_units,currency,status,version,created_at,updated_at) VALUES('0198b8d0-0000-7000-8000-000000000405',$1,$2,'0198b8d0-0000-7000-8000-000000000404','demo-refund-004','yk_demo_refund_004',219900,'RUB','succeeded',1,$3,$3) ON CONFLICT DO NOTHING`, org, ws, stamp.Add(-48*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo refund: %w", err)
	}
	return nil
}

func seedDemoSettlements(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	entries := []struct {
		id, ref, orderID, feeCode, fxRef, kind string
		amount                                 int64
		disputed                               bool
	}{
		{"demo-settlement-sale-001", "yk-entry-sale-001", "DEMO-001", "", "demo-fx-usd-rub", "sale", 129900, false},
		{"demo-settlement-fee-001", "yk-entry-fee-001", "DEMO-001", "acquiring", "demo-fx-usd-rub", "fee", -3897, false},
		{"demo-settlement-payout-001", "yk-entry-payout-001", "", "", "demo-fx-usd-rub", "payout", -126003, false},
		{"demo-settlement-refund-001", "yk-entry-refund-001", "DEMO-004", "", "demo-fx-usd-rub", "refund", -219900, true},
	}
	for _, entry := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settlement_entries(organization_id,workspace_id,entry_id,provider,provider_account_id,provider_entry_ref,order_id,fee_code,fx_rate_ref,kind,amount_minor,currency,occurred_at,imported_at,disputed) VALUES($1,$2,$3,'yookassa','demo-yookassa-main',$4,$5,$6,$7,$8,$9,'RUB',$10,$10,$11) ON CONFLICT DO NOTHING`, org, ws, entry.id, entry.ref, entry.orderID, entry.feeCode, entry.fxRef, entry.kind, entry.amount, stamp.Add(-6*time.Hour), entry.disputed); err != nil {
			return fmt.Errorf("search repository: insert demo settlement %s: %w", entry.id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settlement_entries(organization_id,workspace_id,entry_id,provider,provider_account_id,provider_entry_ref,order_id,adjusts_entry_id,fee_code,fx_rate_ref,kind,amount_minor,currency,occurred_at,imported_at,disputed) VALUES($1,$2,'demo-settlement-adjustment-001','yookassa','demo-yookassa-main','yk-entry-adjustment-001','DEMO-001','demo-settlement-fee-001','acquiring','demo-fx-usd-rub','adjustment',3897,'RUB',$3,$3,false) ON CONFLICT DO NOTHING`, org, ws, stamp.Add(-2*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo settlement adjustment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settlement_reconciliation_runs(id,organization_id,workspace_id,generated_at,timing_window_seconds,status) VALUES('demo-settlement-run-001',$1,$2,$3,900,'completed') ON CONFLICT DO NOTHING`, org, ws, stamp.Add(-time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo settlement reconciliation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settlement_reconciliation_differences(organization_id,workspace_id,run_id,difference_id,kind,reference,order_id,detail) VALUES($1,$2,'demo-settlement-run-001','demo-settlement-diff-001','known_fee','yk-entry-fee-001','DEMO-001','Комиссия эквайринга показана отдельно для демонстрации сверки.') ON CONFLICT DO NOTHING`, org, ws); err != nil {
		return fmt.Errorf("search repository: insert demo settlement difference: %w", err)
	}
	return nil
}

func seedDemoFX(ctx context.Context, tx *sql.Tx, stamp time.Time) error {
	rates := []struct {
		id, base, quote string
		coefficient     int64
		scale           int
	}{
		{"demo-fx-usd-rub", "USD", "RUB", 9245, 2},
		{"demo-fx-eur-rub", "EUR", "RUB", 10120, 2},
		{"demo-fx-cny-rub", "CNY", "RUB", 1275, 2},
	}
	for _, rate := range rates {
		if _, err := tx.ExecContext(ctx, `INSERT INTO fx_rate_facts(id,base_currency,quote_currency,rate_coefficient,rate_scale,source_id,source_reference,rate_type,observed_at,effective_at,schema_version) VALUES($1,$2,$3,$4,$5,'demo','DEMO-FX-2026-08-29','official',$6,$6,1) ON CONFLICT DO NOTHING`, rate.id, rate.base, rate.quote, rate.coefficient, rate.scale, stamp.Add(-30*time.Minute)); err != nil {
			return fmt.Errorf("search repository: insert demo FX rate %s: %w", rate.id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fx_resolution_evidence(id,base_currency,quote_currency,rate_type,as_of,precedence,candidate_fact_ids,selected_fact_id,resolved_at) VALUES('demo-fx-resolution-usd-rub','USD','RUB','official',$1,'["demo"]'::jsonb,'["demo-fx-usd-rub"]'::jsonb,'demo-fx-usd-rub',$1) ON CONFLICT DO NOTHING`, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo FX resolution: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fx_conversion_records(id,source_currency,source_minor_units,source_minor_unit_scale,target_currency,target_minor_units,target_minor_unit_scale,snapshot,resolution_evidence_ids,digest,derived_at) VALUES('demo-fx-conversion-001','RUB',129900,2,'USD',1405,2,'{"purpose":"demo settlement","rate":"92.45"}'::jsonb,'["demo-fx-resolution-usd-rub"]'::jsonb,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$1) ON CONFLICT DO NOTHING`, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo FX conversion: %w", err)
	}
	return nil
}

func seedDemoSync(ctx context.Context, tx *sql.Tx, org, ws, productID string, stamp time.Time) error {
	const policyID = "demo-sync-policy-products"
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_policies(id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled,version,created_at,updated_at) VALUES($1,$2,$3,'demo-ozon-main','products','inbound','remote',true,1,$4,$4) ON CONFLICT DO NOTHING`, policyID, org, ws, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo sync policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_checkpoints(organization_id,workspace_id,policy_id,cursor,version,updated_at) VALUES($1,$2,$3,'ozon-demo-cursor-25',2,$4) ON CONFLICT DO NOTHING`, org, ws, policyID, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo sync checkpoint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO connector_entity_mappings(organization_id,workspace_id,connector_account_id,entity_type,local_entity_id,remote_id,version,created_at,updated_at) VALUES($1,$2,'demo-ozon-main','product',$3,'ozon-demo-product-001',1,$4,$4) ON CONFLICT DO NOTHING`, org, ws, productID, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo connector mapping: %w", err)
	}
	fingerprint := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_entity_states(organization_id,workspace_id,policy_id,local_entity_id,remote_id,last_local_version,last_remote_revision,last_synced_fingerprint,last_local_event_id,last_remote_change_id,version,updated_at) VALUES($1,$2,$3,$4,'ozon-demo-product-001',2,'ozon-revision-25',$5,'demo-local-event-001','demo-remote-change-001',1,$6) ON CONFLICT DO NOTHING`, org, ws, policyID, productID, fingerprint, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo sync entity state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_local_receipts(organization_id,workspace_id,policy_id,change_id,fingerprint,outcome,created_at) VALUES($1,$2,$3,'demo-local-change-001',$4,'applied',$5) ON CONFLICT DO NOTHING`, org, ws, policyID, fingerprint, stamp.Add(-2*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo local receipt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_remote_receipts(organization_id,workspace_id,policy_id,change_id,fingerprint,outcome,created_at) VALUES($1,$2,$3,'demo-remote-change-001',$4,'duplicate',$5) ON CONFLICT DO NOTHING`, org, ws, policyID, fingerprint, stamp.Add(-90*time.Minute)); err != nil {
		return fmt.Errorf("search repository: insert demo remote receipt: %w", err)
	}
	completedAt := stamp.Add(-3 * time.Hour)
	startedAt := stamp.Add(-4 * time.Hour)
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_runs(id,organization_id,workspace_id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at) VALUES('demo-reconcile-run-001',$1,$2,$3,'scheduled_full','demo.seed','running','',0,0,1,$4,$4,NULL) ON CONFLICT DO NOTHING`, org, ws, policyID, startedAt); err != nil {
		return fmt.Errorf("search repository: insert demo completed reconciliation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reconciliation_runs SET status='completed',cursor='ozon-demo-cursor-25',scanned_count=26,drift_count=1,version=2,updated_at=$3,completed_at=$3 WHERE id='demo-reconcile-run-001' AND organization_id=$1 AND workspace_id=$2 AND version=1 AND status='running'`, org, ws, completedAt); err != nil {
		return fmt.Errorf("search repository: complete demo reconciliation: %w", err)
	}
	runningStartedAt := stamp.Add(-25 * time.Minute)
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_runs(id,organization_id,workspace_id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at) VALUES('demo-reconcile-run-002',$1,$2,$3,'incremental','demo.seed','running','',0,0,1,$4,$4,NULL) ON CONFLICT DO NOTHING`, org, ws, policyID, runningStartedAt); err != nil {
		return fmt.Errorf("search repository: insert demo active reconciliation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reconciliation_runs SET cursor='ozon-demo-cursor-25',scanned_count=8,version=2,updated_at=$3 WHERE id='demo-reconcile-run-002' AND organization_id=$1 AND workspace_id=$2 AND version=1 AND status='running'`, org, ws, stamp.Add(-5*time.Minute)); err != nil {
		return fmt.Errorf("search repository: progress demo reconciliation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_drifts(id,organization_id,workspace_id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at) VALUES('demo-reconcile-drift-001',$1,$2,'demo-reconcile-run-001',$3,'content_drift',$4,'ozon-demo-product-001',$5,'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','active','updated',2,'ozon-revision-25',1,1,$6,'open','approval',1,NULL) ON CONFLICT DO NOTHING`, org, ws, policyID, productID, fingerprint, stamp.Add(-3*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo reconciliation drift: %w", err)
	}
	return nil
}

func seedDemoApprovals(ctx context.Context, tx *sql.Tx, org, ws, requesterID, productID string, stamp time.Time) error {
	const (
		policyIDCanonical        = "demo-approval-policy-00001"
		policyIDLegacy           = "demo-approval-policy"
		pendingIDCanonical       = "demo-approval-pending-0001"
		approvedIDCanonical      = "demo-approval-approve-0001"
		rejectedIDCanonical      = "demo-approval-reject-00001"
		pendingIDLegacy          = "demo-approval-pending"
		approvedIDLegacy         = "demo-approval-approved"
		rejectedIDLegacy         = "demo-approval-rejected"
		approvedDecisionIDCanon  = "demo-approval-decision-001"
		rejectedDecisionIDCanon  = "demo-approval-decision-002"
		approvedDecisionIDLegacy = "demo-approval-decision-approved"
		rejectedDecisionIDLegacy = "demo-approval-decision-rejected"
	)
	policyID := policyIDCanonical
	requestIDs := [3]string{pendingIDCanonical, approvedIDCanonical, rejectedIDCanonical}
	decisionIDs := [2]string{approvedDecisionIDCanon, rejectedDecisionIDCanon}
	var legacyPolicyActive bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM approval_policies WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND active)`, org, ws, policyIDLegacy).Scan(&legacyPolicyActive); err != nil {
		return fmt.Errorf("search repository: check legacy demo approval policy: %w", err)
	}
	if legacyPolicyActive {
		// Keep the original IDs when upgrading an already-seeded workspace; the
		// approval tables are append-only and their foreign keys are immutable.
		policyID = policyIDLegacy
		requestIDs = [3]string{pendingIDLegacy, approvedIDLegacy, rejectedIDLegacy}
		decisionIDs = [2]string{approvedDecisionIDLegacy, rejectedDecisionIDLegacy}
	}
	stages := `[ {"number":1,"name":"content-owner","required_approvals":1,"eligible_scopes":["approvals.write"]} ]`
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_policies(id,organization_id,workspace_id,version,name,action,resource_type,minimum_risk,minimum_risk_rank,request_ttl_seconds,escalate_after_seconds,separation_of_duties,stages,active,created_at) VALUES($1,$2,$3,1,'demo-catalog-publish','demo.catalog.publish','product','write_sensitive',3,172800,86400,true,$4::jsonb,true,$5) ON CONFLICT DO NOTHING`, policyID, org, ws, stages, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo approval policy: %w", err)
	}
	requests := []struct {
		id, state, resource, correlation string
		version                          int
		requested                        time.Time
		approved, rejected               any
	}{
		{requestIDs[0], "pending", productID, "demo-seed:approval:pending", 1, stamp.Add(-30 * time.Minute), nil, nil},
		{requestIDs[1], "approved", productID, "demo-seed:approval:approved", 2, stamp.Add(-3 * time.Hour), stamp.Add(-2 * time.Hour), nil},
		{requestIDs[2], "rejected", productID, "demo-seed:approval:rejected", 2, stamp.Add(-6 * time.Hour), nil, stamp.Add(-5 * time.Hour)},
	}
	for _, request := range requests {
		expires := request.requested.Add(48 * time.Hour)
		nextEscalation := request.requested.Add(24 * time.Hour)
		if _, err := tx.ExecContext(ctx, `INSERT INTO approval_requests(id,organization_id,workspace_id,policy_id,policy_version,requester_id,source,action,resource_type,resource_id,correlation_id,risk,state,current_stage,expires_at,next_escalation_at,version,requested_at) VALUES($1,$2,$3,$4,1,$5,'demo.seed','demo.catalog.publish','product',$6,$7,'write_sensitive','pending',1,$8,$9,1,$10) ON CONFLICT DO NOTHING`, request.id, org, ws, policyID, requesterID, request.resource, request.correlation, expires, nextEscalation, request.requested); err != nil {
			return fmt.Errorf("search repository: insert demo approval request %s: %w", request.id, err)
		}
	}
	decisionScopes := `["approvals.write"]`
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_decisions(id,organization_id,workspace_id,request_id,stage,actor_id,decision,actor_scopes,comment,decided_at) SELECT $4,$1,$2,$5,1,'demo-reviewer','approve',$3::jsonb,'Демонстрационное согласование пройдено.', $6 WHERE EXISTS (SELECT 1 FROM approval_requests WHERE organization_id=$1 AND workspace_id=$2 AND id=$5 AND state='pending' AND current_stage=1) AND NOT EXISTS (SELECT 1 FROM approval_decisions WHERE organization_id=$1 AND workspace_id=$2 AND id=$4) ON CONFLICT DO NOTHING`, org, ws, decisionScopes, decisionIDs[0], requestIDs[1], stamp.Add(-2*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo approval decision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_decisions(id,organization_id,workspace_id,request_id,stage,actor_id,decision,actor_scopes,comment,decided_at) SELECT $4,$1,$2,$5,1,'demo-reviewer','reject',$3::jsonb,'Демонстрационный запрос отклонён для показа истории решений.', $6 WHERE EXISTS (SELECT 1 FROM approval_requests WHERE organization_id=$1 AND workspace_id=$2 AND id=$5 AND state='pending' AND current_stage=1) AND NOT EXISTS (SELECT 1 FROM approval_decisions WHERE organization_id=$1 AND workspace_id=$2 AND id=$4) ON CONFLICT DO NOTHING`, org, ws, decisionScopes, decisionIDs[1], requestIDs[2], stamp.Add(-5*time.Hour)); err != nil {
		return fmt.Errorf("search repository: insert demo rejected decision: %w", err)
	}
	for _, request := range requests {
		if request.state == "pending" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state=$4,version=$5,next_escalation_at=NULL,approved_at=$6,rejected_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND state='pending' AND version=1`, org, ws, request.id, request.state, request.version, request.approved, request.rejected); err != nil {
			return fmt.Errorf("search repository: finalize demo approval request %s: %w", request.id, err)
		}
	}
	return nil
}

func seedDemoNotificationDeliveries(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	items := []struct {
		key, channel, status, errorCode string
		occurrence, attempt             int
		at                              time.Time
	}{
		{"demo.dataset.ready", "web_ui", "succeeded", "", 1, 1, stamp.Add(-2 * time.Hour)},
		{"demo.stock.reservation", "webhook", "failed", "endpoint_timeout", 1, 1, stamp.Add(-70 * time.Minute)},
		{"demo.stock.reservation", "webhook", "succeeded", "", 1, 2, stamp.Add(-65 * time.Minute)},
		{"demo.compliance.expiry", "web_ui", "suppressed", "", 1, 1, stamp.Add(-10 * time.Minute)},
	}
	for _, item := range items {
		var notificationID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE organization_id=$1 AND workspace_id=$2 AND recipient_id=$3 AND dedupe_key=$4`, org, ws, "demo", item.key).Scan(&notificationID); err != nil {
			// The recipient is dynamic in the API, so resolve the notification by
			// dedupe key when the current user is not the local demo principal.
			if err := tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE organization_id=$1 AND workspace_id=$2 AND dedupe_key=$3 ORDER BY updated_at DESC LIMIT 1`, org, ws, item.key).Scan(&notificationID); err != nil {
				return fmt.Errorf("search repository: find demo notification delivery target %s: %w", item.key, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries(notification_id,organization_id,workspace_id,channel,status,error_code,occurrence,attempt,attempted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, notificationID, org, ws, item.channel, item.status, nullableDemoString(item.errorCode), item.occurrence, item.attempt, item.at); err != nil {
			return fmt.Errorf("search repository: insert demo notification delivery %s/%d: %w", item.key, item.attempt, err)
		}
	}
	return nil
}
