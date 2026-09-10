package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
)

type paymentWebhookFixture struct {
	ctx          context.Context
	db, admin    *sql.DB
	scope        tenancy.Scope
	paymentScope payments.Scope
	repo         *paymentsrepo.Repository
	api          paymentWebhookAPI
	id           payments.PaymentID
	path         string
}

func newPaymentWebhookFixture(t *testing.T) paymentWebhookFixture {
	t.Helper()
	ctx, db, admin, scope := auditPostgres(t)
	accountID := auditFixtureID()
	if _, err := admin.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,provider,family,status) VALUES($1,$2,$3,'synthetic','payment','disabled')`, accountID, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE connector_accounts SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	repo, _ := paymentsrepo.New(db)
	accounts, _ := connectorrepo.New(db)
	ps, _ := payments.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	id, _ := payments.ParsePaymentID(auditFixtureID())
	amount, _ := domain.NewMoney(15000, "RUB")
	payment, err := repo.CreatePayment(ctx, ps, payments.CreatePayment{ID: id, ConnectorAccountID: accountID, ExternalID: id.String(), Amount: amount, ExpiresAt: time.Now().UTC().Add(time.Hour)}, paymentsMutation("system:fixture", "fixture-create"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ChangePaymentStatus(ctx, ps, payments.ChangePaymentStatus{ID: id, ExpectedVersion: payment.Version, Status: payments.StatusCreated, RemoteID: "synthetic-remote"}, paymentsMutation("system:fixture", "fixture-bind")); err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	gateway := fakeWebhookGateway{verify: func(_ context.Context, body, _ []byte) (sdk.PaymentWebhook, error) {
		digest := sha256.Sum256(body)
		return sdk.PaymentWebhook{DeliveryID: string(body), RemotePaymentID: "synthetic-remote", EventType: "payment_succeeded", BodyDigest: hex.EncodeToString(digest[:]), OccurredAt: observedAt, Ack: "provider-ack"}, nil
	}}
	api := paymentWebhookAPI{repository: repo, accounts: accounts, secrets: fakeWebhookSecrets{}, registry: createGatewayResolver{gateway}}
	path := paymentWebhooksPathPrefix + "synthetic/" + scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + accountID
	return paymentWebhookFixture{ctx, db, admin, scope, ps, repo, api, id, path}
}

func (f paymentWebhookFixture) send(delivery string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.api.receive(w, httptest.NewRequest(http.MethodPost, f.path, strings.NewReader(delivery)).WithContext(f.ctx))
	return w
}

func webhookRowCount(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, table string) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Inject failures in actual PostgreSQL statements, including a deferred COMMIT
// failure after every write has succeeded. Table and timing are test constants.
func webhookFailureSwitch(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, table, timing string) func() {
	t.Helper()
	name := "webhook_fail_" + strings.ReplaceAll(scope.WorkspaceID().String(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE OR REPLACE FUNCTION webhook_fixture_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic persistence failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`CREATE %s %s %s ON %s FOR EACH ROW WHEN (NEW.workspace_id='%s') EXECUTE FUNCTION webhook_fixture_failure()`, "TRIGGER", name, timing, table, scope.WorkspaceID().String())
	if timing == "deferred" {
		query = fmt.Sprintf(`CREATE CONSTRAINT TRIGGER %s AFTER INSERT ON %s DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.workspace_id='%s') EXECUTE FUNCTION webhook_fixture_failure()`, name, table, scope.WorkspaceID().String())
	}
	if _, err := admin.ExecContext(ctx, query); err != nil {
		t.Fatal(err)
	}
	drop := func() {
		if _, err := admin.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON `+table); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(drop)
	return drop
}

