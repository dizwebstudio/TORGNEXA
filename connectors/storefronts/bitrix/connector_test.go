package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 7, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "bitrix-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "bitrix", Family: sdk.FamilyStorefront, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	return callback([]byte(`{"user_id":"1","webhook_code":"test-webhook-code"}`))
}

type testConfig struct{}

func (testConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{StoreHost: "shop.example.com", CatalogIblockID: 23, StoreCurrency: "RUB", PriceTypeID: 1}, nil
}

type scriptedTransport struct {
	fn func(Request) (Response, error)
}

func (transport scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	return transport.fn(request)
}

func TestManifestAndInterfaces(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []sdk.Capability{"inventory.read", "inventory.write", "orders.read", "orders.status.write", "prices.read", "prices.write", "products.read", "products.write"} {
		if !Manifest().Supports(capability) {
			t.Fatalf("missing %s", capability)
		}
	}
	var connector any = New(scriptedTransport{}, testConfig{}, nil)
	for _, check := range []struct {
		name string
		ok   bool
	}{
		{"ProductReader", func() bool { _, ok := connector.(sdk.ProductReader); return ok }()},
		{"ProductWriter", func() bool { _, ok := connector.(sdk.ProductWriter); return ok }()},
		{"PriceReader", func() bool { _, ok := connector.(sdk.PriceReader); return ok }()},
		{"PriceWriter", func() bool { _, ok := connector.(sdk.PriceWriter); return ok }()},
		{"InventoryReader", func() bool { _, ok := connector.(sdk.InventoryReader); return ok }()},
		{"OrderReader", func() bool { _, ok := connector.(sdk.OrderReader); return ok }()},
		{"OrderStatusWriter", func() bool { _, ok := connector.(sdk.OrderStatusWriter); return ok }()},
	} {
		if !check.ok {
			t.Fatalf("%s missing", check.name)
		}
	}
}

func TestReadOrdersUsesSaleAndBasketAPIs(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "sale.order.list":
			if !strings.Contains(string(request.Body), `"statusId"`) || !strings.Contains(string(request.Body), `"start":0`) {
				t.Fatalf("unexpected order list request: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"orders":[{"id":236,"accountNumber":"392","statusId":"N","dateInsert":"2026-08-28T09:00:00+03:00","dateUpdate":"2026-08-28T09:05:00+03:00"}]},"total":1}`)}, nil
		case "sale.basketitem.list":
			if !strings.Contains(string(request.Body), `"@orderId":[236]`) {
				t.Fatalf("unexpected basket list request: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"basketItems":[{"id":"9001","orderId":"236","productId":348,"quantity":2}]},"total":1}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	page, err := New(transport, testConfig{}, nil).ReadOrders(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "236" || page.Items[0].ExternalID != "392" || page.Items[0].StatusRemoteID != "N" || len(page.Items[0].Items) != 1 || page.Items[0].Items[0].RemoteID != "9001" || page.Items[0].Items[0].VariantRemoteID != "348" || page.Items[0].Items[0].Quantity != 2 {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestReadOrdersOmitsCustomBasketLines(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod == "sale.order.list" {
			return Response{StatusCode: 200, Body: []byte(`{"result":{"orders":[{"id":236,"accountNumber":"392","statusId":"N","dateInsert":"2026-08-28T09:00:00+03:00","dateUpdate":"2026-08-28T09:05:00+03:00"}]},"total":1}`)}, nil
		}
		if request.APIMethod == "sale.basketitem.list" {
			return Response{StatusCode: 200, Body: []byte(`{"result":{"basketItems":[{"id":"9001","orderId":"236","productId":0,"quantity":1}]},"total":1}`)}, nil
		}
		t.Fatalf("unexpected method %s", request.APIMethod)
		return Response{}, nil
	}}
	page, err := New(transport, testConfig{}, nil).ReadOrders(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Items) != 0 {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestWriteOrderStatusUpdatesAndReconciles(t *testing.T) {
	getCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "sale.order.get":
			getCalls++
			status := "N"
			if getCalls > 1 {
				status = "F"
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"order":{"id":236,"statusId":"` + status + `"}}}`)}, nil
		case "sale.order.update":
			if !strings.Contains(string(request.Body), `"statusId":"F"`) {
				t.Fatalf("unexpected update request: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":true}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).WriteOrderStatus(context.Background(), testAccount(), testRuntime{}, sdk.OrderStatusWriteRequest{OrderRemoteID: "236", StatusRemoteID: "F", IdempotencyKey: "order-status-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "236" || receipt.Reconciled || getCalls != 2 {
		t.Fatalf("receipt=%#v getCalls=%d", receipt, getCalls)
	}
}

func TestWriteOrderStatusDuplicateDoesNotMutate(t *testing.T) {
	updateCalled := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod == "sale.order.update" {
			updateCalled = true
			t.Fatalf("unexpected update request: %s", request.Body)
		}
		if request.APIMethod != "sale.order.get" {
			t.Fatalf("unexpected method %s", request.APIMethod)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"order":{"id":236,"statusId":"F"}}}`)}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).WriteOrderStatus(context.Background(), testAccount(), testRuntime{}, sdk.OrderStatusWriteRequest{OrderRemoteID: "236", StatusRemoteID: "F", IdempotencyKey: "order-status-duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Duplicate || !receipt.Reconciled || updateCalled {
		t.Fatalf("receipt=%#v updateCalled=%v", receipt, updateCalled)
	}
}

