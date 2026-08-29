package worker

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

func TestMoneyToMajorUsesCurrencyMinorUnits(t *testing.T) {
	rub, err := domain.NewCurrency("RUB")
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.NewMoney(149990, rub)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustMoneyToMajor(t, value), "1499.90"; got != want {
		t.Fatalf("moneyToMajor() = %q, want %q", got, want)
	}

	jpy, _ := domain.NewCurrency("JPY")
	zeroScale, _ := domain.NewMoney(1500, jpy)
	if got, want := mustMoneyToMajor(t, zeroScale), "1500"; got != want {
		t.Fatalf("moneyToMajor(JPY) = %q, want %q", got, want)
	}
}

func TestDiscreteQuantityRejectsFractionalAndNonUnit(t *testing.T) {
	value, _ := domain.NewDecimal(24, 0)
	unit, _ := domain.NewUnitCode("PCS")
	quantity, _ := domain.NewQuantity(value, unit)
	if got, err := discreteQuantity(quantity); err != nil || got != 24 {
		t.Fatalf("discreteQuantity() = %d, %v", got, err)
	}

	fractional, _ := domain.NewDecimal(5, 1)
	fractionalQuantity, _ := domain.NewQuantity(fractional, unit)
	if _, err := discreteQuantity(fractionalQuantity); err == nil {
		t.Fatal("expected fractional quantity to be rejected")
	}
	kg, _ := domain.NewUnitCode("KG")
	kgQuantity, _ := domain.NewQuantity(value, kg)
	if _, err := discreteQuantity(kgQuantity); err == nil {
		t.Fatal("expected non-unit quantity to be rejected")
	}
}

func TestCommerceWriteEntityOnlyRoutesCanonicalEvents(t *testing.T) {
	if got, ok := commerceWriteEntity("commerce.catalog.product_changed.v1"); !ok || got != commerceProductsEntity {
		t.Fatalf("product event route = %q, %v", got, ok)
	}
	if got, ok := commerceWriteEntity("commerce.pricing.price_changed.v1"); !ok || got != commercePricesEntity {
		t.Fatalf("price event route = %q, %v", got, ok)
	}
	if got, ok := commerceWriteEntity("commerce.inventory.position_changed.v1"); !ok || got != commerceInventoryEntity {
		t.Fatalf("inventory event route = %q, %v", got, ok)
	}
	if _, ok := commerceWriteEntity("commerce.catalog.offer_changed.v1"); ok {
		t.Fatal("offer event must remain outside the commerce product write route")
	}
	if got, ok := commerceWriteEntity("commerce.orders.order_changed.v1"); !ok || got != commerceOrdersEntity {
		t.Fatalf("order event route = %q, %v", got, ok)
	}
}

func TestCommerceOrderEventValidation(t *testing.T) {
	var event commerceOrderEvent
	if err := decodeCommerceEvent(json.RawMessage(`{"order_id":"order-1","status":"processing","version":2,"change":"status_changed"}`), &event); err != nil {
		t.Fatal(err)
	}
	if event.OrderID != "order-1" || event.Status != "processing" || event.Version != 2 || event.Change != "status_changed" || !isCanonicalOrderStatus(event.Status) {
		t.Fatalf("unexpected order event: %+v", event)
	}
	for _, status := range []string{"", "unknown", "PROCESSING"} {
		if isCanonicalOrderStatus(status) {
			t.Fatalf("status %q must be rejected", status)
		}
	}
	if !isCanonicalOrderStatus("cancelled") {
		t.Fatal("cancelled must be a canonical order status")
	}
}

func TestCommerceProductEventValidation(t *testing.T) {
	var event commerceProductEvent
	if err := decodeCommerceEvent(json.RawMessage(`{"product_id":"product-1","version":2,"status":"active","change":"updated"}`), &event); err != nil {
		t.Fatal(err)
	}
	if event.ProductID != "product-1" || event.Version != 2 || !catalog.Status(event.Status).Valid() || !validProductChange(event.Change) {
		t.Fatalf("unexpected product event: %+v", event)
	}
	if err := decodeCommerceEvent(json.RawMessage(`{"product_id":"product-1","version":2,"status":"deleted","change":"updated"}`), &event); err != nil {
		t.Fatal(err)
	}
	if catalog.Status(event.Status).Valid() {
		t.Fatal("unknown product status must fail validation")
	}
	if validProductChange("deleted") {
		t.Fatal("unknown product change must fail validation")
	}
}

func TestCommerceSyncIdempotencyKeyIsStableAndPolicyScoped(t *testing.T) {
	first := commerceSyncIdempotencyKey("policy-1", "event-1")
	if first != commerceSyncIdempotencyKey("policy-1", "event-1") {
		t.Fatal("idempotency key must be stable")
	}
	if first == commerceSyncIdempotencyKey("policy-2", "event-1") || first == commerceSyncIdempotencyKey("policy-1", "event-2") {
		t.Fatal("idempotency key must include policy and event identity")
	}
}

func TestDecodeCommerceEventRejectsUnknownAndTrailingFields(t *testing.T) {
	var event commercePriceEvent
	if err := decodeCommerceEvent(json.RawMessage(`{"price_id":"p","offer_id":"o","kind":"regular","amount":{"minor_units":100,"currency":"RUB"},"version":1,"extra":true}`), &event); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
	if err := decodeCommerceEvent(json.RawMessage(`{"price_id":"p","offer_id":"o","kind":"regular","amount":{"minor_units":100,"currency":"RUB"},"version":1} {}`), &event); err == nil {
		t.Fatal("trailing JSON must be rejected")
	}
}

func TestClassifyCommerceWriteErrorPreservesRetryClass(t *testing.T) {
	retryable := classifyCommerceWriteError(&sdk.RemoteError{Category: sdk.ErrorRateLimited, Code: "rate_limited"})
	if class, _ := eventbus.ClassifyFailure(retryable); class != eventbus.FailureRetryable {
		t.Fatalf("retryable remote error classified as %v", class)
	}
	permanent := classifyCommerceWriteError(&sdk.RemoteError{Category: sdk.ErrorInvalidRequest, Code: "invalid"})
	if class, _ := eventbus.ClassifyFailure(permanent); class != eventbus.FailurePermanent {
		t.Fatalf("permanent remote error classified as %v", class)
	}
	if got := classifyCommerceWriteError(errors.New("transport")); got == nil {
		t.Fatal("unknown connector error must be retryable")
	}
}

func mustMoneyToMajor(t *testing.T, value domain.Money) string {
	t.Helper()
	result, err := moneyToMajor(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
