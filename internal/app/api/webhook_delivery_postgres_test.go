package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/inbox"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

func webhookEncryptedSecrets(t *testing.T, db *sql.DB) *secrets.LocalEncryptedProvider {
	t.Helper()
	repo, err := secretrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := secrets.NewLocalEncryptedProvider(repo, keys)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func webhookAccountFixture(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, provider, family, reference string) string {
	t.Helper()
	id := auditFixtureID()
	if _, err := admin.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,provider,family,status,secret_reference) VALUES($1,$2,$3,$4,$5,'disabled',$6)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), provider, family, reference); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE connector_accounts SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

type webhookAdapterFixture struct {
	AdapterID          string          `json:"adapter_id"`
	VerificationHeader string          `json:"verification_header"`
	TopicHeader        string          `json:"topic_header"`
	Body               json.RawMessage `json:"body"`
}

func readWebhookFixture(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func TestA04PostgresCommerceWebhookOutboxFailureAndRetry(t *testing.T) {
	var fixture webhookAdapterFixture
	readWebhookFixture(t, "testdata/commerce-webhook-delivery.json", &fixture)
	ctx, db, admin, scope := auditPostgres(t)
	provider := webhookEncryptedSecrets(t, db)
	signing := strings.Repeat("synthetic", 8)
	material, _ := json.Marshal(map[string]string{"consumer_key": strings.Repeat("k", 32), "consumer_secret": strings.Repeat("s", 32), "webhook_secret": signing})
	metadata, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, material)
	if err != nil {
		t.Fatal(err)
	}
	accountID := webhookAccountFixture(t, ctx, admin, scope, fixture.AdapterID, "marketplace", metadata.Reference.String())
	configs, _ := connectorconfigrepo.New(db)
	accounts, _ := connectorrepo.New(db)
	processor, _ := inboxrepo.New(db)
	digest := sha256.Sum256([]byte(commerceTestReference))
	raw, _ := json.Marshal(map[string]any{"store_host": "shop.example.test", "store_currency": "RUB", "commerce_webhook_subscriptions": []map[string]string{{"reference_sha256": hex.EncodeToString(digest[:]), "topic": "product.updated"}}})
	if _, err := configs.Put(ctx, scope, accountID, raw, 0); err != nil {
		t.Fatal(err)
	}
	routes := newCommerceWebhookRoutes(accounts, configs, provider, builtinruntime.New(), processor)
	path := commerceWebhooksPathPrefix + fixture.AdapterID + "/" + scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + accountID + "?subscription=" + commerceTestReference
	// A timestamp-free body exercises arrival-time inference on redelivery.
	body := `{"id":123}`
	mac := hmac.New(sha256.New, []byte(signing))
	mac.Write([]byte(body))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	send := func(valid bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
		sig := signature
		if !valid {
			sig = base64.StdEncoding.EncodeToString(make([]byte, 32))
		}
		r.Header.Set(fixture.VerificationHeader, sig)
		r.Header.Set(fixture.TopicHeader, "product.updated")
		w := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(w, r)
		return w
	}
	assertWebhookPersistenceRetry(t, ctx, admin, scope, send)
}

func TestA04PostgresSocialWebhookOutboxFailureAndRetry(t *testing.T) {
	var fixtures []webhookAdapterFixture
	readWebhookFixture(t, "testdata/social-webhook-deliveries.json", &fixtures)
	if len(fixtures) != 2 {
		t.Fatal("expected both admitted social webhook fixtures")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.AdapterID, func(t *testing.T) {
			socialWebhookPersistenceRetry(t, fixture.AdapterID, fixture.VerificationHeader, string(fixture.Body))
		})
	}
}

