package builtinruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type supportTestRuntime struct{}

func (supportTestRuntime) Secrets() sdk.SecretAccessor { return nil }

func supportTestAccount(t *testing.T, connectorID string) sdk.Account {
	t.Helper()
	manifest, err := sdk.CatalogManifest(connectorID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "runtime-account", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: connectorID, Family: manifest.Family, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef",
		Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestRuntimeSupportIsFailClosedAndDirectionExact(t *testing.T) {
	if !SupportsSync("wildberries", "products", "inbound") || SupportsSync("wildberries", "products", "outbound") {
		t.Fatal("Wildberries product support directions are not exact")
	}
	if !SupportsSync("woocommerce", "products", "bidirectional") || !SupportsSync("opencart", "products", "outbound") {
		t.Fatal("qualified storefront writes are not exposed")
	}
	if SupportsAccountConfiguration("avito") || SupportsCapability("avito", "classified.listings.read") || SupportsSync("avito", "products", "inbound") {
		t.Fatal("catalog-only connector gained executable authority")
	}
	if SupportsAccountConfiguration("deepseek") || !SupportsCapability("woocommerce", "products.read") || SupportsCapability("woocommerce", "orders.read") {
		t.Fatal("runtime surface/capability projection is inaccurate")
	}
	cbr, ok := SupportFor("cbr-fx")
	if !ok || cbr.Stage != SupportSeparateSurface || cbr.Surface != "finance" || len(cbr.OperationalCapabilities) != 1 || cbr.OperationalCapabilities[0] != "fx.rates.read" {
		t.Fatalf("CBR FX separate-surface support is inaccurate: %+v", cbr)
	}
	telegram, ok := SupportFor("telegram")
	if !ok || telegram.Stage != SupportSeparateSurface || telegram.Surface != "social" || !SupportsAccountConfiguration("telegram") || !SupportsCapability("telegram", "social.post.text") || SupportsCapability("telegram", "social.post.media") || SupportsSync("telegram", "products", "inbound") {
		t.Fatalf("Telegram social runtime support is inaccurate: %+v", telegram)
	}
}

func TestTelegramSocialPublisherAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"chat_id":-1001234567890}`), nil
	}
	if _, err := registry.SocialPublisher(supportTestAccount(t, "telegram"), load); err != nil {
		t.Fatalf("Telegram publisher unavailable: %v", err)
	}
	if _, err := registry.SocialPublisher(supportTestAccount(t, "avito"), load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted social publisher resolved: %v", err)
	}
}

func TestFXRateReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	if _, err := registry.FXRateReader("cbr-fx"); err != nil {
		t.Fatalf("CBR FX reader unavailable: %v", err)
	}
	if _, err := registry.FXRateReader("avito"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted FX reader resolved: %v", err)
	}
}

func TestEveryReadyIntegrationResolvesProductReader(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	ready := []string{"aliexpress-ru", "magnit-market", "megamarket", "moysklad", "onec", "opencart", "ozon", "prestashop", "wildberries", "woocommerce", "yandex-market"}
	for _, connectorID := range ready {
		if _, err := registry.ProductReader(supportTestAccount(t, connectorID), supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s product reader unavailable: %v", connectorID, err)
		}
	}
	if _, err := registry.ProductReader(supportTestAccount(t, "avito"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("planned connector resolved: %v", err)
	}
}
