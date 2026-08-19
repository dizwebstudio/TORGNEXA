package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/orders"
	"github.com/torgnexa/torgnexa/internal/core/pricing"
)

const (
	auditOrgID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	auditWSID      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	auditProductID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101"
	auditOfferID   = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"
)

var task076bFloatTypePattern = regexp.MustCompile(`(?m)\bfloat(?:32|64)\b`)
var task076bSQLFloatPattern = regexp.MustCompile(`(?i)\b(?:real|float4|float8|double\s+precision)\b`)
var task076bNaiveTimestampPattern = regexp.MustCompile(`(?im)\btimestamp\s+(?:not\s+null|null|default|,|\))`)

func TestTask076bCrossLocaleCurrencyTimezoneAndTax(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	product := catalog.Product{
		ID: catalog.ProductID(auditProductID), OrganizationID: auditOrgID, WorkspaceID: auditWSID,
		Code: "GLOBAL-001", Title: "Кофе 日本語 Café", Description: "Описание — 商品説明",
		Status: catalog.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := product.Validate(); err != nil {
		t.Fatalf("locale-neutral Unicode catalog text rejected: %v", err)
	}
	product.CreatedAt = time.Date(2026, 8, 9, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	if err := product.Validate(); err == nil {
		t.Fatal("catalog accepted persisted non-UTC timestamp")
	}

	for _, code := range []string{"RUB", "EUR", "JPY", "BHD"} {
		currency, err := pricing.NewCurrency(code)
		if err != nil {
			t.Fatalf("pricing currency %s rejected: %v", code, err)
		}
		if _, err := pricing.NewMoney(12345, currency); err != nil {
			t.Fatalf("pricing money %s rejected: %v", code, err)
		}
	}

	for _, tc := range []struct{ value, unit string }{
		{"0.125", "KG"}, {"1", "EA"}, {"2.500000001", "L"},
	} {
		decimal, err := inventory.ParseDecimal(tc.value)
		if err != nil {
			t.Fatalf("inventory decimal %s rejected: %v", tc.value, err)
		}
		unit, err := inventory.NewUnitCode(tc.unit)
		if err != nil {
			t.Fatalf("inventory unit %s rejected: %v", tc.unit, err)
		}
		quantity, err := inventory.NewQuantity(decimal, unit)
		if err != nil || quantity.Value.String() != tc.value || quantity.Unit.String() != tc.unit {
			t.Fatalf("inventory exact quantity drift for %s %s: %#v err=%v", tc.value, tc.unit, quantity, err)
		}
	}
	local := time.Date(2026, 8, 9, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	warehouse := inventory.Warehouse{
		ID:             inventory.WarehouseID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302"),
		OrganizationID: auditOrgID, WorkspaceID: auditWSID, Code: "AMS-01", Name: "Amsterdam",
		Status: inventory.WarehouseActive, Version: 1, CreatedAt: local, UpdatedAt: local,
	}
	if err := warehouse.Validate(); err == nil {
		t.Fatal("inventory accepted persisted local time")
	}
	warehouse.CreatedAt, warehouse.UpdatedAt = local.UTC(), local.UTC()
	if err := warehouse.Validate(); err != nil {
		t.Fatalf("inventory rejected canonical UTC time: %v", err)
	}

	eur, err := orders.NewCurrency("EUR")
	if err != nil {
		t.Fatal(err)
	}
	qtyDecimal, _ := orders.ParseDecimal("2.5")
	pcs, _ := orders.NewUnitCode("PCS")
	qty, _ := orders.NewQuantity(qtyDecimal, pcs)
	unitPrice, _ := orders.NewMoney(4000, eur)
	subtotal, _ := orders.NewMoney(10000, eur)
	discount, _ := orders.NewMoney(1000, eur)
	taxTotal, _ := orders.NewMoney(1800, eur)
	lineTotal, _ := orders.NewMoney(10800, eur)
	shipping, _ := orders.NewMoney(500, eur)
	taxRate, _ := orders.ParseDecimal("0.19")
	zone, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	placedLocal := time.Date(2026, 8, 9, 11, 0, 0, 0, zone)
	cmd := orders.CreateOrder{
		ID: orders.OrderID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601"), Number: "ORD-2026-EU-1", Currency: eur,
		ShippingTotal: shipping, PlacedAt: placedLocal.UTC(),
		Items: []orders.CreateItem{{
			ID: orders.OrderItemID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0602"), OfferID: orders.OfferID(auditOfferID),
			Position: 1, SKU: "GLOBAL-001-EU", Quantity: qty, UnitPrice: unitPrice, Subtotal: subtotal,
			DiscountTotal: discount, TaxTotal: taxTotal, LineTotal: lineTotal,
			Tax: orders.TaxSnapshot{Jurisdiction: "DE", Category: "standard", Rate: taxRate, PriceIncludesTax: false},
		}},
	}
	scope, _ := orders.ParseScope(auditOrgID, auditWSID)
	order, err := orders.BuildCreate(cmd, scope, placedLocal.UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("non-RU order rejected: %v", err)
	}
	if order.Currency.String() != "EUR" || order.Items[0].Tax.Jurisdiction != "DE" || order.Items[0].Tax.Rate.String() != "0.19" {
		t.Fatalf("international order snapshot drifted: %#v", order.Items[0].Tax)
	}
	if order.PlacedAt.Location() != time.UTC || order.CreatedAt.Location() != time.UTC {
		t.Fatal("localized time leaked into persisted order model")
	}
	zero, _ := orders.ParseDecimal("0")
	if err := (orders.TaxSnapshot{Jurisdiction: "AE", Category: "zero", Rate: zero}).Validate(); err != nil {
		t.Fatalf("provider-neutral zero-tax treatment rejected: %v", err)
	}
}

func TestTask076bCommerceSourceAndAdapterParity(t *testing.T) {
	root := task076bRepositoryRoot(t)
	for _, relative := range []string{
		"internal/core/catalog/catalog.go",
		"internal/core/pricing/pricing.go",
		"internal/core/inventory/inventory.go",
		"internal/core/orders/orders.go",
	} {
		data := task076bReadFile(t, root, relative)
		if task076bFloatTypePattern.Match(data) {
			t.Errorf("%s contains binary floating-point domain type", relative)
		}
		text := string(data)
		for _, forbidden := range []string{"time.Local", "time.Now()", "time.FixedZone("} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s persists or constructs implicit/local time via %q", relative, forbidden)
			}
		}
	}

	parity := map[string][]string{
		"internal/core/pricing/pricing.go": {
			"type Currency string", "type Money struct", "minorUnits int64", "currency   Currency",
		},
		"internal/core/inventory/inventory.go": {
			"const MaxDecimalScale = 9", "type Decimal struct", "coefficient int64", "scale       uint8", "type Quantity struct", "Value Decimal", "Unit  UnitCode",
		},
		"internal/core/orders/orders.go": {
			"const MaxDecimalScale = 9", "type Currency string", "type Money struct", "minorUnits int64", "type Decimal struct", "coefficient int64", "scale       uint8", "type Quantity struct", "Value Decimal", "Unit  UnitCode",
		},
	}
	for relative, required := range parity {
		text := string(task076bReadFile(t, root, relative))
		for _, token := range required {
			if !strings.Contains(text, token) {
				t.Errorf("%s no longer mirrors Task-076 primitive token %q", relative, token)
			}
		}
	}
	for _, relative := range []string{
		"internal/platform/postgres/pricingrepo/repository.go",
		"internal/platform/postgres/inventoryrepo/repository.go",
		"internal/platform/postgres/ordersrepo/repository.go",
	} {
		text := string(task076bReadFile(t, root, relative))
		if !strings.Contains(text, `github.com/torgnexa/torgnexa/internal/platform/domain`) {
			t.Errorf("%s no longer maps Core values to shared Task-076 wire primitives", relative)
		}
	}

	providerTokens := []string{"wildberries", "ozon", "yandex_market", "marketplace_id", "provider_status"}
	for _, relative := range []string{
		"internal/core/catalog/catalog.go", "internal/core/pricing/pricing.go", "internal/core/inventory/inventory.go", "internal/core/orders/orders.go",
	} {
		lower := strings.ToLower(string(task076bReadFile(t, root, relative)))
		for _, token := range providerTokens {
			if strings.Contains(lower, token) {
				t.Errorf("provider-local token %q leaked into %s", token, relative)
			}
		}
	}
}

func TestTask076bMigrationsUseExactStorageAndUTC(t *testing.T) {
	root := task076bRepositoryRoot(t)
	for _, relative := range []string{
		"migrations_legacy_pre_v1/000009_catalog_domain.sql",
		"migrations_legacy_pre_v1/000010_price_inventory.sql",
		"migrations_legacy_pre_v1/000011_orders.sql",
	} {
		data := task076bReadFile(t, root, relative)
		lower := strings.ToLower(string(data))
		if task076bSQLFloatPattern.Match(data) {
			t.Errorf("%s contains binary floating-point SQL storage", relative)
		}
		if strings.Contains(lower, "timestamp without time zone") || task076bNaiveTimestampPattern.MatchString(lower) {
			t.Errorf("%s contains local/naive timestamp storage", relative)
		}
		if !strings.Contains(lower, "timestamptz") {
			t.Errorf("%s does not demonstrate UTC-capable timestamptz persistence", relative)
		}
	}

	priceInventory := strings.ToLower(string(task076bReadFile(t, root, "migrations_legacy_pre_v1/000010_price_inventory.sql")))
	for _, required := range []string{"minor_units bigint", "on_hand_coefficient bigint", "on_hand_scale smallint", "reserved_coefficient bigint", "reserved_scale smallint"} {
		if !strings.Contains(priceInventory, required) {
			t.Errorf("000010 exact storage contract missing %q", required)
		}
	}
	orderSQL := strings.ToLower(string(task076bReadFile(t, root, "migrations_legacy_pre_v1/000011_orders.sql")))
	for _, required := range []string{"subtotal_minor_units bigint", "quantity_coefficient bigint", "quantity_scale smallint", "tax_rate_coefficient bigint", "tax_rate_scale smallint"} {
		if !strings.Contains(orderSQL, required) {
			t.Errorf("000011 exact storage contract missing %q", required)
		}
	}
}

func TestTask076bContractsUseCanonicalExactWireShapesAndUTC(t *testing.T) {
	root := task076bRepositoryRoot(t)
	files := []string{
		"contracts/catalog/product.schema.json", "contracts/catalog/offer.schema.json",
		"contracts/pricing/price.schema.json", "contracts/inventory/warehouse.schema.json",
		"contracts/inventory/inventory-position.schema.json", "contracts/orders/order.schema.json",
		"contracts/orders/order-item.schema.json", "contracts/events/product-changed-v1.schema.json",
		"contracts/events/offer-changed-v1.schema.json", "contracts/events/price-changed-v1.schema.json",
		"contracts/events/inventory-position-changed-v1.schema.json", "contracts/events/warehouse-changed-v1.schema.json",
		"contracts/events/order-changed-v1.schema.json",
	}
	for _, relative := range files {
		data := task076bReadFile(t, root, relative)
		var schema any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		if err := task076bAuditSchemaNode(schema, relative); err != nil {
			t.Error(err)
		}
	}
}

func task076bAuditSchemaNode(node any, path string) error {
	switch value := node.(type) {
	case map[string]any:
		if typ, _ := value["type"].(string); typ == "number" {
			return fmt.Errorf("%s contains JSON number where exact commerce values must avoid binary-float semantics", path)
		}
		if format, _ := value["format"].(string); format == "date-time" {
			pattern, _ := value["pattern"].(string)
			if !strings.HasSuffix(pattern, "Z$") {
				return fmt.Errorf("%s contains date-time schema without canonical UTC Z pattern", path)
			}
		}
		if properties, ok := value["properties"].(map[string]any); ok {
			if minor, hasMinor := properties["minor_units"].(map[string]any); hasMinor {
				if minor["type"] != "integer" {
					return fmt.Errorf("%s money minor_units is not integer", path)
				}
				currency, ok := properties["currency"].(map[string]any)
				if !ok || currency["type"] != "string" || currency["pattern"] != "^[A-Z]{3}$" {
					return fmt.Errorf("%s money currency is not canonical three-letter code", path)
				}
			}
			if quantityValue, hasValue := properties["value"].(map[string]any); hasValue {
				if _, hasUnit := properties["unit"]; hasUnit && quantityValue["type"] != "string" {
					return fmt.Errorf("%s quantity value is not an exact decimal string", path)
				}
			}
		}
		for _, child := range value {
			if err := task076bAuditSchemaNode(child, path); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := task076bAuditSchemaNode(child, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func task076bRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve Task-076b audit source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func task076bReadFile(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}
