package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/secretrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestA07PostgresSignedWebhookTopicBindingBeforeInbox(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	secretRepo, _ := secretrepo.New(db)
	keys, _ := secrets.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": make([]byte, 32)})
	provider, _ := secrets.NewLocalEncryptedProvider(secretRepo, keys)
	signing := strings.Repeat("synthetic", 8)
	material, _ := json.Marshal(map[string]string{"consumer_key": strings.Repeat("k", 32), "consumer_secret": strings.Repeat("s", 32), "webhook_secret": signing})
	metadata, err := provider.Create(ctx, scope, secrets.ClassConnectorToken, material)
	if err != nil {
		t.Fatal(err)
	}
	accountID := auditFixtureID()
	if _, err := admin.ExecContext(ctx, `INSERT INTO connector_accounts(id,organization_id,workspace_id,provider,family,status,secret_reference) VALUES($1,$2,$3,'woocommerce','marketplace','disabled',$4)`, accountID, scope.OrganizationID().String(), scope.WorkspaceID().String(), metadata.Reference.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE connector_accounts SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	configs, _ := connectorconfigrepo.New(db)
	accounts, _ := connectorrepo.New(db)
	processor, _ := inboxrepo.New(db)
	digest := sha256.Sum256([]byte(commerceTestReference))
	raw, _ := json.Marshal(map[string]any{"store_host": "shop.example.test", "store_currency": "RUB", "commerce_webhook_subscriptions": []map[string]string{{"reference_sha256": hex.EncodeToString(digest[:]), "topic": "product.updated"}}})
	if _, err := configs.Put(ctx, scope, accountID, raw, 0); err != nil {
		t.Fatal(err)
	}
	routes := newCommerceWebhookRoutes(accounts, configs, provider, builtinruntime.New(), processor)
	path := commerceWebhooksPathPrefix + "woocommerce/" + scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + accountID
	body := `{"id":123,"date_modified_gmt":"2026-09-08T10:00:00"}`
	mac := hmac.New(sha256.New, []byte(signing))
	mac.Write([]byte(body))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	send := func(query, topic, sig string) {
		r := httptest.NewRequest("POST", path+query, strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("X-WC-Webhook-Signature", sig)
		r.Header.Set("X-WC-Webhook-Topic", topic)
		w := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != "{}" {
			t.Fatalf("uniform response changed: %d", w.Code)
		}
	}
	count := func() int {
		var n int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM outbox_events WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	query := "?subscription=" + commerceTestReference
	send(query, "order.deleted", signature)
	send("", "product.updated", signature)
	send("?subscription="+strings.Repeat("A", 42)+"E", "product.updated", signature)
	send(query+"&subscription="+commerceTestReference, "product.updated", signature)
	send(query, "product.updated", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if count() != 0 {
		t.Fatal("untrusted topic/reference/signature consumed inbox")
	}
	send(query, "product.updated", signature)
	send(query, "product.updated", signature)
	if count() != 1 {
		t.Fatal("valid redelivery must produce exactly one outbox event")
	}
	var eventType string
	if err := admin.QueryRowContext(ctx, `SELECT payload->>'event_type' FROM outbox_events WHERE workspace_id=$1`, scope.WorkspaceID().String()).Scan(&eventType); err != nil || eventType != "product.updated" {
		t.Fatal("wrong durable topic", err, eventType)
	}
	if _, err := configs.Put(ctx, scope, accountID, json.RawMessage(`{"store_host":"shop.example.test","store_currency":"RUB"}`), 1); err != nil {
		t.Fatal(err)
	}
	body = `{"id":456,"date_modified_gmt":"2026-09-08T11:00:00"}`
	mac = hmac.New(sha256.New, []byte(signing))
	mac.Write([]byte(body))
	signature = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	send(query, "product.updated", signature)
	if count() != 1 {
		t.Fatal("revoked subscription delivered")
	}
}