func socialWebhookPersistenceRetry(t *testing.T, connectorID, header, body string) {
	t.Helper()
	ctx, db, admin, scope := auditPostgres(t)
	provider := webhookEncryptedSecrets(t, db)
	secret := strings.Repeat("synthetic", 8)
	metadata, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	accountID := webhookAccountFixture(t, ctx, admin, scope, connectorID, "social", metadata.Reference.String())
	if _, err := admin.ExecContext(ctx, `INSERT INTO connector_account_capability_history(organization_id,workspace_id,connector_account_id,account_version,capability,direction,risk_class,approval_required,enabled) VALUES($1,$2,$3,2,'social.webhooks','read','read',false,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID); err != nil {
		t.Fatal(err)
	}
	configs, _ := connectorconfigrepo.New(db)
	accounts, _ := connectorrepo.New(db)
	processor, _ := inboxrepo.New(db)
	raw := json.RawMessage(fmt.Sprintf(`{"chat_id":-70801090403050,"webhook_secret_reference":%q}`, metadata.Reference.String()))
	if _, err := configs.Put(ctx, scope, accountID, raw, 0); err != nil {
		t.Fatal(err)
	}
	routes := newSocialWebhookRoutes(accounts, configs, provider, builtinruntime.New(), processor)
	path := fmt.Sprintf("%s%s/%s/%s/%s", socialWebhooksPathPrefix, connectorID, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	send := func(valid bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
		token := secret
		if !valid {
			token = strings.Repeat("invalid", 8)
		}
		r.Header.Set(header, token)
		w := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(w, r)
		return w
	}
	assertWebhookPersistenceRetry(t, ctx, admin, scope, send)
}

func assertWebhookPersistenceRetry(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope, send func(bool) *httptest.ResponseRecorder) {
	t.Helper()
	restore := webhookFailureSwitch(t, ctx, admin, scope, "outbox_events", "BEFORE INSERT")
	if response := send(false); response.Code != 200 || response.Body.String() != "{}" || response.Header().Get("Retry-After") != "" {
		t.Fatalf("unverified response=%d", response.Code)
	}
	if response := send(true); response.Code != 503 || response.Body.String() != "{}" || response.Header().Get("Retry-After") == "" {
		t.Fatalf("failed commit response=%d", response.Code)
	}
	for _, table := range []string{"outbox_events", "inbox_receipts"} {
		if n := webhookRowCount(t, ctx, admin, scope, table); n != 0 {
			t.Fatalf("failed delivery %s=%d", table, n)
		}
	}
	restore()
	for range 2 {
		if response := send(true); response.Code != 200 || response.Body.String() != "{}" {
			t.Fatalf("redelivery response=%d", response.Code)
		}
	}
	for _, table := range []string{"outbox_events", "inbox_receipts"} {
		if n := webhookRowCount(t, ctx, admin, scope, table); n != 1 {
			t.Fatalf("committed delivery %s=%d", table, n)
		}
	}
}

func TestA04PostgresCommerceReplayRetainsTimeAndDetectsCollision(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	processor, _ := inboxrepo.New(db)
	account := testWebhookAccount()
	account.Family = sdk.FamilyMarketplace
	account.OrganizationID, account.WorkspaceID = scope.OrganizationID().String(), scope.WorkspaceID().String()
	dedup := commerceWebhookDeduplicator{processor: processor, scope: scope}
	first := time.Now().UTC()
	claim := sdk.CommerceWebhookClaim{DeliveryID: "sha256:" + strings.Repeat("a", 64), EventType: "product.updated", ResourceKind: "product", ResourceRemoteID: "123", OccurredAt: first, CanonicalPayload: json.RawMessage(`{"id":123}`)}
	if duplicate, err := dedup.ClaimCommerceWebhook(ctx, account, claim); err != nil || duplicate {
		t.Fatalf("initial=%v %v", duplicate, err)
	}
	claim.OccurredAt = first.Add(time.Minute)
	if duplicate, err := dedup.ClaimCommerceWebhook(ctx, account, claim); err != nil || !duplicate {
		t.Fatalf("replay=%v %v", duplicate, err)
	}
	claim.CanonicalPayload = json.RawMessage(`{"id":456}`)
	if _, err := dedup.ClaimCommerceWebhook(ctx, account, claim); !errors.Is(err, inbox.ErrCollision) {
		t.Fatalf("payload collision=%v", err)
	}
	if count := webhookRowCount(t, ctx, admin, scope, "outbox_events"); count != 1 {
		t.Fatalf("outbox=%d", count)
	}
}
