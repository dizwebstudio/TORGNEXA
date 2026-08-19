package catalogrepo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
)

func TestBuildCatalogEventsUseCanonicalEnvelope(t *testing.T) {
	scope, err := catalog.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	mutation := catalog.Mutation{EventID: "evt.catalog.product.1", OccurredAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), Source: "api", CorrelationID: "request-1"}
	product := catalog.Product{ID: catalog.ProductID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101"), OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Code: "P-1", Title: "Product", Status: catalog.StatusActive, Version: 2, CreatedAt: mutation.OccurredAt, UpdatedAt: mutation.OccurredAt}
	event, err := buildProductEvent(scope, mutation, product, "status_changed")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type.String() != "commerce.catalog.product_changed.v1" || event.EntityType != "product" || event.EntityID != product.ID.String() {
		t.Fatalf("unexpected event: %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "active" || payload["change"] != "status_changed" || payload["version"] != float64(2) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	for _, forbidden := range []string{"title", "description", "remote_id", "price", "quantity"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("event leaked %s", forbidden)
		}
	}
}
