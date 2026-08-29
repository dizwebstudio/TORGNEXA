// Package builtinruntime is the single reviewed composition boundary for
// statically linked, first-party connector implementations. App/Core stay
// provider-neutral and depend only on this bounded registry surface.
package builtinruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	claude "github.com/torgnexa/torgnexa/connectors/ai/claude"
	deepseek "github.com/torgnexa/torgnexa/connectors/ai/deepseek"
	gemini "github.com/torgnexa/torgnexa/connectors/ai/gemini"
	gigachat "github.com/torgnexa/torgnexa/connectors/ai/gigachat"
	grok "github.com/torgnexa/torgnexa/connectors/ai/grok"
	kimi "github.com/torgnexa/torgnexa/connectors/ai/kimi"
	lmstudio "github.com/torgnexa/torgnexa/connectors/ai/lm-studio"
	ollama "github.com/torgnexa/torgnexa/connectors/ai/ollama"
	openwebui "github.com/torgnexa/torgnexa/connectors/ai/open-webui"
	openaicompatible "github.com/torgnexa/torgnexa/connectors/ai/openai-compatible"
	qwen "github.com/torgnexa/torgnexa/connectors/ai/qwen"
	yandexgpt "github.com/torgnexa/torgnexa/connectors/ai/yandexgpt"
	bitrix24 "github.com/torgnexa/torgnexa/connectors/crm/bitrix24"
	moysklad "github.com/torgnexa/torgnexa/connectors/erp/moysklad"
	onec "github.com/torgnexa/torgnexa/connectors/erp/onec"
	cbrfx "github.com/torgnexa/torgnexa/connectors/finance/cbr-fx"
	cdek "github.com/torgnexa/torgnexa/connectors/logistics/cdek"
	dellin "github.com/torgnexa/torgnexa/connectors/logistics/dellin"
	fivepost "github.com/torgnexa/torgnexa/connectors/logistics/fivepost"
	ozondelivery "github.com/torgnexa/torgnexa/connectors/logistics/ozon-delivery"
	pek "github.com/torgnexa/torgnexa/connectors/logistics/pek"
	pochtarussia "github.com/torgnexa/torgnexa/connectors/logistics/pochta-russia"
	aliexpressru "github.com/torgnexa/torgnexa/connectors/marketplaces/aliexpress-ru"
	magnitmarket "github.com/torgnexa/torgnexa/connectors/marketplaces/magnit-market"
	megamarket "github.com/torgnexa/torgnexa/connectors/marketplaces/megamarket"
	ozon "github.com/torgnexa/torgnexa/connectors/marketplaces/ozon"
	wildberries "github.com/torgnexa/torgnexa/connectors/marketplaces/wildberries"
	yandexmarket "github.com/torgnexa/torgnexa/connectors/marketplaces/yandex-market"
	dolyami "github.com/torgnexa/torgnexa/connectors/payments/dolyami"
	ozonpay "github.com/torgnexa/torgnexa/connectors/payments/ozon-pay"
	robokassa "github.com/torgnexa/torgnexa/connectors/payments/robokassa"
	sbp "github.com/torgnexa/torgnexa/connectors/payments/sbp"
	yookassa "github.com/torgnexa/torgnexa/connectors/payments/yookassa"
	maxmessenger "github.com/torgnexa/torgnexa/connectors/social/max-messenger"
	telegram "github.com/torgnexa/torgnexa/connectors/social/telegram"
	bitrixstore "github.com/torgnexa/torgnexa/connectors/storefronts/bitrix"
	cscart "github.com/torgnexa/torgnexa/connectors/storefronts/cs-cart"
	magento "github.com/torgnexa/torgnexa/connectors/storefronts/magento"
	medusa "github.com/torgnexa/torgnexa/connectors/storefronts/medusa"
	opencart "github.com/torgnexa/torgnexa/connectors/storefronts/opencart"
	prestashop "github.com/torgnexa/torgnexa/connectors/storefronts/prestashop"
	saleor "github.com/torgnexa/torgnexa/connectors/storefronts/saleor"
	shopify "github.com/torgnexa/torgnexa/connectors/storefronts/shopify"
	shopware "github.com/torgnexa/torgnexa/connectors/storefronts/shopware"
	woocommerce "github.com/torgnexa/torgnexa/connectors/storefronts/woocommerce"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/fx"
)

var (
	ErrUnavailable         = errors.New("built-in runtime: connector unavailable")
	ErrConfigurationNeeded = errors.New("built-in runtime: runtime configuration required")
)

// ConfigLoader returns host-owned non-secret JSON for one connector account.
// Credentials never cross this surface; providers resolve them only through
// sdk.Runtime callback-scoped secret access.
type ConfigLoader func(context.Context, string) (json.RawMessage, error)

type Product struct {
	ID         string
	Code       string
	Title      string
	Status     string
	Revision   string
	ObservedAt time.Time
}

type ProductPage struct {
	Items      []Product
	NextCursor string
}

type ProductReader interface {
	Read(context.Context, sdk.PageRequest) (ProductPage, error)
}

