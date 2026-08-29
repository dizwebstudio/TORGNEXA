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
	if !SupportsSync("prestashop", "prices", "outbound") || !SupportsSync("prestashop", "inventory", "outbound") || SupportsSync("prestashop", "prices", "inbound") {
		t.Fatal("PrestaShop commerce write directions are not exact")
	}
	if !SupportsAccountConfiguration("avito") || !HealthOnly("avito") || SupportsCapability("avito", "classified.listings.read") || SupportsSync("avito", "products", "inbound") {
		t.Fatal("classified health-only surface projection is inaccurate")
	}
	for _, connectorID := range []string{"lamoda", "mvideo"} {
		marketplace, ok := SupportFor(connectorID)
		if !ok || marketplace.Stage != SupportSeparateSurface || marketplace.Surface != "marketplace" || !marketplace.HealthOnly || !SupportsAccountConfiguration(connectorID) || SupportsCapability(connectorID, "products.read") || SupportsSync(connectorID, "products", "inbound") {
			t.Fatalf("%s marketplace health-only support is inaccurate: %+v", connectorID, marketplace)
		}
	}
	if SupportsAccountConfiguration("deepseek") || !SupportsCapability("woocommerce", "products.read") || SupportsCapability("woocommerce", "orders.read") {
		t.Fatal("runtime surface/capability projection is inaccurate")
	}
	claude, ok := SupportFor("claude")
	if !ok || claude.Stage != SupportSeparateSurface || claude.Surface != "ai_providers" || SupportsAccountConfiguration("claude") || SupportsCapability("claude", "ai.completion.generate") || SupportsSync("claude", "products", "inbound") {
		t.Fatalf("Claude AI provider support is inaccurate: %+v", claude)
	}
	for _, connectorID := range []string{"ollama", "lm-studio", "open-webui"} {
		local, ok := SupportFor(connectorID)
		if !ok || local.Stage != SupportSeparateSurface || local.Surface != "ai_providers" || SupportsAccountConfiguration(connectorID) || SupportsCapability(connectorID, "ai.completion.generate") || SupportsSync(connectorID, "products", "inbound") {
			t.Fatalf("%s local AI provider support is inaccurate: %+v", connectorID, local)
		}
	}
	cbr, ok := SupportFor("cbr-fx")
	if !ok || cbr.Stage != SupportSeparateSurface || cbr.Surface != "finance" || len(cbr.OperationalCapabilities) != 1 || cbr.OperationalCapabilities[0] != "fx.rates.read" {
		t.Fatalf("CBR FX separate-surface support is inaccurate: %+v", cbr)
	}
	telegram, ok := SupportFor("telegram")
	if !ok || telegram.Stage != SupportSeparateSurface || telegram.Surface != "social" || !SupportsAccountConfiguration("telegram") || !SupportsCapability("telegram", "social.post.text") || SupportsCapability("telegram", "social.post.media") || SupportsSync("telegram", "products", "inbound") || SocialTextLimit("telegram") != 4096 {
		t.Fatalf("Telegram social runtime support is inaccurate: %+v", telegram)
	}
	max, ok := SupportFor("max-messenger")
	if !ok || max.Stage != SupportSeparateSurface || max.Surface != "social" || !SupportsAccountConfiguration("max-messenger") || !SupportsCapability("max-messenger", "social.post.text") || SupportsCapability("max-messenger", "social.post.media") || SupportsSync("max-messenger", "products", "inbound") || SocialTextLimit("max-messenger") != 4000 {
		t.Fatalf("MAX social runtime support is inaccurate: %+v", max)
	}
	if SocialTextLimit("avito") != 0 {
		t.Fatal("health-only connector gained an executable social text limit")
	}
	bitrix, ok := SupportFor("bitrix24")
	if !ok || bitrix.Stage != SupportSeparateSurface || bitrix.Surface != "crm" || !SupportsAccountConfiguration("bitrix24") || !SupportsCapability("bitrix24", "crm.entities.read") || !SupportsCapability("bitrix24", "crm.productrows.write") || SupportsSync("bitrix24", "products", "inbound") {
		t.Fatalf("Bitrix24 CRM runtime support is inaccurate: %+v", bitrix)
	}
	storefront, ok := SupportFor("bitrix")
	if !ok || storefront.Stage != SupportReady || storefront.Surface != "integrations" || !SupportsAccountConfiguration("bitrix") || !SupportsCapability("bitrix", "products.read") || !SupportsCapability("bitrix", "products.write") || !SupportsCapability("bitrix", "prices.write") || !SupportsCapability("bitrix", "inventory.write") || !SupportsSync("bitrix", "products", "bidirectional") || !SupportsSync("bitrix", "prices", "outbound") || !SupportsSync("bitrix", "inventory", "outbound") || SupportsCapability("bitrix", "prices.read") || SupportsCapability("bitrix", "orders.read") {
		t.Fatalf("1C-Bitrix storefront runtime support is inaccurate: %+v", storefront)
	}
	csCart, ok := SupportFor("cs-cart")
	if !ok || csCart.Stage != SupportReady || csCart.Surface != "integrations" || !SupportsAccountConfiguration("cs-cart") || !SupportsCapability("cs-cart", "products.read") || !SupportsCapability("cs-cart", "products.write") || !SupportsSync("cs-cart", "products", "bidirectional") || SupportsCapability("cs-cart", "orders.read") {
		t.Fatalf("CS-Cart storefront runtime support is inaccurate: %+v", csCart)
	}
	for _, connectorID := range []string{"cdek", "dellin", "fivepost", "ozon-delivery", "pek", "pochta-russia"} {
		carrier, ok := SupportFor(connectorID)
		if !ok || carrier.Stage != SupportSeparateSurface || carrier.Surface != "logistics" || !SupportsAccountConfiguration(connectorID) || len(carrier.OperationalCapabilities) != 0 || SupportsCapability(connectorID, "logistics.shipment.create") || SupportsSync(connectorID, "products", "inbound") {
			t.Fatalf("%s logistics verification support is inaccurate: %+v", connectorID, carrier)
		}
	}
	dolyami, ok := SupportFor("dolyami")
	if !ok || dolyami.Stage != SupportSeparateSurface || dolyami.Surface != "finance" || !dolyami.HealthOnly || !SupportsAccountConfiguration("dolyami") || SupportsCapability("dolyami", "payments.create") || SupportsSync("dolyami", "products", "inbound") {
		t.Fatalf("Dolyami health-only payment support is inaccurate: %+v", dolyami)
	}
}