func (f paymentWebhookFixture) assertState(t *testing.T, status payments.Status, receipts, effects int) {
	t.Helper()
	saved, err := f.repo.Payment(f.ctx, f.paymentScope, f.id)
	if err != nil || saved.Status != status {
		t.Fatalf("payment status=%s error=%v", saved.Status, err)
	}
	for table, expected := range map[string]int{"payment_webhook_receipts": receipts, "audit_records": effects, "outbox_events": effects} {
		if got := webhookRowCount(t, f.ctx, f.admin, f.scope, table); got != expected {
			t.Fatalf("%s=%d want=%d", table, got, expected)
		}
	}
}

func TestA03PostgresPaymentWebhookRollbackAndRedelivery(t *testing.T) {
	for _, failure := range []struct{ name, table, timing string }{
		{"receipt", "payment_webhook_receipts", "BEFORE INSERT"},
		{"transition", "payments", "BEFORE UPDATE"},
		{"audit", "audit_records", "BEFORE INSERT"},
		{"outbox", "outbox_events", "BEFORE INSERT"},
		{"commit", "payment_webhook_receipts", "deferred"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			f := newPaymentWebhookFixture(t)
			restore := webhookFailureSwitch(t, f.ctx, f.admin, f.scope, failure.table, failure.timing)
			response := f.send("delivery-1")
			if response.Code != 503 || response.Body.String() != "{}" || response.Header().Get("Retry-After") == "" {
				t.Fatalf("failure response=%d %s", response.Code, response.Body.String())
			}
			f.assertState(t, payments.StatusCreated, 0, 2)
			restore()
			for range 2 {
				response = f.send("delivery-1")
				if response.Code != 200 || response.Body.String() != "provider-ack" {
					t.Fatalf("redelivery response=%d %s", response.Code, response.Body.String())
				}
			}
			f.assertState(t, payments.StatusSucceeded, 1, 3)
		})
	}
}

func TestA03PostgresPaymentWebhookConcurrentDeliveries(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			f := newPaymentWebhookFixture(t)
			f.db.SetMaxOpenConns(6)
			var wg sync.WaitGroup
			start := make(chan struct{})
			codes := make([]int, 6)
			deliveries := make([]string, 6)
			for i := range codes {
				deliveries[i] = "delivery-1"
				if !same {
					deliveries[i] = fmt.Sprintf("delivery-%d", i)
				}
				wg.Add(1)
				go func() { defer wg.Done(); <-start; codes[i] = f.send(deliveries[i]).Code }()
			}
			close(start)
			wg.Wait()
			for i, code := range codes {
				if code != 200 && code != 503 {
					t.Fatalf("concurrent response=%d", code)
				}
				if retry := f.send(deliveries[i]); retry.Code != 200 {
					t.Fatalf("retry=%d", retry.Code)
				}
			}
			receipts := 6
			if same {
				receipts = 1
			}
			f.assertState(t, payments.StatusSucceeded, receipts, 3)
		})
	}
}

