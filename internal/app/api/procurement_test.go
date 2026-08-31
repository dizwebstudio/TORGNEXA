package api

import (
	"encoding/json"
	"testing"
)

func TestProcurementRequestUsesWireFieldNames(t *testing.T) {
	var request purchaseOrderRequest
	if err := json.Unmarshal([]byte(`{"id":"po_1","supplier_id":"sup_1","warehouse_id":"wh_1","currency":"RUB","recommendation_id":"rec_1","recommendation_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","lines":[{"id":"line_1","offer_id":"offer_1","sku":"SKU-1","quantity":"2","unit":"PCS","unit_price_minor":1250}]}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.SupplierID != "sup_1" || request.WarehouseID != "wh_1" || request.RecommendationDigest == "" || len(request.Lines) != 1 || request.Lines[0].UnitPriceMinor != 1250 {
		t.Fatalf("request did not decode snake_case fields: %+v", request)
	}
}

func TestProcurementRoutesExposeAllOperatorActions(t *testing.T) {
	routes := newProcurementRoutes(nil, nil, nil, nil)
	paths := make(map[string]bool, len(routes))
	for _, route := range routes {
		paths[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET " + ProcurementSuppliersPath,
		"POST " + ProcurementPriceListsPath + "/preview",
		"POST " + ProcurementPurchaseOrdersPath + "/from-recommendations",
		"POST " + ProcurementPurchaseOrdersPath + "/",
		"GET " + ProcurementFindingsPath,
	} {
		if !paths[expected] {
			t.Fatalf("route %q is missing", expected)
		}
	}
}