func TestProductStatusTranslationMatchesWriterContracts(t *testing.T) {
	registry := New()
	cases := map[string]map[string]string{
		"bitrix":      {"draft": "N", "active": "Y", "archived": "N"},
		"cs-cart":     {"draft": "D", "active": "A", "archived": "H"},
		"magento":     {"draft": "disabled", "active": "enabled", "archived": "disabled"},
		"medusa":      {"draft": "draft", "active": "published", "archived": "rejected"},
		"opencart":    {"draft": "draft", "active": "publish", "archived": "draft"},
		"saleor":      {"draft": "unpublished", "active": "published", "archived": "unpublished"},
		"shopify":     {"draft": "draft", "active": "active", "archived": "archived"},
		"shopware":    {"draft": "inactive", "active": "active", "archived": "inactive"},
		"woocommerce": {"draft": "draft", "active": "publish", "archived": "private"},
	}
	for connectorID, statuses := range cases {
		for canonical, expected := range statuses {
			if got, ok := registry.ProductStatus(connectorID, canonical); !ok || got != expected {
				t.Fatalf("ProductStatus(%q, %q) = %q, %v; want %q, true", connectorID, canonical, got, ok, expected)
			}
		}
	}
	if _, ok := registry.ProductStatus("prestashop", "active"); ok {
		t.Fatal("product status must remain unavailable for a read-only product runtime")
	}
}

func TestSocialPublisherAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"chat_id":-70801090403050}`), nil
	}
	if _, err := registry.SocialPublisher(supportTestAccount(t, "telegram"), load); err != nil {
		t.Fatalf("Telegram publisher unavailable: %v", err)
	}
	if _, err := registry.SocialPublisher(supportTestAccount(t, "max-messenger"), load); err != nil {
		t.Fatalf("MAX publisher unavailable: %v", err)
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
	ready := []string{"aliexpress-ru", "bitrix", "cs-cart", "magento", "magnit-market", "medusa", "megamarket", "moysklad", "onec", "opencart", "ozon", "prestashop", "shopify", "shopware", "wildberries", "woocommerce", "yandex-market"}
	for _, connectorID := range ready {
		if _, err := registry.ProductReader(supportTestAccount(t, connectorID), supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s product reader unavailable: %v", connectorID, err)
		}
	}
	if _, err := registry.ProductReader(supportTestAccount(t, "avito"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("health-only connector resolved as product reader: %v", err)
	}
}

func TestPrestaShopCommerceWriteAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "prestashop")
	if !registry.SupportsPriceWrite(account) || !registry.SupportsInventoryWrite(account) {
		t.Fatal("PrestaShop commerce write capabilities are not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.ru","base_path":"","store_currency":"RUB","language_id":1,"shop_id":0}`), nil
	}
	if _, err := registry.PriceWriter(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("PrestaShop price writer unavailable: %v", err)
	}
	if _, err := registry.InventoryWriter(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("PrestaShop inventory writer unavailable: %v", err)
	}
	if _, err := registry.PriceWriter(supportTestAccount(t, "wildberries"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted price writer resolved: %v", err)
	}
}

func TestBitrixPriceWriterAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "bitrix")
	if !registry.SupportsPriceWrite(account) || !SupportsCapability("bitrix", "prices.write") || !SupportsSync("bitrix", "prices", "outbound") {
		t.Fatal("1C-Bitrix price write runtime support is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","base_path":"","catalog_iblock_id":23,"store_currency":"RUB","price_type_id":1}`), nil
	}
	if _, err := registry.PriceWriter(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("1C-Bitrix price writer unavailable: %v", err)
	}
	if _, err := registry.PriceWriter(account, supportTestRuntime{}, nil); !errors.Is(err, ErrConfigurationNeeded) {
		t.Fatalf("missing 1C-Bitrix runtime configuration returned %v", err)
	}
}

func TestBitrixInventoryWriterAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "bitrix")
	if !registry.SupportsInventoryWrite(account) || !SupportsCapability("bitrix", "inventory.write") || !SupportsSync("bitrix", "inventory", "outbound") {
		t.Fatal("1C-Bitrix inventory write runtime support is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","base_path":"","catalog_iblock_id":23,"store_currency":"RUB","price_type_id":1}`), nil
	}
	if _, err := registry.InventoryWriter(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("1C-Bitrix inventory writer unavailable: %v", err)
	}
	if _, err := registry.InventoryWriter(account, supportTestRuntime{}, nil); !errors.Is(err, ErrConfigurationNeeded) {
		t.Fatalf("missing 1C-Bitrix runtime configuration returned %v", err)
	}
}

func TestHealthOnlyCatalogProvidersResolveProbeConnector(t *testing.T) {
	registry := New()
	for _, connectorID := range []string{"auto-ru", "avito", "chestny-znak", "cian", "diadoc", "egais", "instagram", "lamoda", "mvideo", "odnoklassniki", "rutube", "saby-edo", "threads", "vetis-mercury", "vk", "youtube"} {
		connector, err := registry.healthConnector(supportTestAccount(t, connectorID), func(context.Context, string) (json.RawMessage, error) {
			return json.RawMessage(`{"probe_url":"https://example.com/health"}`), nil
		})
		if err != nil || connector == nil || connector.Manifest().ID != connectorID {
			t.Fatalf("%s health-only connector unavailable: connector=%T err=%v", connectorID, connector, err)
		}
	}
}

func TestBitrix24CRMRegistryAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"portal_host":"tenant.bitrix24.com"}`), nil
	}
	account := supportTestAccount(t, "bitrix24")
	if _, err := registry.CRMReader(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("Bitrix24 CRM reader unavailable: %v", err)
	}
	if _, err := registry.CRMWriter(account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("Bitrix24 CRM writer unavailable: %v", err)
	}
	if _, err := registry.CRMReader(supportTestAccount(t, "avito"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted connector resolved as CRM reader: %v", err)
	}
}

func TestLogisticsHealthRegistryAdmissionIsExact(t *testing.T) {
	registry := New()
	for _, connectorID := range []string{"cdek", "dellin", "fivepost", "ozon-delivery", "pek", "pochta-russia"} {
		connector, err := registry.healthConnector(supportTestAccount(t, connectorID), nil)
		if err != nil || connector == nil || connector.Manifest().ID != connectorID {
			t.Fatalf("%s health connector unavailable: connector=%T err=%v", connectorID, connector, err)
		}
	}
}

func TestOzonPayHealthRegistryAdmissionIsExact(t *testing.T) {
	registry := New()
	connector, err := registry.healthConnector(supportTestAccount(t, "ozon-pay"), nil)
	if err != nil || connector == nil || connector.Manifest().ID != "ozon-pay" {
		t.Fatalf("ozon-pay health connector unavailable: connector=%T err=%v", connector, err)
	}
	if SupportsCapability("ozon-pay", "payments.create") || SupportsCapability("ozon-delivery", "logistics.shipment.create") {
		t.Fatal("Ozon qualification-gated capabilities became executable")
	}
}

func TestDolyamiHealthRegistryAdmissionIsExact(t *testing.T) {
	registry := New()
	connector, err := registry.healthConnector(supportTestAccount(t, "dolyami"), func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"probe_url":"https://api.example.test/health"}`), nil
	})
	if err != nil || connector == nil || connector.Manifest().ID != "dolyami" {
		t.Fatalf("dolyami health connector unavailable: connector=%T err=%v", connector, err)
	}
	if SupportsCapability("dolyami", "payments.create") {
		t.Fatal("Dolyami payment mutation became executable")
	}
}