func TestListInventoryLocationsUsesActiveStores(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod != "catalog.store.list" || !strings.Contains(string(request.Body), `"active":"Y"`) {
			t.Fatalf("unexpected request %#v", request)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"stores":[{"id":1,"title":"Основной склад","active":"Y"}]},"total":1}`)}, nil
	}}
	locations, err := New(transport, testConfig{}, nil).ListInventoryLocations(context.Background(), testAccount(), testRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 || locations[0].RemoteID != "1" || locations[0].Name != "Основной склад" {
		t.Fatalf("unexpected %#v", locations)
	}
}

func TestReadInventoryReturnsRequestedProductsAndZeroForMissing(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod != "catalog.storeproduct.list" || !strings.Contains(string(request.Body), `"storeId":1`) || !strings.Contains(string(request.Body), `"@productId":[1267,1268]`) {
			t.Fatalf("unexpected request %#v", request)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"storeProducts":[{"id":11,"productId":1267,"storeId":1,"amount":18}]},"total":1}`)}, nil
	}}
	items, err := New(transport, testConfig{}, nil).ReadInventory(context.Background(), testAccount(), testRuntime{}, sdk.InventoryQuery{LocationRemoteID: "1", VariantRemoteIDs: []string{"1267", "1268"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].VariantRemoteID != "1267" || items[0].Quantity != 18 || items[1].VariantRemoteID != "1268" || items[1].Quantity != 0 {
		t.Fatalf("unexpected %#v", items)
	}
}

func TestReadInventoryRejectsFractionalQuantity(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		return Response{StatusCode: 200, Body: []byte(`{"result":{"storeProducts":[{"id":11,"productId":1267,"storeId":1,"amount":18.5}]},"total":1}`)}, nil
	}}
	_, err := New(transport, testConfig{}, nil).ReadInventory(context.Background(), testAccount(), testRuntime{}, sdk.InventoryQuery{LocationRemoteID: "1", VariantRemoteIDs: []string{"1267"}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected invalid response, got %v", err)
	}
}

func TestWriteInventoryUsesWarehouseDocumentAndReconciles(t *testing.T) {
	storeProductCalls := 0
	elementListCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.storeproduct.list":
			storeProductCalls++
			amount := "10"
			if storeProductCalls > 1 {
				amount = "15"
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"storeProducts":[{"id":11,"productId":1267,"storeId":1,"amount":` + amount + `}]},"total":1}`)}, nil
		case "catalog.document.list":
			var payload struct {
				Filter struct {
					DocNumber string `json:"docNumber"`
				} `json:"filter"`
			}
			if json.Unmarshal(request.Body, &payload) != nil || payload.Filter.DocNumber == "" {
				t.Fatalf("invalid document lookup payload: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"documents":[]},"total":0}`)}, nil
		case "catalog.document.add":
			var payload struct {
				Fields struct {
					DocNumber string `json:"docNumber"`
				} `json:"fields"`
			}
			if json.Unmarshal(request.Body, &payload) != nil {
				t.Fatalf("invalid document add payload: %s", request.Body)
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"document":{"id":42,"docType":"S","docNumber":"` + payload.Fields.DocNumber + `","status":"N"}}}`)}, nil
		case "catalog.document.element.list":
			elementListCalls++
			if elementListCalls == 1 {
				return Response{StatusCode: 200, Body: []byte(`{"result":{"documentElements":[]},"total":0}`)}, nil
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"documentElements":[{"id":99,"docId":42,"elementId":1267,"amount":5,"storeTo":1,"storeFrom":null}]},"total":1}`)}, nil
		case "catalog.document.element.add":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"documentElement":{"id":99}}}`)}, nil
		case "catalog.document.conduct":
			return Response{StatusCode: 200, Body: []byte(`{"result":true}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).WriteInventory(context.Background(), testAccount(), testRuntime{}, sdk.InventoryWriteRequest{VariantRemoteID: "1267", LocationRemoteID: "1", Quantity: 15, IdempotencyKey: "inventory-add-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1267" || storeProductCalls != 2 || elementListCalls != 2 {
		t.Fatalf("receipt=%#v storeProductCalls=%d elementListCalls=%d", receipt, storeProductCalls, elementListCalls)
	}
}

func TestWriteInventoryResumesDraftDocument(t *testing.T) {
	storeProductCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.storeproduct.list":
			storeProductCalls++
			amount := "10"
			if storeProductCalls > 1 {
				amount = "15"
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"storeProducts":[{"id":11,"productId":1267,"storeId":1,"amount":` + amount + `}]},"total":1}`)}, nil
		case "catalog.document.list":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"documents":[{"id":42,"docType":"S","docNumber":"` + inventoryDocumentNumber("inventory-draft-1") + `","status":"N"}]},"total":1}`)}, nil
		case "catalog.document.element.list":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"documentElements":[{"id":99,"docId":42,"elementId":1267,"amount":5,"storeTo":1,"storeFrom":null}]},"total":1}`)}, nil
		case "catalog.document.conduct":
			return Response{StatusCode: 200, Body: []byte(`{"result":true}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).WriteInventory(context.Background(), testAccount(), testRuntime{}, sdk.InventoryWriteRequest{VariantRemoteID: "1267", LocationRemoteID: "1", Quantity: 15, IdempotencyKey: "inventory-draft-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || storeProductCalls != 2 {
		t.Fatalf("receipt=%#v storeProductCalls=%d", receipt, storeProductCalls)
	}
}

