package worker

import (
	corepayments "github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestA05PostgresReconcileUnboundIntent(t *testing.T) {
	for _, scenario := range []string{"succeeded", "canceled", "wrong_amount", "wrong_account", "missing_external_id"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			repo, _ := paymentsrepo.New(db)
			account := testPaymentReconciliationAccount()
			account.ID = auditFixtureID()
			account.OrganizationID = scope.OrganizationID().String()
			account.WorkspaceID = scope.WorkspaceID().String()
			if _, err := admin.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,provider,family,status) VALUES($1,$2,$3,$4,'payment','disabled')`, account.ID, account.OrganizationID, account.WorkspaceID, account.ConnectorID); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.ExecContext(ctx, `UPDATE connector_accounts SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, account.ID); err != nil {
				t.Fatal(err)
			}
			paymentScope, _ := corepayments.ParseScope(account.OrganizationID, account.WorkspaceID)
			id, _ := corepayments.ParsePaymentID(auditFixtureID())
			currency, _ := domain.NewCurrency("RUB")
			amount, _ := domain.NewMoney(15000, currency)
			initial, err := repo.CreatePayment(ctx, paymentScope, corepayments.CreatePayment{ID: id, ConnectorAccountID: account.ID, ExternalID: id.String(), Amount: amount, ExpiresAt: time.Now().UTC().Add(time.Hour)}, paymentReconciliationMutation(id.String(), "synthetic"))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			observation := connectors.PaymentSettlement{ExternalID: initial.ExternalID, RemoteID: "synthetic-remote", Kind: "sale", Status: "succeeded", Amount: connectors.PaymentAmount{MinorUnits: 15000, Currency: "RUB"}, OccurredAt: now}
			want := corepayments.StatusPending
			switch scenario {
			case "succeeded":
				want = corepayments.StatusSucceeded
			case "canceled":
				observation.Status = "canceled"
				want = corepayments.StatusCanceled
			case "wrong_amount":
				observation.Amount.MinorUnits++
			case "wrong_account":
				account.ID = auditFixtureID()
			case "missing_external_id":
				observation.ExternalID = ""
			}
			runner := paymentReconciliationRunner{payments: repo, secrets: paymentReconcileSecretsStub{}, refresh: paymentReconcileRefreshStub{}, registry: paymentGatewayResolverStub{gateway: paymentReconcileGatewayStub{result: connectors.PaymentReconcileResult{Items: []connectors.PaymentSettlement{observation}, ObservedAt: now}}}, now: func() time.Time { return now }}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			for range 2 {
				if err := runner.reconcileAccount(ctx, scope, paymentScope, account, now.Add(-time.Hour), now, logger); err != nil {
					t.Fatal(err)
				}
			}
			saved, err := repo.Payment(ctx, paymentScope, id)
			if err != nil || saved.Status != want {
				t.Fatalf("reconciliation state=%s want=%s err=%v", saved.Status, want, err)
			}
			expectedWrites := 1
			if want != corepayments.StatusPending {
				expectedWrites = 3
				if saved.RemoteID != observation.RemoteID || saved.Version != 3 {
					t.Fatal("remote binding or replay version invalid")
				}
			}
			var audits, events int
			if err := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM audit_records WHERE workspace_id=$1),(SELECT count(*) FROM outbox_events WHERE workspace_id=$1)`, scope.WorkspaceID().String()).Scan(&audits, &events); err != nil {
				t.Fatal(err)
			}
			if audits != expectedWrites || events != expectedWrites {
				t.Fatalf("audit=%d events=%d want=%d", audits, events, expectedWrites)
			}
		})
	}
}
