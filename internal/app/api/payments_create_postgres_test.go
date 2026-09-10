package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type lostCreateGateway struct {
	fakeWebhookGateway
	url string
}

func (g lostCreateGateway) CreatePayment(ctx context.Context, _ sdk.Account, _ sdk.Runtime, input sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	raw, _ := json.Marshal(input)
	r, err := http.NewRequestWithContext(ctx, "POST", g.url, bytes.NewReader(raw))
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	r.Header.Set("Idempotency-Key", input.IdempotencyKey)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	response.Body.Close()
	return sdk.PaymentCreateResult{RemoteID: "synthetic-remote-payment", Status: "pending"}, nil
}

type createGatewayResolver struct{ gateway builtinruntime.PaymentGateway }

func (r createGatewayResolver) PaymentGateway(sdk.Account, builtinruntime.ConfigLoader) (builtinruntime.PaymentGateway, error) {
	return r.gateway, nil
}
func TestA05PostgresCreateResponseLostDoesNotReplayCharge(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	accounts, _ := connectorrepo.New(db)
	repo, _ := paymentsrepo.New(db)
	accountID := auditFixtureID()
	if _, err := admin.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,provider,family,status) VALUES($1,$2,$3,'synthetic','payment','disabled')`, accountID, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE connector_accounts SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	var remoteCalls atomic.Int64
	var reply atomic.Bool
	keys := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls.Add(1)
		keys <- r.Header.Get("Idempotency-Key")
		if reply.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	api := paymentsAPI{repository: repo, accounts: accounts, secrets: fakeWebhookSecrets{}, registry: createGatewayResolver{lostCreateGateway{url: server.URL}}}
	id := auditFixtureID()
	body := fmt.Sprintf(`{"id":%q,"connector_account_id":%q,"amount":{"minor_units":15000,"currency":"RUB"},"purpose":"Synthetic","expires_in_seconds":3600}`, id, accountID)
	for range 3 {
		w := httptest.NewRecorder()
		api.createPayment(w, auditFixtureRequest(ctx, scope, "POST", paymentsPath, id, body))
		if w.Code != 201 {
			t.Fatalf("create=%d %s", w.Code, w.Body.String())
		}
	}
	if remoteCalls.Load() != 1 || <-keys != id {
		t.Fatal("ambiguous create was replayed or key changed")
	}
	paymentScope, _ := payments.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	paymentID, _ := payments.ParsePaymentID(id)
	payment, err := repo.Payment(ctx, paymentScope, paymentID)
	if err != nil || payment.Status != payments.StatusPending || payment.RemoteID != "" || payment.Version != 1 {
		t.Fatalf("ambiguous state=%+v err=%v", payment, err)
	}
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("retry duplicated payment audit")
	}
	reply.Store(true)
	nextID := auditFixtureID()
	nextBody := strings.ReplaceAll(body, id, nextID)
	for range 2 {
		w := httptest.NewRecorder()
		api.createPayment(w, auditFixtureRequest(ctx, scope, "POST", paymentsPath, nextID, nextBody))
		if w.Code != 201 {
			t.Fatalf("successful create status=%d", w.Code)
		}
	}
	nextPaymentID, _ := payments.ParsePaymentID(nextID)
	saved, err := repo.Payment(ctx, paymentScope, nextPaymentID)
	if err != nil || saved.Status != payments.StatusCreated || saved.RemoteID != "synthetic-remote-payment" || remoteCalls.Load() != 2 {
		t.Fatal("normal create/replay failed", err)
	}

}