func TestWriteInventoryDuplicateDoesNotCreateDocument(t *testing.T) {
	called := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod != "catalog.storeproduct.list" {
			called = true
			t.Fatalf("unexpected mutating or document method %s", request.APIMethod)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"storeProducts":[{"id":11,"productId":1267,"storeId":1,"amount":15}]},"total":1}`)}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).WriteInventory(context.Background(), testAccount(), testRuntime{}, sdk.InventoryWriteRequest{VariantRemoteID: "1267", LocationRemoteID: "1", Quantity: 15, IdempotencyKey: "inventory-duplicate-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Duplicate || !receipt.Reconciled || called {
		t.Fatalf("receipt=%#v called=%v", receipt, called)
	}
}

func priceResponse(id, productID, groupID int, value, timestamp string) []byte {
	return []byte(`{"result":{"prices":[{"id":` + strconv.Itoa(id) + `,"productId":` + strconv.Itoa(productID) + `,"catalogGroupId":` + strconv.Itoa(groupID) + `,"price":` + value + `,"currency":"RUB","timestampX":"` + timestamp + `"` + `}]}}`)
}

func TestReadPricesUsesConfiguredPriceType(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod != "catalog.price.list" {
			t.Fatalf("unexpected method %s", request.APIMethod)
		}
		return Response{StatusCode: 200, Body: priceResponse(91, 1267, 1, "1999.50", "2026-08-28T09:00:00+03:00")}, nil
	}}
	page, err := New(transport, testConfig{}, nil).ReadPrices(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].VariantRemoteID != "1267" || page.Items[0].Value != "1999.50" || page.Items[0].Currency != "RUB" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestWritePriceAddsAndReconciles(t *testing.T) {
	listCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.price.list":
			listCalls++
			if listCalls == 1 {
				return Response{StatusCode: 200, Body: []byte(`{"result":{"prices":[]}}`)}, nil
			}
			return Response{StatusCode: 200, Body: priceResponse(91, 1267, 1, "1999.50", "2026-08-28T09:00:00+03:00")}, nil
		case "catalog.price.add":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"price":{"id":91}}}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).WritePrice(context.Background(), testAccount(), testRuntime{}, sdk.PriceWriteRequest{VariantRemoteID: "1267", Value: "1999.50", Currency: "RUB", IdempotencyKey: "price-add-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1267" || listCalls != 2 {
		t.Fatalf("receipt=%#v listCalls=%d", receipt, listCalls)
	}
}

func TestWritePriceUpdatesExistingPrice(t *testing.T) {
	listCalls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.price.list":
			listCalls++
			value := "1000"
			if listCalls > 1 {
				value = "1250"
			}
			return Response{StatusCode: 200, Body: priceResponse(91, 1267, 1, value, "2026-08-28T09:00:00+03:00")}, nil
		case "catalog.price.update":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"price":{"id":91}}}`)}, nil
		default:
			t.Fatalf("unexpected method %s", request.APIMethod)
			return Response{}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).WritePrice(context.Background(), testAccount(), testRuntime{}, sdk.PriceWriteRequest{VariantRemoteID: "1267", Value: "1250", Currency: "RUB", IdempotencyKey: "price-update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1267" || listCalls != 2 {
		t.Fatalf("receipt=%#v listCalls=%d", receipt, listCalls)
	}
}

func TestWritePriceDuplicateDoesNotMutate(t *testing.T) {
	updateCalled := false
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod == "catalog.price.update" || request.APIMethod == "catalog.price.add" {
			updateCalled = true
		}
		if request.APIMethod != "catalog.price.list" {
			t.Fatalf("unexpected method %s", request.APIMethod)
		}
		return Response{StatusCode: 200, Body: priceResponse(91, 1267, 1, "1250", "2026-08-28T09:00:00+03:00")}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).WritePrice(context.Background(), testAccount(), testRuntime{}, sdk.PriceWriteRequest{VariantRemoteID: "1267", Value: "1250", Currency: "RUB", IdempotencyKey: "price-duplicate-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Duplicate || !receipt.Reconciled || updateCalled {
		t.Fatalf("receipt=%#v updateCalled=%v", receipt, updateCalled)
	}
}

func TestReadProductsUsesCatalogRESTShape(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.Method != "POST" || request.APIMethod != "catalog.product.list" || request.Host != "shop.example.com" || string(request.UserID) != "1" || string(request.WebhookCode) != "test-webhook-code" {
			t.Fatalf("unexpected request %#v", request)
		}
		return Response{StatusCode: 200, Body: []byte(`{"result":{"products":[{"id":1267,"iblockId":23,"name":"Товар","active":"Y","xmlId":"SKU-1","detailText":"Описание","timestampX":"2026-08-28T09:00:00+03:00"}]}}`)}, nil
	}}
	page, err := New(transport, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteID != "1267" || page.Items[0].SellerSKU != "SKU-1" || page.Items[0].Title != "Товар" {
		t.Fatalf("unexpected %#v", page)
	}
}

