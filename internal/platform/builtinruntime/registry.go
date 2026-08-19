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

	gigachat "github.com/torgnexa/torgnexa/connectors/gigachat"
	kimi "github.com/torgnexa/torgnexa/connectors/kimi"
	moysklad "github.com/torgnexa/torgnexa/connectors/moysklad"
	onec "github.com/torgnexa/torgnexa/connectors/onec"
	openaicompatible "github.com/torgnexa/torgnexa/connectors/openai-compatible"
	ozon "github.com/torgnexa/torgnexa/connectors/ozon"
	wildberries "github.com/torgnexa/torgnexa/connectors/wildberries"
	woocommerce "github.com/torgnexa/torgnexa/connectors/woocommerce"
	yandexmarket "github.com/torgnexa/torgnexa/connectors/yandex-market"
	yandexgpt "github.com/torgnexa/torgnexa/connectors/yandexgpt"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
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

type Registry struct {
	http *httpTransport

	// gigachat is held across calls (unlike the other AI connectors, which
	// are stateless and constructed per call) because it caches the OAuth
	// access token exchanged per account; a per-call instance would re-do
	// that OAuth round trip on every completion.
	gigachat *gigachat.Connector
}

func New() *Registry {
	transport := newHTTPTransport()
	return &Registry{http: transport, gigachat: gigachat.New(gigaChatHTTP{transport}, nil)}
}

func (r *Registry) ProductReader(account sdk.Account, runtime sdk.Runtime, load ConfigLoader) (ProductReader, error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return nil, ErrUnavailable
	}
	switch account.ConnectorID {
	case "wildberries":
		return marketplaceReader{value: wildberries.New(wbHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "ozon":
		return marketplaceReader{value: ozon.New(ozonHTTP{r.http}, nil), account: account, runtime: runtime}, nil
	case "yandex-market":
		if load == nil {
			return nil, ErrConfigurationNeeded
		}
		return marketplaceReader{value: yandexmarket.New(ymHTTP{r.http}, yandexConfigSource{load: load}, nil), account: account, runtime: runtime}, nil
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
	default:
		return nil, ErrUnavailable
	}
}

func (r *Registry) SupportsProductWrite(account sdk.Account) bool {
	if r == nil || account.Validate() != nil {
		return false
	}
	return account.ConnectorID == "woocommerce"
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
	default:
		return nil, ErrUnavailable
	}
}

func (r *Registry) SupportsPriceWrite(account sdk.Account) bool {
	if r == nil || account.Validate() != nil {
		return false
	}
	return account.ConnectorID == "yandex-market" || account.ConnectorID == "woocommerce"
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

// AIComplete resolves the registered AI connector for account.ConnectorID
// and sends one bounded completion request through it. host is a bare
// hostname override (only openai-compatible and kimi honor it); folderID is
// required only by yandexgpt. This is the sole point in the repository that
// branches on an AI provider identity, matching the exemption
// architecture/policy.json grants provider_composition_module.
func (r *Registry) AICompletion(ctx context.Context, account sdk.Account, runtime sdk.Runtime, host, folderID, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if r == nil || r.http == nil || account.Validate() != nil || runtime == nil {
		return "", "", ErrUnavailable
	}
	switch account.ConnectorID {
	case "openai-compatible":
		return openaicompatible.New(openAICompatibleHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "kimi":
		return kimi.New(kimiHTTP{r.http}, nil).Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "gigachat":
		if r.gigachat == nil {
			return "", "", ErrUnavailable
		}
		return r.gigachat.Complete(ctx, account, runtime, host, model, systemPrompt, userPrompt)
	case "yandexgpt":
		return yandexgpt.New(yandexGPTHTTP{r.http}, nil).Complete(ctx, account, runtime, folderID, model, systemPrompt, userPrompt)
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