// CRMReader is the provider-neutral CRM read surface admitted by the
// production registry. It intentionally exposes only SDK contracts, not the
// Bitrix24 implementation or transport.
type CRMReader interface {
	sdk.CRMEntityReader
	sdk.CRMProductRowReader
}

// CRMWriter is the provider-neutral CRM write surface admitted by the
// production registry. Remote writes remain capability-gated and require the
// connector SDK idempotency key.
type CRMWriter interface {
	sdk.CRMEntityWriter
	sdk.CRMProductRowWriter
}

type Registry struct {
	http    *httpTransport
	localAI *localAIHTTP
	cbr     *cbrfx.Connector

	// gigachat is held across calls (unlike the other AI connectors, which
	// are stateless and constructed per call) because it caches the OAuth
	// access token exchanged per account; a per-call instance would re-do
	// that OAuth round trip on every completion.
	gigachat *gigachat.Connector
}

func New() *Registry {
	transport := newHTTPTransport()
	return &Registry{
		http:     transport,
		localAI:  newLocalAIHTTP(),
		cbr:      cbrfx.New(newCBRDailyHTTP(transport), nil),
		gigachat: gigachat.New(gigaChatHTTP{transport}, nil),
	}
}

// FXRateReader resolves an admitted reference-rate provider without exposing
// its concrete transport outside the reviewed built-in composition boundary.
func (r *Registry) FXRateReader(connectorID string) (sdk.FXRateReader, error) {
	if r == nil || r.http == nil || r.cbr == nil || connectorID != "cbr-fx" {
		return nil, ErrUnavailable
	}
	return r.cbr, nil
}

