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
	if !SupportsSync("prestashop", "prices", "bidirectional") || !SupportsSync("prestashop", "inventory", "bidirectional") || !SupportsSync("prestashop", "orders", "bidirectional") || !SupportsSync("opencart", "orders", "bidirectional") {
		t.Fatal("storefront commerce sync directions are not exact")
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
	if SupportsAccountConfiguration("deepseek") || !SupportsCapability("woocommerce", "products.read") || !SupportsCapability("woocommerce", "orders.read") {
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
	if !ok || telegram.Stage != SupportSeparateSurface || telegram.Surface != "social" || !SupportsAccountConfiguration("telegram") || !SupportsCapability("telegram", "social.post.text") || !SupportsCapability("telegram", "social.post.media") || !SupportsCapability("telegram", "social.post.video") || !SupportsCapability("telegram", "social.post.buttons") || SupportsSync("telegram", "products", "inbound") || SocialTextLimit("telegram") != 4096 {
		t.Fatalf("Telegram social runtime support is inaccurate: %+v", telegram)
	}
	max, ok := SupportFor("max-messenger")
	if !ok || max.Stage != SupportSeparateSurface || max.Surface != "social" || !SupportsAccountConfiguration("max-messenger") || !SupportsCapability("max-messenger", "social.post.text") || !SupportsCapability("max-messenger", "social.post.media") || !SupportsCapability("max-messenger", "social.post.video") || !SupportsCapability("max-messenger", "social.post.buttons") || SupportsSync("max-messenger", "products", "inbound") || SocialTextLimit("max-messenger") != 4000 {
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
	if !ok || storefront.Stage != SupportReady || storefront.Surface != "integrations" || !SupportsAccountConfiguration("bitrix") || !SupportsCapability("bitrix", "products.read") || !SupportsCapability("bitrix", "products.write") || !SupportsCapability("bitrix", "prices.write") || !SupportsCapability("bitrix", "prices.read") || !SupportsCapability("bitrix", "inventory.write") || !SupportsCapability("bitrix", "inventory.read") || !SupportsCapability("bitrix", "orders.read") || !SupportsCapability("bitrix", "orders.status.write") || !SupportsSync("bitrix", "products", "bidirectional") || !SupportsSync("bitrix", "prices", "outbound") || !SupportsSync("bitrix", "inventory", "outbound") || !SupportsSync("bitrix", "orders", "bidirectional") {
		t.Fatalf("1C-Bitrix storefront runtime support is inaccurate: %+v", storefront)
	}
	csCart, ok := SupportFor("cs-cart")
	if !ok || csCart.Stage != SupportReady || csCart.Surface != "integrations" || !SupportsAccountConfiguration("cs-cart") || !SupportsCapability("cs-cart", "products.read") || !SupportsCapability("cs-cart", "products.write") || !SupportsCapability("cs-cart", "prices.read") || !SupportsCapability("cs-cart", "prices.write") || !SupportsCapability("cs-cart", "inventory.read") || !SupportsCapability("cs-cart", "inventory.write") || !SupportsCapability("cs-cart", "orders.read") || !SupportsCapability("cs-cart", "orders.status.write") || !SupportsSync("cs-cart", "products", "bidirectional") || !SupportsSync("cs-cart", "prices", "bidirectional") || !SupportsSync("cs-cart", "inventory", "bidirectional") || !SupportsSync("cs-cart", "orders", "bidirectional") {
		t.Fatalf("CS-Cart storefront runtime support is inaccurate: %+v", csCart)
	}
	for _, connectorID := range []string{"cdek", "dellin", "fivepost", "ozon-delivery", "pek", "pochta-russia"} {
		carrier, ok := SupportFor(connectorID)
		if !ok || carrier.Stage != SupportSeparateSurface || carrier.Surface != "logistics" || !SupportsAccountConfiguration(connectorID) || (connectorID != "cdek" && connectorID != "dellin" && connectorID != "pek" && connectorID != "pochta-russia" && SupportsCapability(connectorID, "logistics.shipment.create")) || SupportsSync(connectorID, "products", "inbound") {
			t.Fatalf("%s logistics verification support is inaccurate: %+v", connectorID, carrier)
		}
		if connectorID == "cdek" || connectorID == "dellin" || connectorID == "pek" || connectorID == "pochta-russia" {
			wantCapabilities := 1
			if connectorID == "cdek" {
				wantCapabilities = 8
			} else if connectorID == "dellin" {
				wantCapabilities = 6
			} else if connectorID == "pek" {
				wantCapabilities = 6
			} else if connectorID == "pochta-russia" {
				wantCapabilities = 8
			}
			if len(carrier.OperationalCapabilities) != wantCapabilities || !SupportsCapability(connectorID, "pickup.points.read") {
				t.Fatalf("%s pickup-point support is inaccurate: %+v", connectorID, carrier)
			}
			if (connectorID == "cdek" || connectorID == "dellin" || connectorID == "pek" || connectorID == "pochta-russia") && !SupportsCapability(connectorID, "logistics.track.read") {
				t.Fatalf("%s tracking support is inaccurate: %+v", connectorID, carrier)
			}
			if (connectorID == "cdek" || connectorID == "dellin" || connectorID == "pek" || connectorID == "pochta-russia") && !SupportsCapability(connectorID, "logistics.rates.read") {
				t.Fatalf("%s rate support is inaccurate: %+v", connectorID, carrier)
			}
			if connectorID == "cdek" || connectorID == "dellin" || connectorID == "pek" || connectorID == "pochta-russia" {
				if !SupportsCapability(connectorID, "logistics.shipment.cancel") {
					t.Fatalf("%s cancellation support is inaccurate: %+v", connectorID, carrier)
				}
			}
			if connectorID == "cdek" && !SupportsCapability(connectorID, "logistics.shipment.create") {
				t.Fatalf("%s shipment creation support is inaccurate: %+v", connectorID, carrier)
			}
			if connectorID == "cdek" && !SupportsCapability(connectorID, "logistics.webhooks.verify") {
				t.Fatalf("%s webhook verification support is inaccurate: %+v", connectorID, carrier)
			}
			if connectorID == "dellin" && !SupportsCapability(connectorID, "logistics.shipment.create") {
				t.Fatalf("%s shipment creation support is inaccurate: %+v", connectorID, carrier)
			}
			if connectorID == "pek" && !SupportsCapability(connectorID, "logistics.shipment.create") {
				t.Fatalf("%s shipment creation support is inaccurate: %+v", connectorID, carrier)
			}
			if connectorID == "pochta-russia" && !SupportsCapability(connectorID, "logistics.shipment.create") {
				t.Fatalf("%s shipment creation support is inaccurate: %+v", connectorID, carrier)
			}
			if (connectorID == "cdek" || connectorID == "dellin" || connectorID == "pek" || connectorID == "pochta-russia") && !SupportsCapability(connectorID, "logistics.label.read") {
				t.Fatalf("%s label support is inaccurate: %+v", connectorID, carrier)
			}
			if (connectorID == "cdek" || connectorID == "pochta-russia") && !SupportsCapability(connectorID, "logistics.return.create") {
				t.Fatalf("%s return support is inaccurate: %+v", connectorID, carrier)
			}
		} else if len(carrier.OperationalCapabilities) != 0 {
			t.Fatalf("%s must remain capability-free until qualification: %+v", connectorID, carrier)
		}
	}
	dolyami, ok := SupportFor("dolyami")
	if !ok || dolyami.Stage != SupportSeparateSurface || dolyami.Surface != "finance" || !dolyami.HealthOnly || !SupportsAccountConfiguration("dolyami") || SupportsCapability("dolyami", "payments.create") || SupportsSync("dolyami", "products", "inbound") {
		t.Fatalf("Dolyami health-only payment support is inaccurate: %+v", dolyami)
	}
}

func TestPaymentWebhookAdmissionIsExact(t *testing.T) {
	registry := New()
	for _, connectorID := range []string{"robokassa", "yookassa", "sbp"} {
		if !SupportsCapability(connectorID, "payments.webhooks") {
			t.Fatalf("%s webhook capability is not admitted", connectorID)
		}
		var load ConfigLoader
		if connectorID == "sbp" {
			load = func(context.Context, string) (json.RawMessage, error) {
				return json.RawMessage(`{"gateway_host":"sbp-gateway.example.test","member_id":"100000001"}`), nil
			}
		}
		gateway, err := registry.PaymentGateway(supportTestAccount(t, connectorID), load)
		if err != nil || gateway == nil {
			t.Fatalf("%s payment gateway unavailable for webhook route: gateway=%T err=%v", connectorID, gateway, err)
		}
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

func TestSocialWebhookReceiverAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"chat_id":-70801090403050,"webhook_secret_reference":"sec:v1:1123456789abcdef0123456789abcdef"}`), nil
	}
	receiver, err := registry.SocialWebhookReceiver(supportTestAccount(t, "max-messenger"), load)
	if err != nil {
		t.Fatalf("MAX webhook receiver unavailable: %v", err)
	}
	if _, ok := receiver.(sdk.SocialWebhookReceiver); !ok {
		t.Fatalf("MAX receiver has unexpected type %T", receiver)
	}
	if _, err := registry.SocialWebhookReceiver(supportTestAccount(t, "telegram"), load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted Telegram webhook receiver resolved: %v", err)
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

func TestERPInventoryReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "onec")
	if !SupportsCapability("onec", "erp.inventory.read") {
		t.Fatal("1C ERP inventory capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"host":"erp.example.ru","base_path":"/odata/standard.odata","catalog":{"resource":"Catalog_Номенклатура","id_field":"Ref_Key","code_field":"Code","sku_field":"Артикул","title_field":"Description","brand_field":"Бренд","revision_field":"DataVersion","archived_field":"DeletionMark"},"inventory":{"resource":"AccumulationRegister_ТоварыНаСкладах","function":"Balance","product_field":"Номенклатура_Key","location_field":"Склад_Key","quantity_field":"КоличествоBalance"}}`), nil
	}
	reader, err := registry.ERPInventoryReader(account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("1C ERP inventory reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ERPInventoryReader); !ok {
		t.Fatalf("unexpected ERP inventory reader type %T", reader)
	}
	if _, err := registry.ERPInventoryReader(supportTestAccount(t, "moysklad"), supportTestRuntime{}, load); err != nil {
		t.Fatalf("MoySklad ERP inventory reader unavailable: %v", err)
	}
}

func TestERPOrderReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "moysklad")
	if !SupportsCapability("moysklad", "erp.orders.read") {
		t.Fatal("MoySklad ERP order capability is not admitted")
	}
	reader, err := registry.ERPOrderReader(account, supportTestRuntime{}, nil)
	if err != nil {
		t.Fatalf("MoySklad ERP order reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ERPOrderReader); !ok {
		t.Fatalf("unexpected ERP order reader type %T", reader)
	}
	if _, err := registry.ERPOrderReader(supportTestAccount(t, "onec"), supportTestRuntime{}, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("1C ERP order reader unexpectedly resolved: %v", err)
	}
}

func TestMagentoReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "magento")
	if !SupportsCapability("magento", "returns.read") {
		t.Fatal("Magento return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"USD"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Magento return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected Magento return reader type %T", reader)
	}
	if _, err := registry.ReturnReader(context.Background(), supportTestAccount(t, "prestashop"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted PrestaShop return reader resolved: %v", err)
	}
}

func TestMedusaReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "medusa")
	if !SupportsCapability("medusa", "returns.read") {
		t.Fatal("Medusa return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"USD"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Medusa return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected Medusa return reader type %T", reader)
	}
}

func TestShopifyReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "shopify")
	if !SupportsCapability("shopify", "returns.read") {
		t.Fatal("Shopify return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"shop_domain":"demo.myshopify.com","store_currency":"USD"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Shopify return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected Shopify return reader type %T", reader)
	}
}

func TestShopwareReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "shopware")
	if !SupportsCapability("shopware", "returns.read") {
		t.Fatal("Shopware return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"USD"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Shopware return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected Shopware return reader type %T", reader)
	}
}

func TestSaleorReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "saleor")
	if !SupportsCapability("saleor", "returns.read") {
		t.Fatal("Saleor return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","channel":"default-channel","warehouse":"default"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Saleor return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected Saleor return reader type %T", reader)
	}
}

func TestWooCommerceReturnReaderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "woocommerce")
	if !SupportsCapability("woocommerce", "returns.read") {
		t.Fatal("WooCommerce return capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"USD"}`), nil
	}
	reader, err := registry.ReturnReader(context.Background(), account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("WooCommerce return reader unavailable: %v", err)
	}
	if _, ok := reader.(sdk.ReturnReader); !ok {
		t.Fatalf("unexpected WooCommerce return reader type %T", reader)
	}
}

func TestWooCommerceWebhookReceiverAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "woocommerce")
	if !SupportsCapability("woocommerce", "notifications.receive") {
		t.Fatal("WooCommerce webhook capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"USD"}`), nil
	}
	receiver, err := registry.CommerceWebhookReceiver(account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("WooCommerce webhook receiver unavailable: %v", err)
	}
	if _, ok := receiver.(sdk.CommerceWebhookReceiver); !ok {
		t.Fatalf("unexpected WooCommerce webhook receiver type %T", receiver)
	}
	if _, err := registry.CommerceWebhookReceiver(supportTestAccount(t, "shopify"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted Shopify webhook receiver resolved: %v", err)
	}
}

func TestSaleorWebhookReceiverAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "saleor")
	if !SupportsCapability("saleor", "notifications.receive") {
		t.Fatal("Saleor webhook capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","channel":"default-channel","warehouse":"main-warehouse"}`), nil
	}
	receiver, err := registry.CommerceWebhookReceiver(account, supportTestRuntime{}, load)
	if err != nil {
		t.Fatalf("Saleor webhook receiver unavailable: %v", err)
	}
	if _, ok := receiver.(sdk.CommerceWebhookReceiver); !ok {
		t.Fatalf("unexpected Saleor webhook receiver type %T", receiver)
	}
	if _, err := registry.CommerceWebhookReceiver(supportTestAccount(t, "shopify"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted Shopify webhook receiver resolved: %v", err)
	}
}

func TestYandexNotificationDecoderAdmissionIsExact(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "yandex-market")
	if !SupportsCapability("yandex-market", "notifications.receive") {
		t.Fatal("Yandex Market notification capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"business_id":1001,"campaign_id":2002,"inventory_mode":"DBS","price_mode":"API"}`), nil
	}
	decoder, err := registry.MarketplaceNotificationDecoder(account, load)
	if err != nil {
		t.Fatalf("Yandex Market notification decoder unavailable: %v", err)
	}
	if _, ok := decoder.(sdk.MarketplaceNotificationDecoder); !ok {
		t.Fatalf("unexpected Yandex Market notification decoder type %T", decoder)
	}
	if _, err := registry.MarketplaceNotificationDecoder(supportTestAccount(t, "wildberries"), load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unadmitted Wildberries notification decoder resolved: %v", err)
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

func TestStorefrontPriceWriterAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	for _, connectorID := range []string{"bitrix", "cs-cart", "magento", "medusa", "opencart", "prestashop", "saleor", "shopify", "shopware", "woocommerce", "yandex-market"} {
		account := supportTestAccount(t, connectorID)
		if !registry.SupportsPriceWrite(account) || !SupportsCapability(connectorID, "prices.write") || !SupportsSync(connectorID, "prices", "outbound") {
			t.Fatalf("%s price write support is not admitted", connectorID)
		}
		if _, err := registry.PriceWriter(account, supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s price writer unavailable: %v", connectorID, err)
		}
	}
}

func TestStorefrontInventoryWriterAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	for _, connectorID := range []string{"bitrix", "cs-cart", "magento", "medusa", "opencart", "prestashop", "saleor", "shopify", "shopware", "woocommerce", "yandex-market"} {
		account := supportTestAccount(t, connectorID)
		if !registry.SupportsInventoryWrite(account) || !SupportsCapability(connectorID, "inventory.write") || !SupportsSync(connectorID, "inventory", "outbound") {
			t.Fatalf("%s inventory write support is not admitted", connectorID)
		}
		if _, err := registry.InventoryWriter(account, supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s inventory writer unavailable: %v", connectorID, err)
		}
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

func TestBitrixOrderRuntimeAdmissionRequiresExplicitStatusMap(t *testing.T) {
	registry := New()
	account := supportTestAccount(t, "bitrix")
	if !registry.SupportsOrderStatusWrite(account) {
		t.Fatal("1C-Bitrix order status writer capability is not admitted")
	}
	load := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","base_path":"","catalog_iblock_id":23,"store_currency":"RUB","price_type_id":1,"order_statuses":{"pending":"N","confirmed":"P","processing":"T","fulfilled":"F","cancelled":"C"}}`), nil
	}
	if _, err := registry.OrderReader(context.Background(), account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("1C-Bitrix order reader unavailable: %v", err)
	}
	if _, err := registry.OrderStatusWriter(context.Background(), account, supportTestRuntime{}, load); err != nil {
		t.Fatalf("1C-Bitrix order status writer unavailable: %v", err)
	}
	if got, ok := registry.OrderStatus(context.Background(), account, "processing", load); !ok || got != "T" {
		t.Fatalf("processing status = %q, %v; want T, true", got, ok)
	}
	missing := func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"store_host":"shop.example.com","base_path":"","catalog_iblock_id":23,"store_currency":"RUB","price_type_id":1}`), nil
	}
	if _, err := registry.OrderReader(context.Background(), account, supportTestRuntime{}, missing); !errors.Is(err, ErrConfigurationNeeded) {
		t.Fatalf("missing Bitrix order status map returned %v", err)
	}
}

func TestStorefrontOrderStatusWriterAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	cases := map[string]map[string]string{
		"cs-cart":     {"pending": "O", "confirmed": "Y", "processing": "P", "fulfilled": "C", "cancelled": "I"},
		"magento":     {"cancelled": "canceled"},
		"medusa":      {"cancelled": "canceled"},
		"opencart":    {"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"prestashop":  {"pending": "1", "confirmed": "2", "processing": "3", "fulfilled": "4", "cancelled": "5"},
		"saleor":      {"cancelled": "CANCELED"},
		"shopify":     {"cancelled": "cancelled"},
		"shopware":    {"cancelled": "cancelled"},
		"woocommerce": {"pending": "pending", "confirmed": "on-hold", "processing": "processing", "fulfilled": "completed", "cancelled": "cancelled"},
	}
	for connectorID, statuses := range cases {
		account := supportTestAccount(t, connectorID)
		currentLoad := load
		if connectorID == "prestashop" || connectorID == "opencart" {
			currentLoad = func(context.Context, string) (json.RawMessage, error) {
				if connectorID == "prestashop" {
					return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"EUR","language_id":1,"order_statuses":{"pending":"1","confirmed":"2","processing":"3","fulfilled":"4","cancelled":"5"}}`), nil
				}
				return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"EUR","order_statuses":{"pending":"1","confirmed":"2","processing":"3","fulfilled":"4","cancelled":"5"}}`), nil
			}
		}
		if !registry.SupportsOrderStatusWrite(account) || !SupportsCapability(connectorID, "orders.status.write") || !SupportsSync(connectorID, "orders", "outbound") {
			t.Fatalf("%s order status write support is not admitted", connectorID)
		}
		if _, err := registry.OrderStatusWriter(context.Background(), account, supportTestRuntime{}, currentLoad); err != nil {
			t.Fatalf("%s order status writer unavailable: %v", connectorID, err)
		}
		for canonical, expected := range statuses {
			if got, ok := registry.OrderStatus(context.Background(), account, canonical, currentLoad); !ok || got != expected {
				t.Fatalf("%s status %q = %q, %v; want %q, true", connectorID, canonical, got, ok, expected)
			}
		}
		if _, ok := registry.OrderStatus(context.Background(), account, "confirmed", currentLoad); connectorID != "cs-cart" && connectorID != "woocommerce" && connectorID != "prestashop" && connectorID != "opencart" && ok {
			t.Fatalf("%s exposed an unsupported non-cancel status transition", connectorID)
		}
	}
}

func TestCommerceReadAdaptersAreAdmittedOnlyWhenComposed(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	for _, connectorID := range []string{"bitrix", "cs-cart", "magnit-market", "medusa", "magento", "opencart", "prestashop", "saleor", "shopify", "shopware", "woocommerce", "yandex-market"} {
		account := supportTestAccount(t, connectorID)
		if !SupportsCapability(connectorID, "prices.read") {
			t.Fatalf("%s price reader capability is not admitted", connectorID)
		}
		if _, err := registry.PriceReader(account, supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s price reader unavailable: %v", connectorID, err)
		}
	}
	for _, connectorID := range []string{"bitrix", "cs-cart", "wildberries", "ozon", "magnit-market", "megamarket", "medusa", "magento", "opencart", "prestashop", "saleor", "shopify", "shopware", "woocommerce", "yandex-market"} {
		account := supportTestAccount(t, connectorID)
		if !SupportsCapability(connectorID, "inventory.read") {
			t.Fatalf("%s inventory reader capability is not admitted", connectorID)
		}
		if _, err := registry.InventoryReader(account, supportTestRuntime{}, load); err != nil {
			t.Fatalf("%s inventory reader unavailable: %v", connectorID, err)
		}
	}
	for _, connectorID := range []string{"cs-cart", "magnit-market", "megamarket", "medusa", "magento", "opencart", "prestashop", "saleor", "shopify", "shopware", "woocommerce", "yandex-market"} {
		account := supportTestAccount(t, connectorID)
		currentLoad := load
		if connectorID == "prestashop" || connectorID == "opencart" {
			currentLoad = func(context.Context, string) (json.RawMessage, error) {
				if connectorID == "prestashop" {
					return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"EUR","language_id":1,"order_statuses":{"pending":"1","confirmed":"2","processing":"3","fulfilled":"4","cancelled":"5"}}`), nil
				}
				return json.RawMessage(`{"store_host":"shop.example.com","store_currency":"EUR","order_statuses":{"pending":"1","confirmed":"2","processing":"3","fulfilled":"4","cancelled":"5"}}`), nil
			}
		}
		if !SupportsCapability(connectorID, "orders.read") || !SupportsSync(connectorID, "orders", "inbound") {
			t.Fatalf("%s order reader capability is not admitted", connectorID)
		}
		if _, err := registry.OrderReader(context.Background(), account, supportTestRuntime{}, currentLoad); err != nil {
			t.Fatalf("%s order reader unavailable: %v", connectorID, err)
		}
	}
	if _, err := registry.PriceReader(supportTestAccount(t, "wildberries"), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Wildberries price reader unexpectedly resolved: %v", err)
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

func TestLogisticsCreatorAdmissionIsExact(t *testing.T) {
	registry := New()
	load := func(context.Context, string) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	for _, connectorID := range []string{"cdek", "dellin", "pek", "pochta-russia"} {
		creator, err := registry.LogisticsCreator(context.Background(), supportTestAccount(t, connectorID), supportTestRuntime{}, load)
		if err != nil || creator == nil {
			t.Fatalf("%s shipment creator unavailable: creator=%T err=%v", connectorID, creator, err)
		}
	}
	for _, connectorID := range []string{"fivepost", "ozon-delivery"} {
		if _, err := registry.LogisticsCreator(context.Background(), supportTestAccount(t, connectorID), supportTestRuntime{}, load); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s unqualified shipment creator resolved: %v", connectorID, err)
		}
	}
}