func TestUpsertProductUpdatesAndReconciles(t *testing.T) {
	calls := 0
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		if request.APIMethod == "catalog.product.get" {
			calls++
			name, description := "Старое название", "старое описание"
			if calls > 1 {
				name, description = "Товар", "Описание"
			}
			return Response{StatusCode: 200, Body: []byte(`{"result":{"product":{"id":1267,"iblockId":23,"name":"` + name + `","active":"Y","xmlId":"SKU-1","detailText":"` + description + `","timestampX":"2026-08-28T09:00:00+03:00"}}}`)}, nil
		}
		if request.APIMethod == "catalog.product.update" {
			return Response{StatusCode: 200, Body: []byte(`{"result":{"element":{"id":1267}}}`)}, nil
		}
		return Response{StatusCode: 404}, nil
	}}
	receipt, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{}, sdk.ProductWriteRequest{RemoteID: "1267", SellerSKU: "SKU-1", Title: "Товар", Description: "Описание", StatusRemoteID: "publish", IdempotencyKey: "update-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1267" || calls != 2 {
		t.Fatalf("receipt=%#v calls=%d", receipt, calls)
	}
}

func TestUpsertProductCreateIsIdempotentByXMLID(t *testing.T) {
	transport := scriptedTransport{fn: func(request Request) (Response, error) {
		switch request.APIMethod {
		case "catalog.product.list":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"products":[]}}`)}, nil
		case "catalog.product.add":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"element":{"id":1268}}}`)}, nil
		case "catalog.product.get":
			return Response{StatusCode: 200, Body: []byte(`{"result":{"product":{"id":1268,"iblockId":23,"name":"Новый","active":"Y","xmlId":"SKU-2","detailText":"","timestampX":"2026-08-28T09:00:00+03:00"}}}`)}, nil
		default:
			return Response{StatusCode: 404}, nil
		}
	}}
	receipt, err := New(transport, testConfig{}, nil).UpsertProduct(context.Background(), testAccount(), testRuntime{}, sdk.ProductWriteRequest{SellerSKU: "SKU-2", Title: "Новый", StatusRemoteID: "publish", IdempotencyKey: "create-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Applied || receipt.RemoteID != "1268" {
		t.Fatalf("unexpected %#v", receipt)
	}
}

func TestAPIErrorIsNormalized(t *testing.T) {
	transport := scriptedTransport{fn: func(Request) (Response, error) {
		return Response{StatusCode: 200, Body: []byte(`{"error":"ACCESS_DENIED","error_description":"secret text must not escape"}`)}, nil
	}}
	_, err := New(transport, testConfig{}, nil).ReadProducts(context.Background(), testAccount(), testRuntime{}, sdk.PageRequest{Limit: 50})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
