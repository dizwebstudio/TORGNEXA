package connectors

import "testing"

func TestERPExactQuantityAndDuplicateBoundaries(t *testing.T) {
	valid := ERPInventoryPage{Items: []ERPInventory{{LocationRemoteID: "warehouse-a", ProductRemoteID: "product-a", Quantity: "12.500"}}}
	if err := valid.Validate(10); err != nil {
		t.Fatal(err)
	}
	for _, quantity := range []string{"1e3", "01", "1.", ".5", "1000000000000000000", "1.1234567890", "--1"} {
		item := ERPInventory{LocationRemoteID: "warehouse-a", ProductRemoteID: "product-a", Quantity: quantity}
		if item.Validate() == nil {
			t.Fatalf("unsafe quantity accepted: %q", quantity)
		}
	}
	negative := ERPInventory{LocationRemoteID: "warehouse-a", ProductRemoteID: "product-a", Quantity: "-1.25"}
	if err := negative.Validate(); err != nil {
		t.Fatalf("signed ERP balance rejected: %v", err)
	}
	duplicate := ERPInventoryPage{Items: []ERPInventory{{LocationRemoteID: "warehouse-a", ProductRemoteID: "product-a", Quantity: "1"}, {LocationRemoteID: "warehouse-a", ProductRemoteID: "product-a", Quantity: "2"}}}
	if duplicate.Validate(10) == nil {
		t.Fatal("duplicate ERP balance accepted")
	}
}

func TestERPProductRequiresRevisionAndBoundedIdentity(t *testing.T) {
	item := ERPProduct{RemoteID: "remote-1", Code: "0001", Title: "Product", Revision: "AQAAAA=="}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	item.Revision = ""
	if item.Validate() == nil {
		t.Fatal("missing revision accepted")
	}
}

func TestERPProductAllowsMissingOptionalCode(t *testing.T) {
	item := ERPProduct{RemoteID: "remote-1", Title: "Product", Revision: "2026-08-10 12:00:00.000"}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestERPOrderProjectionBoundaries(t *testing.T) {
	valid := ERPOrder{RemoteID: "order-1", Number: "00001", Revision: "2026-08-10 12:00:00.000", StatusRemoteID: "state-1", LocationRemoteID: "store-1", Applicable: true}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	page := ERPOrderPage{Items: []ERPOrder{valid}}
	if err := page.Validate(10); err != nil {
		t.Fatal(err)
	}
	duplicate := ERPOrderPage{Items: []ERPOrder{valid, valid}}
	if duplicate.Validate(10) == nil {
		t.Fatal("duplicate ERP order accepted")
	}
	valid.Number = ""
	if valid.Validate() == nil {
		t.Fatal("order without number accepted")
	}
}