func TestLogisticsCancelerAdmissionIsExact(t *testing.T) {
	registry := New()
	for _, connectorID := range []string{"cdek", "dellin", "pek", "pochta-russia"} {
		canceler, err := registry.LogisticsCanceler(context.Background(), supportTestAccount(t, connectorID), supportTestRuntime{})
		if err != nil || canceler == nil {
			t.Fatalf("%s shipment canceler unavailable: canceler=%T err=%v", connectorID, canceler, err)
		}
	}
	for _, connectorID := range []string{"fivepost", "ozon-delivery"} {
		if _, err := registry.LogisticsCanceler(context.Background(), supportTestAccount(t, connectorID), supportTestRuntime{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s unqualified shipment canceler resolved: %v", connectorID, err)
		}
	}
}

func TestLogisticsReturnCreatorAdmissionIsExact(t *testing.T) {
	registry := New()
	cdekCreator, err := registry.LogisticsReturnCreator(context.Background(), supportTestAccount(t, "cdek"), supportTestRuntime{})
	if err != nil || cdekCreator == nil {
		t.Fatalf("CDEK return creator unavailable: creator=%T err=%v", cdekCreator, err)
	}
	creator, err := registry.LogisticsReturnCreator(context.Background(), supportTestAccount(t, "pochta-russia"), supportTestRuntime{})
	if err != nil || creator == nil {
		t.Fatalf("Russian Post return creator unavailable: creator=%T err=%v", creator, err)
	}
	for _, connectorID := range []string{"pek", "dellin", "fivepost", "ozon-delivery"} {
		if _, err := registry.LogisticsReturnCreator(context.Background(), supportTestAccount(t, connectorID), supportTestRuntime{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s unqualified return creator resolved: %v", connectorID, err)
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