// FXReferenceSources returns the reviewed global reference providers as
// provider-neutral FX ports. Provider IDs and synthetic no-secret account
// binding remain confined to this composition package.
func (r *Registry) FXReferenceSources() ([]fx.Provider, error) {
	if r == nil || r.cbr == nil {
		return nil, ErrUnavailable
	}
	source, err := fx.NewSourceID("cbr")
	if err != nil {
		return nil, err
	}
	createdAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	account := sdk.Account{
		ID:             "cbr-reference",
		OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001",
		WorkspaceID:    "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID:    "cbr-fx",
		Family:         sdk.FamilyFX,
		Status:         sdk.AccountActive,
		Version:        1,
		Health:         sdk.Health{Status: sdk.HealthUnknown},
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	provider, err := fx.NewConnectorProvider(source, r.cbr, account, nil)
	if err != nil {
		return nil, err
	}
	return []fx.Provider{provider}, nil
}

func (r *Registry) ProductReader(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (ProductReader, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "aliexpress-ru":
		return marketplaceReader{value: aliexpressru.New(aliexpressHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "wildberries":
		return marketplaceReader{value: wildberries.New(wbHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "ozon":
		return marketplaceReader{value: ozon.New(ozonHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "yandex-market":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: yandexmarket.New(ymHTTP{r.http}, yandexConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "magnit-market":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: magnitmarket.New(magnitHTTP{r.http}, magnitConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "megamarket":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: megamarket.New(megamarketHTTP{r.http}, megamarketConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "onec":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return erpReader{value: onec.New(onecHTTP{r.http}, onecConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "moysklad":
		return erpReader{value: moysklad.New(msHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "woocommerce":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: woocommerce.New(wooHTTP{r.http}, wooConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "shopify":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: shopify.New(shopifyHTTP{r.http}, shopifyConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "medusa":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: medusa.New(medusaHTTP{r.http}, medusaConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "magento":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: magento.New(magentoHTTP{r.http}, magentoConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "saleor":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: saleor.New(saleorHTTP{r.http}, saleorConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "bitrix":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: bitrixstore.New(bitrixStoreHTTP{r.http}, bitrixStoreConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "cs-cart":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: cscart.New(csCartHTTP{r.http}, csCartConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "shopware":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: shopware.New(shopwareHTTP{r.http}, shopwareConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "opencart":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: opencart.New(openCartHTTP{r.http}, openCartConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	case "prestashop":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: prestashop.New(prestaShopHTTP{r.http}, prestaShopConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
	default:
		return nil, ErrUnavailable
	}
}

func (r *Registry) ProductWriter(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (sdk.ProductWriter, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "woocommerce":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return woocommerce.New(wooHTTP{r.http}, wooConfigSource{load: load}, nil), nil
	case "shopify":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return shopify.New(shopifyHTTP{r.http}, shopifyConfigSource{load: load}, nil), nil
	case "medusa":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return medusa.New(medusaHTTP{r.http}, medusaConfigSource{load: load}, nil), nil
	case "bitrix":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return bitrixstore.New(bitrixStoreHTTP{r.http}, bitrixStoreConfigSource{load: load}, nil), nil
	case "cs-cart":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return cscart.New(csCartHTTP{r.http}, csCartConfigSource{load: load}, nil), nil
	case "shopware":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return shopware.New(shopwareHTTP{r.http}, shopwareConfigSource{load: load}, nil), nil
	case "magento":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return magento.New(magentoHTTP{r.http}, magentoConfigSource{load: load}, nil), nil
	case "saleor":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return saleor.New(saleorHTTP{r.http}, saleorConfigSource{load: load}, nil), nil
	case "opencart":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return opencart.New(openCartHTTP{r.http}, openCartConfigSource{load: load}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}

// CRMReader resolves the admitted CRM connector for tenant-scoped reads.
// Bitrix24 is deliberately kept on a separate CRM surface; it is not a
// generic product-sync source.
func (r *Registry) CRMReader(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (CRMReader, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil || account.ConnectorID != "bitrix24" || !SupportsCapability(account.ConnectorID, "crm.entities.read") {
		return nil, ErrUnavailable
	}
	if load == nil {
		return nil, ErrConfigurationNeeded
	}
	return bitrix24.New(bitrix24HTTP{r.http}, bitrix24ConfigSource{load: load}, nil), nil
}

// CRMWriter resolves the admitted CRM connector for tenant-scoped writes.
// The returned adapter performs read-before-write and read-after-write
// reconciliation as required by the Bitrix24 connector contract.
func (r *Registry) CRMWriter(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (CRMWriter, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil || account.ConnectorID != "bitrix24" || !SupportsCapability(account.ConnectorID, "crm.entities.write") {
		return nil, ErrUnavailable
	}
	if load == nil {
		return nil, ErrConfigurationNeeded
	}
	return bitrix24.New(bitrix24HTTP{r.http}, bitrix24ConfigSource{load: load}, nil), nil
}

func (r *Registry) SupportsProductWrite(account sdk.Account) bool {
	if r == nil || account.Validate() != nil {
		return false
	}
	return SupportsCapability(account.ConnectorID, "products.write")
}

// ProductStatus translates the canonical catalog lifecycle into the remote
// vocabulary accepted by the admitted product writer. The SDK deliberately
// keeps ProductWriteRequest.StatusRemoteID provider-native; this translation
// therefore belongs at the provider composition boundary, not in Core or the
// generic commerce event route.
func (r *Registry) ProductStatus(connectorID, status string) (string, bool) {
	if r == nil || !SupportsCapability(connectorID, "products.write") {
		return "", false
	}
	mappings := map[string]map[string]string{
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
	value, ok := mappings[connectorID][status]
	return value, ok
}

// SocialPublisher resolves an admitted social publisher without exposing the
// provider implementation outside this reviewed composition boundary.
func (r *Registry) SocialPublisher(account sdk.Account, load ConfigLoader) (sdk.SocialPublisher, error) {
	if r == nil || r.http == nil || account.Validate() != nil || !SupportsCapability(account.ConnectorID, "social.post.text") {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "telegram":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return telegram.New(telegramHTTP{r.http}, telegramConfigSource{load: load}, nil), nil
	case "max-messenger":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return maxmessenger.New(maxHTTP{r.http}, maxConfigSource{load: load}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}

// PaymentGateway resolves an admitted payment connector implementing every
// payment capability its manifest advertises. Provider-specific
// construction remains confined to this reviewed composition boundary.
type PaymentGateway interface {
	sdk.PaymentCreator
	sdk.PaymentStatusReader
	sdk.PaymentRefunder
	sdk.PaymentReconciler
	sdk.PaymentWebhookVerifier
}

// PaymentGateway resolves an admitted payment connector without exposing the
// provider implementation outside this reviewed composition boundary.
func (r *Registry) PaymentGateway(account sdk.Account, load ConfigLoader) (PaymentGateway, error) {
	if r == nil || r.http == nil || account.Validate() != nil || !SupportsCapability(account.ConnectorID, "payments.create") {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "yookassa":
		return yookassa.New(yookassaHTTP{r.http}, nil), nil
	case "robokassa":
		return robokassa.New(robokassaHTTP{r.http}, nil), nil
	case "sbp":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return sbp.New(newSBPHTTP(r.http), sbpConfigSource{load: load}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}

type sbpConfigSource struct{ load ConfigLoader }

type bitrix24ConfigSource struct{ load ConfigLoader }

func (source bitrix24ConfigSource) Resolve(ctx context.Context, account sdk.Account) (bitrix24.Configuration, error) {
	if source.load == nil {
		return bitrix24.Configuration{}, bitrix24.ErrConfigurationMissing
	}
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return bitrix24.Configuration{}, err
	}
	var value struct {
		PortalHost string `json:"portal_host"`
	}
	if decodeStrict(raw, &value) != nil {
		return bitrix24.Configuration{}, bitrix24.ErrInvalidConfiguration
	}
	configuration := bitrix24.Configuration{PortalHost: value.PortalHost}
	if configuration.Validate() != nil {
		return bitrix24.Configuration{}, bitrix24.ErrInvalidConfiguration
	}
	return configuration, nil
}

func (source sbpConfigSource) Resolve(ctx context.Context, account sdk.Account) (sbp.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return sbp.Configuration{}, err
	}
	var value struct {
		GatewayHost string `json:"gateway_host"`
		MemberID    string `json:"member_id"`
	}
	if decodeStrict(raw, &value) != nil {
		return sbp.Configuration{}, sbp.ErrInvalidConfiguration
	}
	configuration := sbp.Configuration{GatewayHost: value.GatewayHost, MemberID: value.MemberID}
	if configuration.Validate() != nil {
		return sbp.Configuration{}, sbp.ErrInvalidConfiguration
	}
	return configuration, nil
}

// SupportsAccountConfiguration reports whether the generic integration
// surface may create and operate an account through this registry.
func (r *Registry) SupportsAccountConfiguration(connectorID string) bool {
	return r != nil && SupportsAccountConfiguration(connectorID)
}

// SupportsCapability reports whether this registry has an executable route
// for the exact declared connector capability.
func (r *Registry) SupportsCapability(connectorID, capability string) bool {
	return r != nil && SupportsCapability(connectorID, capability)
}

// SupportsSync reports whether this registry supports the exact canonical
// entity and direction for a connector.
func (r *Registry) SupportsSync(connectorID, entityType, direction string) bool {
	return r != nil && SupportsSync(connectorID, entityType, direction)
}

// RuntimeConfigRequired reports whether an admitted connector needs host-owned
// non-secret configuration before it can be enabled.
func (r *Registry) RuntimeConfigRequired(connectorID string) bool {
	value, ok := SupportFor(connectorID)
	return r != nil && ok && value.RuntimeConfigRequired
}

// HealthOnly reports whether an account may be activated after a successful
// health check even though no domain capability is admitted yet. This keeps
// provider credentials testable without manufacturing a synchronization route.
func (r *Registry) HealthOnly(connectorID string) bool {
	return r != nil && HealthOnly(connectorID)
}

// SocialTextLimit reports the exact provider ceiling admitted on the shared
// Social surface. Zero means the text-publish route is unavailable.
func (r *Registry) SocialTextLimit(connectorID string) int {
	if r == nil {
		return 0
	}
	return SocialTextLimit(connectorID)
}

// Health executes the same concrete connector and configuration path used by
// product reconciliation. Generic manifest pings are insufficient evidence
// for configuration-bearing providers.
func (r *Registry) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime, load func(context.Context, string) (json.RawMessage, error)) (sdk.Health, error) {
	if r == nil || r.http == nil || ctx == nil || account.Validate() != nil || runtime == nil {
		return sdk.Health{}, ErrUnavailable
	}
	connector, err := r.healthConnector(account, load)
	if err != nil {
		return sdk.Health{}, err
	}
	return connector.Health(ctx, account, runtime)
}

func (r *Registry) healthConnector(account sdk.Account, load ConfigLoader) (sdk.Connector, error) {
	switch account.ConnectorID {
	case "aliexpress-ru":
		return aliexpressru.New(aliexpressHTTP{r.http}, nil), nil
	case "wildberries":
		return wildberries.New(wbHTTP{r.http}, nil), nil
	case "ozon":
		return ozon.New(ozonHTTP{r.http}, nil), nil
	case "ozon-pay":
		return ozonpay.New(ozonPayHTTP{r.http}, nil), nil
	case "dolyami":
		if load != nil {
			return dolyami.New(dolyamiHTTP{base: r.http}, dolyamiConfigSource{load: load}, nil), nil
		}
	case "ozon-delivery":
		return ozondelivery.New(ozonDeliveryHTTP{r.http}, nil), nil
	case "moysklad":
		return moysklad.New(msHTTP{r.http}, nil), nil
	case "bitrix24":
		if load != nil {
			return bitrix24.New(bitrix24HTTP{r.http}, bitrix24ConfigSource{load: load}, nil), nil
		}
	case "yookassa":
		return yookassa.New(yookassaHTTP{r.http}, nil), nil
	case "robokassa":
		return robokassa.New(robokassaHTTP{r.http}, nil), nil
	case "sbp":
		if load != nil {
			return sbp.New(newSBPHTTP(r.http), sbpConfigSource{load: load}, nil), nil
		}
	case "telegram":
		if load != nil {
			return telegram.New(telegramHTTP{r.http}, telegramConfigSource{load: load}, nil), nil
		}
	case "max-messenger":
		if load != nil {
			return maxmessenger.New(maxHTTP{r.http}, maxConfigSource{load: load}, nil), nil
		}
	case "yandex-market":
		if load != nil {
			return yandexmarket.New(ymHTTP{r.http}, yandexConfigSource{load: load}, nil), nil
		}
	case "magnit-market":
		if load != nil {
			return magnitmarket.New(magnitHTTP{r.http}, magnitConfigSource{load: load}, nil), nil
		}
	case "megamarket":
		if load != nil {
			return megamarket.New(megamarketHTTP{r.http}, megamarketConfigSource{load: load}, nil), nil
		}
	case "onec":
		if load != nil {
			return onec.New(onecHTTP{r.http}, onecConfigSource{load: load}, nil), nil
		}
	case "woocommerce":
		if load != nil {
			return woocommerce.New(wooHTTP{r.http}, wooConfigSource{load: load}, nil), nil
		}
	case "shopify":
		if load != nil {
			return shopify.New(shopifyHTTP{r.http}, shopifyConfigSource{load: load}, nil), nil
		}
	case "medusa":
		if load != nil {
			return medusa.New(medusaHTTP{r.http}, medusaConfigSource{load: load}, nil), nil
		}
	case "shopware":
		if load != nil {
			return shopware.New(shopwareHTTP{r.http}, shopwareConfigSource{load: load}, nil), nil
		}
	case "magento":
		if load != nil {
			return magento.New(magentoHTTP{r.http}, magentoConfigSource{load: load}, nil), nil
		}
	case "saleor":
		if load != nil {
			return saleor.New(saleorHTTP{r.http}, saleorConfigSource{load: load}, nil), nil
		}
	case "bitrix":
		if load != nil {
			return bitrixstore.New(bitrixStoreHTTP{r.http}, bitrixStoreConfigSource{load: load}, nil), nil
		}
	case "cs-cart":
		if load != nil {
			return cscart.New(csCartHTTP{r.http}, csCartConfigSource{load: load}, nil), nil
		}
	case "opencart":
		if load != nil {
			return opencart.New(openCartHTTP{r.http}, openCartConfigSource{load: load}, nil), nil
		}
	case "prestashop":
		if load != nil {
			return prestashop.New(prestaShopHTTP{r.http}, prestaShopConfigSource{load: load}, nil), nil
		}
	case "fivepost":
		return fivepost.New(fivepostHTTP{r.http}, nil), nil
	case "pek":
		return pek.New(pekHTTP{r.http}, nil), nil
	case "cdek":
		return cdek.New(cdekHTTP{r.http}, nil), nil
	case "dellin":
		return dellin.New(dellinHTTP{r.http}, nil), nil
	case "pochta-russia":
		return pochtarussia.New(pochtarussiaHTTP{r.http}, nil), nil
	default:
		if HealthOnly(account.ConnectorID) {
			return catalogProbeConnector{h: r.http, id: account.ConnectorID, config: load}, nil
		}
		return nil, ErrUnavailable
	}
	return nil, ErrConfigurationNeeded
}

type dolyamiConfigSource struct{ load ConfigLoader }

func (source dolyamiConfigSource) Resolve(ctx context.Context, account sdk.Account) (dolyami.Configuration, error) {
	if source.load == nil {
		return dolyami.Configuration{}, dolyami.ErrConfigurationMissing
	}
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return dolyami.Configuration{}, dolyami.ErrConfigurationMissing
	}
	var value struct {
		ProbeURL string `json:"probe_url"`
	}
	if decodeStrict(raw, &value) != nil {
		return dolyami.Configuration{}, dolyami.ErrInvalidConfiguration
	}
	configuration := dolyami.Configuration{ProbeURL: value.ProbeURL}
	if configuration.Validate() != nil {
		return dolyami.Configuration{}, dolyami.ErrInvalidConfiguration
	}
	return configuration, nil
}

// PriceWriter resolves only first-party connectors that explicitly advertise
// the existing prices.write capability. Provider-specific construction remains
// confined to this reviewed composition boundary.
func (r *Registry) PriceWriter(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (sdk.PriceWriter, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "yandex-market":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return yandexmarket.New(ymHTTP{r.http}, yandexConfigSource{load: load}, nil), nil
	case "woocommerce":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return woocommerce.New(wooHTTP{r.http}, wooConfigSource{load: load}, nil), nil
	case "shopify":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return shopify.New(shopifyHTTP{r.http}, shopifyConfigSource{load: load}, nil), nil
	case "medusa":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return medusa.New(medusaHTTP{r.http}, medusaConfigSource{load: load}, nil), nil
	case "shopware":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return shopware.New(shopwareHTTP{r.http}, shopwareConfigSource{load: load}, nil), nil
	case "magento":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return magento.New(magentoHTTP{r.http}, magentoConfigSource{load: load}, nil), nil
	case "saleor":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return saleor.New(saleorHTTP{r.http}, saleorConfigSource{load: load}, nil), nil
	case "prestashop":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return prestashop.New(prestaShopHTTP{r.http}, prestaShopConfigSource{load: load}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}

func (r *Registry) SupportsPriceWrite(account sdk.Account) bool {
	if r == nil || account.Validate() != nil {
		return false
	}
	// Keep the adapter-level admission stable for callers that use this port
	// outside the generic sync route. The generated runtime-support contract
	// separately controls which entities the production worker may route.
	return account.ConnectorID == "yandex-market" || account.ConnectorID == "woocommerce" || account.ConnectorID == "shopify" || account.ConnectorID == "medusa" || account.ConnectorID == "shopware" || account.ConnectorID == "magento" || account.ConnectorID == "saleor" || account.ConnectorID == "prestashop"
}

// InventoryWriter resolves first-party connectors with an executable
// inventory.write route. The support declaration remains the source of truth
// for which connector accounts are admitted to this surface.
func (r *Registry) InventoryWriter(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (sdk.InventoryWriter, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "prestashop":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return prestashop.New(prestaShopHTTP{r.http}, prestaShopConfigSource{load: load}, nil), nil
	default:
		return nil, ErrUnavailable
	}
}

// SupportsInventoryWrite reports whether the built-in runtime has an
// executable inventory.write adapter for the account's connector.
func (r *Registry) SupportsInventoryWrite(account sdk.Account) bool {
	if r == nil || account.Validate() != nil {
		return false
	}
	return SupportsCapability(account.ConnectorID, "inventory.write")
}

type marketplaceReader struct {
	value   sdk.ProductReader
	account sdk.Account
	runtime sdk.Runtime
}

func (r marketplaceReader) Read(ctx context.Context, req sdk.PageRequest) (ProductPage, error) {
	page, err := r.value.ReadProducts(ctx, r.account, r.runtime, req)
	if err != nil {
		return ProductPage{}, err
	}
	out := ProductPage{NextCursor: page.NextCursor, Items: make([]Product, 0, len(page.Items))}
	for _, value := range page.Items {
		out.Items = append(out.Items, Product{
			ID:         value.RemoteID,
			Code:       value.SellerSKU,
			Title:      value.Title,
			Revision:   value.UpdatedAt.UTC().Format(time.RFC3339Nano),
			ObservedAt: value.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

type erpReader struct {
	value   sdk.ERPCatalogReader
	account sdk.Account
	runtime sdk.Runtime
}

func (r erpReader) Read(ctx context.Context, req sdk.PageRequest) (ProductPage, error) {
	page, err := r.value.ReadERPCatalog(ctx, r.account, r.runtime, req)
	if err != nil {
		return ProductPage{}, err
	}
	now := time.Now().UTC()
	out := ProductPage{NextCursor: page.NextCursor, Items: make([]Product, 0, len(page.Items))}
	for _, value := range page.Items {
		code := value.Code
		if code == "" {
			code = value.SKU
		}
		status := "active"
		if value.Archived {
			status = "archived"
		}
		out.Items = append(out.Items, Product{ID: value.RemoteID, Code: code, Title: value.Title, Status: status, Revision: value.Revision, ObservedAt: now})
	}
	return out, nil
}

type yandexConfigSource struct{ load ConfigLoader }

type telegramConfigSource struct{ load ConfigLoader }

type maxConfigSource struct{ load ConfigLoader }

func (source maxConfigSource) Resolve(ctx context.Context, account sdk.Account) (maxmessenger.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return maxmessenger.Configuration{}, err
	}
	var value struct {
		ChatID int64 `json:"chat_id"`
	}
	if decodeStrict(raw, &value) != nil {
		return maxmessenger.Configuration{}, maxmessenger.ErrInvalidConfiguration
	}
	configuration := maxmessenger.Configuration{ChatID: value.ChatID}
	if configuration.Validate() != nil {
		return maxmessenger.Configuration{}, maxmessenger.ErrInvalidConfiguration
	}
	return configuration, nil
}

func (source telegramConfigSource) Resolve(ctx context.Context, account sdk.Account) (telegram.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return telegram.Configuration{}, err
	}
	var value struct {
		ChatID int64 `json:"chat_id"`
	}
	if decodeStrict(raw, &value) != nil {
		return telegram.Configuration{}, telegram.ErrInvalidConfiguration
	}
	configuration := telegram.Configuration{ChatID: value.ChatID}
	if configuration.Validate() != nil {
		return telegram.Configuration{}, telegram.ErrInvalidConfiguration
	}
	return configuration, nil
}

func (source yandexConfigSource) Resolve(ctx context.Context, account sdk.Account) (yandexmarket.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return yandexmarket.Configuration{}, err
	}
	var value struct {
		BusinessID    int64                      `json:"business_id"`
		CampaignID    int64                      `json:"campaign_id"`
		InventoryMode yandexmarket.InventoryMode `json:"inventory_mode"`
		PriceMode     yandexmarket.PriceMode     `json:"price_mode"`
		Warehouses    []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"warehouses"`
	}
	if decodeStrict(raw, &value) != nil {
		return yandexmarket.Configuration{}, yandexmarket.ErrInvalidConfiguration
	}
	configuration := yandexmarket.Configuration{BusinessID: value.BusinessID, CampaignID: value.CampaignID, InventoryMode: value.InventoryMode, PriceMode: value.PriceMode}
	for _, warehouse := range value.Warehouses {
		configuration.Warehouses = append(configuration.Warehouses, yandexmarket.Warehouse{ID: warehouse.ID, Name: warehouse.Name})
	}
	if configuration.Validate() != nil {
		return yandexmarket.Configuration{}, yandexmarket.ErrInvalidConfiguration
	}
	return configuration, nil
}

type magnitConfigSource struct{ load ConfigLoader }

func (source magnitConfigSource) Resolve(ctx context.Context, account sdk.Account) (magnitmarket.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return magnitmarket.Configuration{}, err
	}
	var value struct {
		ShopID          int64                  `json:"shop_id"`
		StockType       magnitmarket.StockType `json:"stock_type"`
		OrderWindowDays int                    `json:"order_window_days"`
	}
	if decodeStrict(raw, &value) != nil {
		return magnitmarket.Configuration{}, magnitmarket.ErrInvalidConfiguration
	}
	configuration := magnitmarket.Configuration{ShopID: value.ShopID, StockType: value.StockType, OrderWindowDays: value.OrderWindowDays}
	if configuration.Validate() != nil {
		return magnitmarket.Configuration{}, magnitmarket.ErrInvalidConfiguration
	}
	return configuration, nil
}

type megamarketConfigSource struct{ load ConfigLoader }

func (source megamarketConfigSource) Resolve(ctx context.Context, account sdk.Account) (megamarket.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return megamarket.Configuration{}, err
	}
	var value struct {
		MerchantID int64             `json:"merchant_id"`
		Scheme     megamarket.Scheme `json:"scheme"`
		Warehouses []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"warehouses"`
	}
	if decodeStrict(raw, &value) != nil {
		return megamarket.Configuration{}, megamarket.ErrInvalidConfiguration
	}
	configuration := megamarket.Configuration{MerchantID: value.MerchantID, Scheme: value.Scheme}
	for _, warehouse := range value.Warehouses {
		configuration.Warehouses = append(configuration.Warehouses, megamarket.Warehouse{ID: warehouse.ID, Name: warehouse.Name})
	}
	if configuration.Validate() != nil {
		return megamarket.Configuration{}, megamarket.ErrInvalidConfiguration
	}
	return configuration, nil
}

type onecConfigSource struct{ load ConfigLoader }

func (source onecConfigSource) Resolve(ctx context.Context, account sdk.Account) (onec.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return onec.Configuration{}, err
	}
	var value struct {
		Host     string `json:"host"`
		BasePath string `json:"base_path"`
		Catalog  struct {
			Resource      string `json:"resource"`
			IDField       string `json:"id_field"`
			CodeField     string `json:"code_field"`
			SKUField      string `json:"sku_field"`
			TitleField    string `json:"title_field"`
			BrandField    string `json:"brand_field"`
			RevisionField string `json:"revision_field"`
			ArchivedField string `json:"archived_field"`
		} `json:"catalog"`
		Inventory struct {
			Resource      string `json:"resource"`
			Function      string `json:"function"`
			ProductField  string `json:"product_field"`
			LocationField string `json:"location_field"`
			QuantityField string `json:"quantity_field"`
		} `json:"inventory"`
	}
	if decodeStrict(raw, &value) != nil {
		return onec.Configuration{}, onec.ErrInvalidConfiguration
	}
	configuration := onec.Configuration{
		Host: value.Host, BasePath: value.BasePath,
		Catalog:   onec.CatalogMapping{Resource: value.Catalog.Resource, IDField: value.Catalog.IDField, CodeField: value.Catalog.CodeField, SKUField: value.Catalog.SKUField, TitleField: value.Catalog.TitleField, BrandField: value.Catalog.BrandField, RevisionField: value.Catalog.RevisionField, ArchivedField: value.Catalog.ArchivedField},
		Inventory: onec.InventoryMapping{Resource: value.Inventory.Resource, Function: value.Inventory.Function, ProductField: value.Inventory.ProductField, LocationField: value.Inventory.LocationField, QuantityField: value.Inventory.QuantityField},
	}
	if configuration.Validate() != nil {
		return onec.Configuration{}, onec.ErrInvalidConfiguration
	}
	return configuration, nil
}

type wooConfigSource struct{ load ConfigLoader }

func (source wooConfigSource) Resolve(ctx context.Context, account sdk.Account) (woocommerce.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return woocommerce.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return woocommerce.Configuration{}, woocommerce.ErrInvalidConfiguration
	}
	configuration := woocommerce.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return woocommerce.Configuration{}, woocommerce.ErrInvalidConfiguration
	}
	return configuration, nil
}

type shopwareConfigSource struct{ load ConfigLoader }

func (source shopwareConfigSource) Resolve(ctx context.Context, account sdk.Account) (shopware.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return shopware.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return shopware.Configuration{}, shopware.ErrInvalidConfiguration
	}
	configuration := shopware.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return shopware.Configuration{}, shopware.ErrInvalidConfiguration
	}
	return configuration, nil
}

type magentoConfigSource struct{ load ConfigLoader }

type bitrixStoreConfigSource struct{ load ConfigLoader }

type csCartConfigSource struct{ load ConfigLoader }

func (source csCartConfigSource) Resolve(ctx context.Context, account sdk.Account) (cscart.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return cscart.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return cscart.Configuration{}, cscart.ErrInvalidConfiguration
	}
	configuration := cscart.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return cscart.Configuration{}, cscart.ErrInvalidConfiguration
	}
	return configuration, nil
}

func (source bitrixStoreConfigSource) Resolve(ctx context.Context, account sdk.Account) (bitrixstore.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return bitrixstore.Configuration{}, err
	}
	var value struct {
		StoreHost       string `json:"store_host"`
		BasePath        string `json:"base_path"`
		CatalogIblockID int64  `json:"catalog_iblock_id"`
		StoreCurrency   string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return bitrixstore.Configuration{}, bitrixstore.ErrInvalidConfiguration
	}
	configuration := bitrixstore.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, CatalogIblockID: value.CatalogIblockID, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return bitrixstore.Configuration{}, bitrixstore.ErrInvalidConfiguration
	}
	return configuration, nil
}

func (source magentoConfigSource) Resolve(ctx context.Context, account sdk.Account) (magento.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return magento.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return magento.Configuration{}, magento.ErrInvalidConfiguration
	}
	configuration := magento.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return magento.Configuration{}, magento.ErrInvalidConfiguration
	}
	return configuration, nil
}

type saleorConfigSource struct{ load ConfigLoader }

func (source saleorConfigSource) Resolve(ctx context.Context, account sdk.Account) (saleor.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return saleor.Configuration{}, err
	}
	var value struct {
		StoreHost string `json:"store_host"`
		BasePath  string `json:"base_path"`
		Channel   string `json:"channel"`
		Warehouse string `json:"warehouse"`
	}
	if decodeStrict(raw, &value) != nil {
		return saleor.Configuration{}, saleor.ErrInvalidConfiguration
	}
	configuration := saleor.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, Channel: value.Channel, Warehouse: value.Warehouse}
	if configuration.Validate() != nil {
		return saleor.Configuration{}, saleor.ErrInvalidConfiguration
	}
	return configuration, nil
}

type medusaConfigSource struct{ load ConfigLoader }

func (source medusaConfigSource) Resolve(ctx context.Context, account sdk.Account) (medusa.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return medusa.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return medusa.Configuration{}, medusa.ErrInvalidConfiguration
	}
	configuration := medusa.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return medusa.Configuration{}, medusa.ErrInvalidConfiguration
	}
	return configuration, nil
}

type shopifyConfigSource struct{ load ConfigLoader }

func (source shopifyConfigSource) Resolve(ctx context.Context, account sdk.Account) (shopify.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return shopify.Configuration{}, err
	}
	var value struct {
		ShopDomain    string `json:"shop_domain"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return shopify.Configuration{}, shopify.ErrInvalidConfiguration
	}
	configuration := shopify.Configuration{ShopDomain: value.ShopDomain, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return shopify.Configuration{}, shopify.ErrInvalidConfiguration
	}
	return configuration, nil
}

type openCartConfigSource struct{ load ConfigLoader }

func (source openCartConfigSource) Resolve(ctx context.Context, account sdk.Account) (opencart.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return opencart.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
	}
	if decodeStrict(raw, &value) != nil {
		return opencart.Configuration{}, opencart.ErrInvalidConfiguration
	}
	configuration := opencart.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency}
	if configuration.Validate() != nil {
		return opencart.Configuration{}, opencart.ErrInvalidConfiguration
	}
	return configuration, nil
}

type prestaShopConfigSource struct{ load ConfigLoader }

func (source prestaShopConfigSource) Resolve(ctx context.Context, account sdk.Account) (prestashop.Configuration, error) {
	raw, err := source.load(ctx, account.ID)
	if err != nil {
		return prestashop.Configuration{}, err
	}
	var value struct {
		StoreHost     string `json:"store_host"`
		BasePath      string `json:"base_path"`
		StoreCurrency string `json:"store_currency"`
		LanguageID    int64  `json:"language_id"`
		ShopID        int64  `json:"shop_id"`
	}
	if decodeStrict(raw, &value) != nil {
		return prestashop.Configuration{}, prestashop.ErrInvalidConfiguration
	}
	configuration := prestashop.Configuration{StoreHost: value.StoreHost, BasePath: value.BasePath, StoreCurrency: value.StoreCurrency, LanguageID: value.LanguageID, ShopID: value.ShopID}
	if configuration.Validate() != nil {
		return prestashop.Configuration{}, prestashop.ErrInvalidConfiguration
	}
	return configuration, nil
}

// AIComplete resolves the registered AI connector for account.ConnectorID
// and sends one bounded completion request through it. host is a bare
// hostname override for cloud providers (openai-compatible, kimi, qwen,
// deepseek and claude); local Ollama, LM Studio and Open WebUI providers
// receive the complete explicitly approved local base URL. folderID is
// required only by yandexgpt. This is the sole point in the
// repository that branches on an AI provider identity, matching the
// exemption architecture/policy.json grants provider_composition_module.
func (r *Registry) AICompletion(ctx context.Context, account sdk.Account, runtime sdk.Runtime, host, folderID, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return "", "", ErrUnavailable
	}
	switch account.ConnectorID {
	case "openai-compatible":
		return openaicompatible.New(openAICompatibleHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "ollama":
		if r.localAI == nil {
			return "", "", ErrUnavailable
		}
		return ollama.New(ollamaHTTP{r.localAI}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "lm-studio":
		if r.localAI == nil {
			return "", "", ErrUnavailable
		}
		return lmstudio.New(lmStudioHTTP{r.localAI}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "open-webui":
		if r.localAI == nil {
			return "", "", ErrUnavailable
		}
		return openwebui.New(openWebUIHTTP{r.localAI}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "kimi":
		return kimi.New(kimiHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "gigachat":
		if r.gigachat == nil {
			return "", "", ErrUnavailable
		}
		return r.gigachat.Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "yandexgpt":
		return yandexgpt.New(yandexGPTHTTP{r.http}, nil).Complete(ctx, account, runtime, folderID, model, systemPrompt, userPrompt)
	case "qwen":
		return qwen.New(qwenHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "deepseek":
		return deepseek.New(deepseekHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "claude":
		return claude.New(claudeHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "gemini":
		return gemini.New(geminiHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "grok":
		return grok.New(grokHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	default:
		return "", "", ErrUnavailable
	}
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("built-in runtime: extra JSON value")
	}
	return nil
}