func TestA04PostgresPaymentWebhookDatabaseUnavailableAfterVerification(t *testing.T) {
	f := newPaymentWebhookFixture(t)
	gateway, _ := f.api.registry.PaymentGateway(sdk.Account{}, nil)
	f.api.registry = createGatewayResolver{fakeWebhookGateway{verify: func(ctx context.Context, body, sig []byte) (sdk.PaymentWebhook, error) {
		result, err := gateway.VerifyPaymentWebhook(ctx, sdk.Account{}, nil, body, sig)
		f.db.Close()
		return result, err
	}}}
	if response := f.send("delivery-1"); response.Code != 503 {
		t.Fatalf("unavailable DB response=%d", response.Code)
	}
	db, err := sql.Open("pgx", os.Getenv("TORGNEXA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f.repo, _ = paymentsrepo.New(db)
	f.api.repository = f.repo
	f.api.accounts, _ = connectorrepo.New(db)
	f.api.registry = createGatewayResolver{gateway}
	f.assertState(t, payments.StatusCreated, 0, 2)
	if response := f.send("delivery-1"); response.Code != 200 {
		t.Fatalf("recovery response=%d", response.Code)
	}
	f.assertState(t, payments.StatusSucceeded, 1, 3)
}

func TestA03PostgresPaymentWebhookOptimisticConflictDoesNotConsumeReceipt(t *testing.T) {
	f := newPaymentWebhookFixture(t)
	application := "webhook-" + f.scope.WorkspaceID().String()
	if _, err := f.db.ExecContext(f.ctx, `SELECT set_config('application_name',$1,false)`, application); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.admin.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(f.ctx, `SELECT id FROM payments WHERE id=$1 FOR UPDATE`, f.id.String()); err != nil {
		t.Fatal(err)
	}
	response := make(chan int, 1)
	go func() { response <- f.send("delivery-conflict").Code }()
	// Wait for the real SELECT FOR UPDATE, after the observation read has
	// captured the previous version. No repository mock drives the conflict.
	blocked := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.admin.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, application).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("webhook did not reach the locked payment row")
	}
	if _, err := blocker.ExecContext(f.ctx, `UPDATE payments SET version=version+1,updated_at=clock_timestamp() WHERE id=$1`, f.id.String()); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-response:
		if code != 503 {
			t.Fatalf("optimistic conflict response=%d", code)
		}
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	f.assertState(t, payments.StatusCreated, 0, 2)
	if response := f.send("delivery-conflict"); response.Code != 200 {
		t.Fatalf("conflict retry=%d", response.Code)
	}
	f.assertState(t, payments.StatusSucceeded, 1, 3)
}

func TestA03PostgresPaymentWebhookReceiptCollision(t *testing.T) {
	f := newPaymentWebhookFixture(t)
	if response := f.send("delivery-1"); response.Code != 200 {
		t.Fatalf("initial=%d", response.Code)
	}
	gateway, _ := f.api.registry.PaymentGateway(sdk.Account{}, nil)
	f.api.registry = createGatewayResolver{fakeWebhookGateway{verify: func(ctx context.Context, body, sig []byte) (sdk.PaymentWebhook, error) {
		result, err := gateway.VerifyPaymentWebhook(ctx, sdk.Account{}, nil, body, sig)
		result.RemotePaymentID = "another-remote-payment"
		return result, err
	}}}
	if response := f.send("delivery-1"); response.Code != 503 {
		t.Fatalf("collision=%d", response.Code)
	}
	f.assertState(t, payments.StatusSucceeded, 1, 3)
}

func TestA03PostgresPaymentWebhookTerminalAndUnchangedStatuses(t *testing.T) {
	for _, observation := range []struct {
		remote string
		status payments.Status
	}{
		{"created", payments.StatusCreated}, {"succeeded", payments.StatusSucceeded},
		{"canceled", payments.StatusCanceled}, {"declined", payments.StatusFailed},
	} {
		t.Run(observation.remote, func(t *testing.T) {
			f := newPaymentWebhookFixture(t)
			gateway, _ := f.api.registry.PaymentGateway(sdk.Account{}, nil)
			f.api.registry = createGatewayResolver{fakeWebhookGateway{verify: func(ctx context.Context, body, sig []byte) (sdk.PaymentWebhook, error) {
				result, err := gateway.VerifyPaymentWebhook(ctx, sdk.Account{}, nil, body, sig)
				result.EventType = "payment_" + observation.remote
				return result, err
			}}}
			for range 2 {
				if response := f.send("delivery-1"); response.Code != 200 {
					t.Fatalf("delivery=%d", response.Code)
				}
			}
			effects := 3
			if observation.status == payments.StatusCreated {
				effects = 2
			}
			f.assertState(t, observation.status, 1, effects)
			saved, err := f.repo.Payment(f.ctx, f.paymentScope, f.id)
			if err != nil {
				t.Fatal(err)
			}
			if (saved.SucceededAt != nil) != (observation.status == payments.StatusSucceeded) {
				t.Fatal("incorrect succeeded_at")
			}
			if (saved.ReasonCode == "provider_declined") != (observation.status == payments.StatusFailed) {
				t.Fatal("incorrect failure reason")
			}
		})
	}
}
